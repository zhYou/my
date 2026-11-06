package command

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/zhYou/my/internal/base/fault"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"
)

// BatchOptions 缺省顺序执行并收集全部结果；单项超时从取得名额后开始计算。
// 并发共享的输入流应由调用方提供并发安全实现，输出始终逐项独立捕获。
type BatchOptions struct {
	Concurrency int
	ItemTimeout time.Duration
	FailFast    bool
	Capture     CaptureOptions
}

// RunBatch 按输入顺序返回结果。派生取消留在单项错误，整体按最先终止批量的原因分类。
// 已获名额后的自身期限到期记为 Canceled；批量取消导致尚未开跑的项才记为 Skipped。
func RunBatch(parent context.Context, calls []Invocation, execution Execution, options BatchOptions) ([]Result, error) {
	if options.Concurrency < 0 || options.ItemTimeout < 0 || options.Capture.Limit < 0 {
		return nil, batchOptionsInvalid.New()
	}
	if options.Concurrency == 0 {
		options.Concurrency = 1
	}
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	results := make([]Result, len(calls))
	for i := range results {
		results[i] = Result{Status: Skipped, ExitCode: -1}
	}
	sem := semaphore.NewWeighted(int64(options.Concurrency))
	var group errgroup.Group
	var mu sync.Mutex
	var first error
	for i, call := range calls {
		if err := sem.Acquire(ctx, 1); err != nil {
			break
		}
		if ctx.Err() != nil {
			sem.Release(1)
			break
		}
		group.Go(func() error {
			defer sem.Release(1)
			itemCtx := ctx
			stop := func() {}
			if options.ItemTimeout > 0 {
				itemCtx, stop = context.WithTimeoutCause(ctx, options.ItemTimeout, itemDeadlineError{})
			}
			defer stop()
			results[i] = Capture(itemCtx, call, execution, options.Capture)
			// 独立 cause 标记避免与同时到来的父期限/内部 fail-fast 取消混淆。
			//nolint:errorlint // 独立 cause 哨兵用于区分本项期限与批量取消，必须比较精确身份。
			if results[i].Status == Skipped && context.Cause(itemCtx) == (itemDeadlineError{}) {
				results[i].Status = Canceled
			}
			if results[i].Err != nil {
				mu.Lock()
				if first == nil {
					first = batchFailure(i, results[i].Err)
					if options.FailFast {
						cancel(first)
					}
				}
				mu.Unlock()
			}
			return nil
		})
	}
	//nolint:errcheck // worker 始终返回 nil；业务错误按输入顺序从 results 聚合，保留 fail-fast 首因。
	_ = group.Wait()
	var failures []error
	for i := range results {
		if results[i].Status == Skipped {
			results[i].Err = contextError(ctx)
		}
		if !options.FailFast && results[i].Err != nil && results[i].Status != Skipped {
			failures = append(failures, batchFailure(i, results[i].Err))
		}
	}
	if options.FailFast && first != nil {
		failures = append(failures, first)
	}
	// WithCancelCause 原子保留首先触发批量终止的原因；收尾期间的父取消不能覆盖 fail-fast 首因。
	//nolint:errorlint // 比较同一个首因对象，保留 fail-fast 与父取消的先后关系。
	if parent.Err() != nil && (!options.FailFast || context.Cause(ctx) != first) {
		failures = append(failures, contextError(parent))
	}
	return results, errors.Join(failures...)
}

func batchFailure(index int, err error) error {
	// 标准进程错误保留 errors.As；派生 deadline/cancel 不使 CLI 将普通批量失败映射为 130。
	cause := withoutCancellation(err)
	if cause == nil {
		return batchItemIncomplete.New(fault.WithParams(fault.Params{"Index": index + 1}))
	}
	return batchItemFailed.Wrap(cause, fault.WithParams(fault.Params{"Index": index + 1}))
}

func withoutCancellation(err error) error {
	//nolint:errorlint // 这里只移除取消叶子；包装节点必须继续递归以保留真实原因。
	if err == nil || err == context.Canceled || err == context.DeadlineExceeded {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var kept []error
		for _, child := range joined.Unwrap() {
			kept = append(kept, withoutCancellation(child))
		}
		return errors.Join(kept...)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return withoutCancellation(errors.Unwrap(err))
	}
	return err
}

// 保留标准 deadline 错误链，同时识别本项自行到期；不引入可变全局状态。
type itemDeadlineError struct{}

func (itemDeadlineError) Error() string {
	return context.DeadlineExceeded.Error()
}

func (itemDeadlineError) Unwrap() error {
	return context.DeadlineExceeded
}
