package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zhYou/my/tools/internal/fixture"
	"github.com/zhYou/my/tools/internal/toolchain"

	"github.com/stretchr/testify/require"
	cmdexec "github.com/zhYou/my/internal/command"
)

func TestDoctorHelper(t *testing.T) {
	if os.Getenv("DOCTOR_HELPER") != "1" {
		return
	}
	switch os.Args[len(os.Args)-1] {
	case "large":
		if _, err := os.Stdout.WriteString(strings.Repeat("x", 4096)); err != nil {
			t.Error(err)
		}
		if _, err := os.Stderr.WriteString(strings.Repeat("y", 4096)); err != nil {
			t.Error(err)
		}
	case "interval":

		start := time.Now().UnixNano()
		ready, gate := os.Args[len(os.Args)-3], os.Args[len(os.Args)-2]
		//nolint:gosec // G703：同步路径只由当前测试的临时目录生成。
		if err := os.WriteFile(ready, []byte("ready"), 0o600); err != nil {
			t.Fatal(err)
		}
		//nolint:gosec // G703：释放路径只由父测试的临时 fixture 传入。
		require.Eventually(t, func() bool {
			_, err := os.Stat(gate)
			return err == nil
		}, 2*time.Second, time.Millisecond, "未收到探测释放信号")
		data, err := json.Marshal([2]int64{start, time.Now().UnixNano()})
		require.NoError(t, err)
		if _, err := os.Stdout.Write(data); err != nil {
			t.Error(err)
		}
	case "sleep":
		time.Sleep(30 * time.Second)
	case "version":
		if _, err := os.Stdout.WriteString("go version go1.27.3 linux/amd64\n"); err != nil {
			t.Error(err)
		}
	}
	os.Exit(0)
}

func TestDoctorProbesBoundedAndOrdered(t *testing.T) {
	t.Setenv("DOCTOR_HELPER", "1")
	exe, err := os.Executable()
	require.NoError(t, err)
	calls := []cmdexec.Invocation{}
	for _, mode := range []string{"sleep", "large", "version"} {
		calls = append(calls, cmdexec.Invocation{
			Program: exe,
			Args:    []string{"-test.run=^TestDoctorHelper$", "--", mode},
		})
	}
	started := time.Now()
	results := runDoctorProbes(t.Context(), calls)
	require.Less(t, time.Since(started), 8*time.Second)
	require.Len(t, results, 3)
	require.Equal(t, cmdexec.Canceled, results[0].Status)
	require.ErrorIs(t, results[0].Err, context.DeadlineExceeded)
	require.Len(t, results[1].Stdout, 2048)
	require.Len(t, results[1].Stderr, 2048)
	require.True(t, results[1].StdoutTruncated)
	require.True(t, results[1].StderrTruncated)
	require.Equal(t, "go version go1.27.3 linux/amd64\n", string(results[2].Stdout))
	require.Contains(t, doctorProbeError("helper", results[0]).Error(), "超时")
	require.Contains(t, doctorProbeError("helper", results[1]).Error(), "截断")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	results = runDoctorProbes(ctx, calls)
	for _, result := range results {
		require.Equal(t, cmdexec.Skipped, result.Status)
		require.Contains(t, doctorProbeError("helper", result).Error(), "取消")
	}
}

func TestDoctorSummaryStableAndExplicit(t *testing.T) {
	var out bytes.Buffer
	calls := []doctorProbe{{
		key:  "go",
		want: "1.27.3",
		spec: toolchain.ToolSpec{Name: "go"},
	}, {
		key:  "syft",
		want: "1.55.0",
		spec: toolchain.ToolSpec{Name: "syft"},
	}}
	results := []cmdexec.Result{
		{Status: cmdexec.Succeeded, Stdout: []byte("go version go1.27.3 linux/amd64\n")},
		{Status: cmdexec.Succeeded, StdoutTruncated: true},
	}
	require.Error(t, reportDoctorProbes(&out, calls, results))
	require.Equal(t, "通过：go 1.27.3\n", out.String())
}

