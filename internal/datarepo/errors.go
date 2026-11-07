package datarepo

import (
	"context"
	"errors"
	"net"

	"github.com/zhYou/my/internal/base/fault"
)

// RequestTimeoutError 标记调用方尚未取消时发生的单次外部调用超时。
// 原始原因可通过 Unwrap 检查，入口可将其与调用方取消区分。
type RequestTimeoutError struct{ Cause error }

// Error 返回不包含外部正文的安全说明。
func (e *RequestTimeoutError) Error() string {
	return "external request timed out"
}

// Unwrap 保留原始超时原因。
func (e *RequestTimeoutError) Unwrap() error {
	return e.Cause
}

func failure(code, phase string, cause error, effects []string, recovery string) error {
	options := []fault.Option{
		fault.WithSensitiveCause(),
		fault.WithDiagnostic(
			fault.Diagnostic{Phase: phase, CompletedEffects: effects, Recovery: &fault.Recovery{Code: recovery}},
		),
	}
	definition := fault.Definition{Code: fault.Code(code), Kind: fault.Runtime}
	if cause == nil {
		return definition.New(options...)
	}
	return definition.Wrap(cause, options...)
}

func requestError(ctx context.Context, err error) error {
	if err == nil || ctx.Err() != nil {
		return err
	}
	var network net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &network) && network.Timeout() {
		return &RequestTimeoutError{Cause: err}
	}
	return err
}
