package command

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/base/iostreams"
	"github.com/zhYou/my/internal/base/logging"
	"github.com/zhYou/my/internal/base/messages"
	"go.opentelemetry.io/otel/trace"
)

// DefaultWaitDelay 是取消时的终止宽限及根进程退出后的管道排空宽限。
const DefaultWaitDelay = 250 * time.Millisecond

// Execution 的流为借用资源，不关闭调用方句柄。Env=nil 继承当前环境，空切片清空环境。
// WaitDelay=0 使用默认宽限；不为普通命令添加独立 deadline。
// Localizer 显式选择日志语言；nil 使用集中目录的英文消息。结构化错误由应用入口统一渲染。
type Execution struct {
	Localizer *messages.Localizer
	Logger    *logging.Logger
	Tracer    trace.Tracer
	Operation string
	Dir       string
	Env       []string
	IO        iostreams.IOStreams
	WaitDelay time.Duration
}

// Run 同步执行并等待自有资源收尾，保留 exec.ExitError 及完整取消原因链。
func Run(ctx context.Context, call Invocation, options Execution) (result error) {
	if ctx.Err() != nil {
		return contextError(ctx)
	}
	if call.Program == "" {
		return programRequired.New()
	}
	if options.WaitDelay < 0 {
		return waitDelayInvalid.New()
	}
	if options.WaitDelay == 0 {
		options.WaitDelay = DefaultWaitDelay
	}

	env := options.Env
	if env == nil {
		env = os.Environ()
	}
	options.Env = append([]string{}, env...)
	call.Args = append([]string(nil), call.Args...)
	options.IO = options.IO.WithDefaults()

	ctx, finish := observe(ctx, call, options)
	defer func() {
		finish(result)
	}()

	path, err := resolve(call.Program, options.Dir, options.Env)
	if err != nil {
		return executionFailure.Wrap(
			err,
			fault.WithDiagnostic(fault.Diagnostic{Phase: "command.resolve", CompletedEffects: []string{}}),
		)
	}
	err = runPlatform(ctx, path, call.Args, options)
	if ctx.Err() != nil {
		err = errors.Join(err, contextError(ctx))
	}
	return executionFailure.Wrap(err, fault.WithDiagnostic(fault.Diagnostic{Phase: "command.execute"}))
}

// WithEnv 独立复制环境，再追加覆盖变量；父配置不变。
func (options Execution) WithEnv(values ...string) Execution {
	env := options.Env
	if env == nil {
		env = os.Environ()
	}
	options.Env = append(append([]string{}, env...), values...)
	return options
}

// WithOperation 补充缺省操作名，保留调用者已有的领域操作。
func (options Execution) WithOperation(operation string) Execution {
	if options.Operation == "" {
		options.Operation = operation
	}
	return options
}

func contextError(ctx context.Context) error {
	return errors.Join(ctx.Err(), context.Cause(ctx))
}