func TestDoctorReportsTimeoutAndTruncationTogether(t *testing.T) {
	err := doctorProbeError(
		"helper",
		cmdexec.Result{Status: cmdexec.Canceled, Err: context.DeadlineExceeded, StdoutTruncated: true},
	)
	require.ErrorContains(t, err, "超时")
	require.ErrorContains(t, err, "截断")
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestDoctorConcurrencyLimit(t *testing.T) {
	t.Setenv("DOCTOR_HELPER", "1")
	root := t.TempDir()
	t.Setenv("MY_HOME", root)
	exe, err := os.Executable()
	require.NoError(t, err)
	calls := make([]cmdexec.Invocation, 8)
	for i := range calls {
		calls[i] = cmdexec.Invocation{
			Program: exe,
			Args: []string{
				"-test.run=^TestDoctorHelper$",
				"--",
				filepath.Join(root, fmt.Sprintf("ready-%d", i)),
				filepath.Join(root, fmt.Sprintf("gate-%d", i/4)),
				"interval",
			},
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	done := make(chan []cmdexec.Result, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		done <- runDoctorProbes(ctx, calls)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(4 * time.Second):
			t.Error("探测 helper 未完成收尾")
		}
	})
	for wave := range 2 {
		for slot := range 4 {
			ready := filepath.Join(root, fmt.Sprintf("ready-%d", wave*4+slot))
			require.Eventually(t, func() bool {
				_, err := os.Stat(ready)
				return err == nil
			}, 2*time.Second, time.Millisecond, "未出现探测就绪标记 %s", ready)
		}
		require.NoError(t, os.WriteFile(filepath.Join(root, fmt.Sprintf("gate-%d", wave)), nil, 0o600))
	}
	var results []cmdexec.Result
	select {
	case results = <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("探测结果超时")
	}
	intervals := make([][2]int64, len(results))
	for i, result := range results {
		require.NoError(t, result.Err)
		require.NoError(t, json.Unmarshal(result.Stdout, &intervals[i]))
	}
	maxActive := 0
	for _, interval := range intervals {
		active := 0
		for _, other := range intervals {
			if other[0] <= interval[0] && interval[0] < other[1] {
				active++
			}
		}
		if active > maxActive {
			maxActive = active
		}
	}
	require.Equal(t, 4, maxActive, "真实进程区间应恰好达到四并发")
}

func TestDoctorRequiredTools(t *testing.T) {
	binary := doctorToolFixture(t)
	t.Run("available", func(t *testing.T) {
		doctorEnvironmentFixture(t, binary, "")
		require.NoError(t, doctor())
	})
	for _, name := range []string{"mise", "git", "gopls", "actionlint", "npx", "uvx"} {
		t.Run("missing-"+name, func(t *testing.T) {
			doctorEnvironmentFixture(t, binary, name)
			require.ErrorContains(t, doctor(), name)
		})
	}
	t.Run("gopls-mismatch", func(t *testing.T) {
		doctorEnvironmentFixture(t, binary, "")
		t.Setenv("DOCTOR_GOPLS_VERSION", "0.24.1")
		require.ErrorContains(t, doctor(), "gopls")
	})
	t.Run("gopls-failure", func(t *testing.T) {
		doctorEnvironmentFixture(t, binary, "")
		t.Setenv("DOCTOR_FAULT_TOOL", "gopls")
		t.Setenv("DOCTOR_FAULT", "failure")
		require.ErrorContains(t, doctor(), "gopls")
	})
	t.Run("mise-below-minimum", func(t *testing.T) {
		doctorEnvironmentFixture(t, binary, "")
		t.Setenv("DOCTOR_MISE_VERSION", "2026.10.20 linux-x64")
		require.ErrorContains(t, doctor(), "mise")
	})
}

func TestDoctorActionlintVersion(t *testing.T) {
	binary := doctorToolFixture(t)
	for _, version := range []string{"1.7.13", "1.7.12-rc.1"} {
		t.Run(version, func(t *testing.T) {
			doctorEnvironmentFixture(t, binary, "")
			t.Setenv("DOCTOR_ACTIONLINT_VERSION", version)
			require.ErrorContains(t, doctor(), "actionlint")
		})
	}
}

func TestDoctorUserToolsOnlyReport(t *testing.T) {
	binary := doctorToolFixture(t)
	out := doctorEnvironmentFixture(t, binary, "")
	for _, name := range []string{"gh", "codex", "rtk"} {
		for _, fault := range []struct {
			mode   string
			detail string
		}{
			{"available", "fixture"}, {"missing", "不可用"}, {"failure", "不可用"}, {"timeout", "超时"}, {"truncated", "截断"},
		} {
			t.Run(name+"/"+fault.mode, func(t *testing.T) {
				if fault.mode == "missing" {
					path, err := exec.LookPath(name)
					require.NoError(t, err)
					require.NoError(t, os.Rename(path, path+".hidden"))
					t.Cleanup(func() {
						require.NoError(t, os.Rename(path+".hidden", path))
					})
				}
				require.NoError(t, out.Truncate(0))
				_, err := out.Seek(0, 0)
				require.NoError(t, err)
				t.Setenv("DOCTOR_FAULT_TOOL", name)
				t.Setenv("DOCTOR_FAULT", fault.mode)
				require.NoError(t, doctor())
				_, err = out.Seek(0, 0)
				require.NoError(t, err)
				data, err := io.ReadAll(out)
				require.NoError(t, err)
				var detail string
				for line := range strings.SplitSeq(string(data), "\n") {
					if strings.HasPrefix(line, "用户工具："+name+"：") {
						detail = line
						break
					}
				}
				require.NotEmpty(t, detail)
				require.Contains(t, detail, fault.detail)
			})
		}
	}
}

func TestDoctorUsesMiseSelectedExecutable(t *testing.T) {
	binary := doctorToolFixture(t)
	doctorEnvironmentFixture(t, binary, "")
	file, err := os.OpenFile("mise.toml", os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = file.WriteString("uv = \"0.13.7\"\n")
	require.NoError(t, err)
	require.NoError(t, file.Close())
	name := "uv"
	if runtime.GOOS == windowsPlatform {
		name += executableSuffix
	}
	require.NoError(t, fixture.CopyFile(binary, filepath.Join(os.Getenv("DOCTOR_PROJECT_BIN"), name)))
	shadow := t.TempDir()
	require.NoError(t, fixture.CopyFile(binary, filepath.Join(shadow, name)))
	t.Setenv("PATH", shadow+string(os.PathListSeparator)+os.Getenv("PATH"))
	require.NoError(t, doctor())
}

// doctorToolFixture 仅模拟命令边界；真实工具和 MCP 在交付验收中独立执行。
func doctorToolFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "main.go")
	require.NoError(t, os.WriteFile(source, []byte(`package main
import ("fmt"; "os"; "path/filepath"; "strings"; "time")
func main() {
    name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
    if name == "mise" && len(os.Args) == 3 && os.Args[1] == "which" {
        path := filepath.Join(os.Getenv("DOCTOR_PROJECT_BIN"), os.Args[2])
        if runtime := os.Getenv("DOCTOR_EXE_SUFFIX"); runtime != "" { path += runtime }
        if _, err := os.Stat(path); err != nil { os.Exit(1) }
        fmt.Println(path); return
    }
    arg := "--version"
    if name == "gopls" { arg = "version" }
    if name == "actionlint" { arg = "-version" }
    if len(os.Args) != 2 || os.Args[1] != arg { os.Exit(2) }
    if name == os.Getenv("DOCTOR_FAULT_TOOL") {
        switch os.Getenv("DOCTOR_FAULT") {
        case "failure": fmt.Fprintln(os.Stderr, "fixture failed"); os.Exit(1)
        case "timeout": time.Sleep(30 * time.Second)
        case "truncated": fmt.Println(strings.Repeat("x", 4096)); return
        }
    }
    switch name {
    case "mise": fmt.Println(os.Getenv("DOCTOR_MISE_VERSION"))
    case "node": fmt.Println("v26.13.0")
    case "uv":
        if filepath.Dir(os.Args[0]) == os.Getenv("DOCTOR_PROJECT_BIN") { fmt.Println("uv 0.13.7") } else { fmt.Println("uv 0.13.6") }
    case "gopls": fmt.Println("golang.org/x/tools/gopls v" + os.Getenv("DOCTOR_GOPLS_VERSION"))
    case "actionlint": fmt.Println(os.Getenv("DOCTOR_ACTIONLINT_VERSION"))
    default: fmt.Println(name + " fixture")
    }
}
`), 0o600))
	binary := filepath.Join(dir, "probe")
	if runtime.GOOS == windowsPlatform {
		binary += executableSuffix
	}
	//nolint:gosec // G204：只构建固定的版本探测 fixture 到临时目录，不证明实际工具安装。
	output, err := exec.Command("go", "build", "-o", binary, source).CombinedOutput()
	require.NoError(t, err, "%s", output)
	return binary
}

