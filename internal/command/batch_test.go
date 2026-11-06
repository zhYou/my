package command

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// 文件门闩确定先后次序；TERM 标记证明失败已触发 fail-fast 且另一项尚在收尾。
func TestBatchFirstCauseSurvivesCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TERM 宽限时序属于 Unix 后端")
	}
	for _, failureFirst := range []bool{true, false} {
		name := "parent-first"
		if failureFirst {
			name = "failure-first"
		}
		t.Run(name, func(t *testing.T) {
			call, options := helper(t, "term-marker")
			options.WaitDelay = time.Second
			ready := filepath.Join(options.Dir, "ready")
			term := filepath.Join(options.Dir, "term")
			failReady := filepath.Join(options.Dir, "fail-ready")
			gate := filepath.Join(options.Dir, "gate")
			call.Args = append(call.Args, ready, term)
			failure := call
			failure.Args = []string{"-test.run=^TestHelperProcess$", "--", "gate-fail", failReady, gate}
			ctx, cancel := context.WithCancelCause(t.Context())
			parentCause := errors.New("父调用停止")
			type outcome struct {
				items []Result
				err   error
			}
			done := make(chan outcome, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				items, err := RunBatch(
					ctx,
					[]Invocation{call, failure, call},
					options,
					BatchOptions{Concurrency: 2, FailFast: true},
				)
				done <- outcome{items, err}
			}()
			t.Cleanup(func() {
				cancel(parentCause)
				select {
				case <-finished:
				case <-time.After(4 * time.Second):
					t.Error("真实 helper 收尾未完成")
				}
			})
			waitMarker(t, ready)
			waitMarker(t, failReady)
			if failureFirst {
				if err := os.WriteFile(gate, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				cancel(parentCause)
			}
			waitMarker(t, term)
			if failureFirst {
				cancel(parentCause)
			} else {
				if err := os.WriteFile(gate, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var result outcome
			select {
			case result = <-done:
			case <-time.After(4 * time.Second):
				t.Fatal("批量没有完成收尾")
			}
			var exit *exec.ExitError
			if !errors.As(result.items[1].Err, &exit) || exit.ExitCode() != 7 {
				t.Fatalf("未保留真实失败: %+v", result.items)
			}
			if result.items[0].Status != Canceled || !errors.Is(result.items[0].Err, context.Canceled) ||
				result.items[2].Status != Skipped {
				t.Fatalf("单项状态/错误链: %+v", result.items)
			}
			if failureFirst {
				if result.err == nil || errors.Is(result.err, context.Canceled) ||
					errors.Is(result.err, context.DeadlineExceeded) {
					t.Fatalf("后到父取消覆盖普通首因: %v", result.err)
				}
				if !errors.As(result.err, &exit) || exit.ExitCode() != 7 {
					t.Fatalf("整体丢失首个退出错误: %v", result.err)
				}
			} else if !errors.Is(result.err, context.Canceled) || !errors.Is(result.err, parentCause) || !errors.Is(result.items[0].Err, parentCause) {
				t.Fatalf("父取消首因未保留: %v %+v", result.err, result.items)
			}
		})
	}
}

// 一纳秒期限在构造子 context 时已到期，直接覆盖拿到名额但尚未进入 Run 的路径。
func TestBatchImmediateItemDeadlineIsNotSkipped(t *testing.T) {
	for _, failFast := range []bool{false, true} {
		name := "collect-all"
		if failFast {
			name = "fail-fast"
		}
		t.Run(name, func(t *testing.T) {
			call, options := helper(t, "bytes")
			results, err := RunBatch(
				t.Context(),
				[]Invocation{call, call, call},
				options,
				BatchOptions{Concurrency: 1, ItemTimeout: time.Nanosecond, FailFast: failFast},
			)
			if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("自身期限应汇总为普通失败: %v", err)
			}
			for i, result := range results {
				if failFast && i > 0 {
					if result.Status != Skipped || !errors.Is(result.Err, context.Canceled) {
						t.Fatalf("未取名额项应 skipped: %+v", result)
					}
					continue
				}
				if result.Status != Canceled || !errors.Is(result.Err, context.DeadlineExceeded) ||
					len(result.Stdout) != 0 {
					t.Fatalf("获名额后的自身期限被吞掉: %+v", result)
				}
			}
		})
	}
	for _, parentDeadline := range []bool{false, true} {
		parent, cancel := context.WithCancel(t.Context())
		cancel()
		if parentDeadline {
			var stop context.CancelFunc
			parent, stop = context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
			defer stop()
		}
		call, options := helper(t, "bytes")
		results, err := RunBatch(parent, []Invocation{call}, options, BatchOptions{ItemTimeout: time.Nanosecond})
		if results[0].Status != Skipped || !errors.Is(results[0].Err, parent.Err()) || !errors.Is(err, parent.Err()) {
			t.Fatalf("父先取消应 skipped: %v %+v", err, results)
		}
	}
}

func TestBatchOrderCollectAll(t *testing.T) {
	first, options := helper(t, "gate-output")
	firstReady, firstGate := filepath.Join(options.Dir, "first-ready"), filepath.Join(options.Dir, "first-gate")
	secondReady, secondGate := filepath.Join(options.Dir, "second-ready"), filepath.Join(options.Dir, "second-gate")
	first.Args = append(first.Args, firstReady, firstGate, "first")
	second := first
	second.Args = []string{"-test.run=^TestHelperProcess$", "--", "gate-output", secondReady, secondGate, "second"}
	failed := first
	failed.Args = []string{"-test.run=^TestHelperProcess$", "--", "exit"}
	items := []Invocation{first, failed, second}
	// 顺序与 collect-all 由文件门闩控制，不对成功进程施加短期限。
	done := startBatch(t.Context(), t, items, options, BatchOptions{Concurrency: 1})
	waitMarker(t, firstReady)
	if err := os.WriteFile(firstGate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitMarker(t, secondReady)
	if err := os.WriteFile(secondGate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	outcome := awaitBatch(t, done)
	results, err := outcome.items, outcome.err
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("整体分类 %v", err)
	}
	if results[0].Status != Succeeded || string(results[0].Stdout) != "first" || results[1].Status != Failed ||
		results[1].ExitCode != 7 ||
		results[2].Status != Succeeded ||
		string(results[2].Stdout) != "second" {
		t.Fatalf("顺序 %+v", results)
	}
}

func TestBatchItemTimeoutStartsAfterSlot(t *testing.T) {
	blocked, options := helper(t, "gate-output")
	blocked.Args = append(
		blocked.Args,
		filepath.Join(options.Dir, "ready"),
		filepath.Join(options.Dir, "never-release"),
		"never",
	)
	next := blocked
	next.Args = []string{"-test.run=^TestHelperProcess$", "--", "sleep", "0s", "after"}
	// 第一项等待永不打开的门闩并用尽期限，下一项获得名额后才开始自己的计时。
	results, err := RunBatch(
		t.Context(),
		[]Invocation{blocked, next},
		options,
		BatchOptions{Concurrency: 1, ItemTimeout: time.Second},
	)
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("整体分类 %v", err)
	}
	if results[0].Status != Canceled || !errors.Is(results[0].Err, context.DeadlineExceeded) ||
		len(results[0].Stdout) != 0 {
		t.Fatalf("单项超时 %+v", results[0])
	}
	if results[1].Status != Succeeded || results[1].Err != nil || string(results[1].Stdout) != "after" {
		t.Fatalf("排队时间占用了单项期限 %+v", results[1])
	}
}

func TestBatchFailFastAndParentCancellation(t *testing.T) {
	fail, options := helper(t, "exit")
	sleep := fail
	sleep.Args = []string{"-test.run=^TestHelperProcess$", "--", "sleep", "1m", "never"}
	results, err := RunBatch(
		t.Context(),
		[]Invocation{fail, sleep, sleep},
		options,
		BatchOptions{Concurrency: 1, FailFast: true},
	)
	if err == nil || errors.Is(err, context.Canceled) || results[1].Status != Skipped || results[2].Status != Skipped {
		t.Fatalf("fail-fast %v %+v", err, results)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	results, err = RunBatch(ctx, []Invocation{sleep, sleep}, options, BatchOptions{})
	if !errors.Is(err, context.Canceled) || results[0].Status != Skipped || results[1].Status != Skipped {
		t.Fatalf("父取消 %v %+v", err, results)
	}
}

func TestBatchConcurrencyBound(t *testing.T) {
	call, options := helper(t, "interval")
	items := make([]Invocation, 6)
	for i := range items {
		items[i] = call
		items[i].Args = append(
			append([]string{}, call.Args...),
			filepath.Join(options.Dir, fmt.Sprintf("ready-%d", i)),
			filepath.Join(options.Dir, fmt.Sprintf("gate-%d", i/2)),
		)
	}
	done := startBatch(t.Context(), t, items, options, BatchOptions{Concurrency: 2})
	for wave := range 3 {
		for slot := range 2 {
			waitMarker(t, filepath.Join(options.Dir, fmt.Sprintf("ready-%d", wave*2+slot)))
		}
		if err := os.WriteFile(filepath.Join(options.Dir, fmt.Sprintf("gate-%d", wave)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result := awaitBatch(t, done)
	if result.err != nil {
		t.Fatal(result.err)
	}
	results := result.items
	intervals := make([][2]int64, len(results))
	for i, r := range results {
		if err := json.Unmarshal(r.Stdout, &intervals[i]); err != nil {
			t.Fatal(err)
		}
	}
	maxActive := 0
	for _, period := range intervals {
		active := 0
		for _, other := range intervals {
			if other[0] <= period[0] && period[0] < other[1] {
				active++
			}
		}
		if active > maxActive {
			maxActive = active
		}
	}
	if maxActive != 2 {
		t.Fatalf("实际最大并发 %d", maxActive)
	}
}

func TestConcurrentFailFastKeepsItemCauseOnly(t *testing.T) {
	failure, options := helper(t, "gate-fail")
	ready, gate := filepath.Join(options.Dir, "ready"), filepath.Join(options.Dir, "gate")
	failureReady := filepath.Join(options.Dir, "failure-ready")
	failure.Args = append(failure.Args, failureReady, gate)
	sleeping := failure
	sleeping.Args = []string{"-test.run=^TestHelperProcess$", "--", "ready-sleep", ready, "1m", "never"}
	done := startBatch(
		t.Context(),
		t,
		[]Invocation{sleeping, failure, sleeping},
		options,
		BatchOptions{Concurrency: 2, FailFast: true},
	)
	waitMarker(t, ready)
	waitMarker(t, failureReady)
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	result := awaitBatch(t, done)
	results, err := result.items, result.err
	var exit *exec.ExitError
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		!errors.As(err, &exit) ||
		exit.ExitCode() != 7 {
		t.Fatalf("整体错误 %v", err)
	}
	if results[0].Status != Canceled || !errors.Is(results[0].Err, context.Canceled) || results[1].Status != Failed ||
		results[1].ExitCode != 7 ||
		results[2].Status != Skipped {
		t.Fatalf("派生取消 %+v", results)
	}
}

func TestActiveParentCancelStopsQueueAndRetainsCause(t *testing.T) {
	call, options := helper(t, "ready-sleep")
	ready := filepath.Join(options.Dir, "ready")
	call.Args = append(call.Args, ready, "1m", "never")
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	cause := errors.New("父调用终止")
	done := startBatch(ctx, t, []Invocation{call, call, call}, options, BatchOptions{Concurrency: 1})
	waitMarker(t, ready)
	cancel(cause)
	result := awaitBatch(t, done)
	results, err := result.items, result.err
	if !errors.Is(err, context.Canceled) || !errors.Is(err, cause) || results[0].Status != Canceled ||
		!errors.Is(results[0].Err, cause) ||
		results[1].Status != Skipped ||
		results[2].Status != Skipped {
		t.Fatalf("父取消 %v %+v", err, results)
	}
}