func doctorEnvironmentFixture(t *testing.T, binary, missing string) *os.File {
	t.Helper()
	root := t.TempDir()
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(root, "mise.toml"),
			[]byte(
				"min_version = \"2026.10.21\"\n[tools]\nactionlint = \"1.7.12\"\n\"go:golang.org/x/tools/gopls\" = \"0.24.0\"\n",
			),
			0o600,
		),
	)
	bin := t.TempDir()
	for _, name := range []string{"mise", "git", "gh", "codex", "rtk", "gopls", "actionlint", "npx", "uvx"} {
		if name == missing {
			continue
		}
		if runtime.GOOS == windowsPlatform {
			name += executableSuffix
		}
		require.NoError(t, fixture.CopyFile(binary, filepath.Join(bin, name)))
	}
	t.Setenv("PATH", bin)
	t.Setenv("DOCTOR_PROJECT_BIN", bin)
	if runtime.GOOS == windowsPlatform {
		t.Setenv("DOCTOR_EXE_SUFFIX", executableSuffix)
	}
	t.Setenv("DOCTOR_MISE_VERSION", "2026.10.25 linux-x64")
	t.Setenv("DOCTOR_GOPLS_VERSION", "0.24.0")
	t.Setenv("DOCTOR_ACTIONLINT_VERSION", "1.7.12")
	t.Setenv("MY_HOME", t.TempDir())
	t.Chdir(root)

	// 先完成 fixture 的首次启动，避免宿主启动开销占用被测探测的三秒预算。
	for _, name := range []string{"mise", "git", "gh", "codex", "rtk", "gopls", "actionlint"} {
		if name == missing {
			continue
		}
		arg := "--version"
		if name == "gopls" {
			arg = "version"
		}
		if name == "actionlint" {
			arg = "-version"
		}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		//nolint:gosec // G204：只启动 PATH 指向的本次测试 fixture；故障模式在就绪后单独设置。
		cmd := exec.CommandContext(ctx, name, arg)
		cmd.Env = append(os.Environ(), "DOCTOR_FAULT_TOOL=", "DOCTOR_FAULT=")
		output, err := cmd.CombinedOutput()
		cancel()
		require.NoError(t, err, "fixture %s 未就绪：%s", name, output)
	}

	out, err := os.CreateTemp(t.TempDir(), "doctor-output")
	require.NoError(t, err)
	original := os.Stdout
	os.Stdout = out
	t.Cleanup(func() {
		os.Stdout = original
		require.NoError(t, out.Close())
	})
	return out
}
