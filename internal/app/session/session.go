package session

import (
	"context"
	"io"
	"path/filepath"
	"sync"

	appconfig "github.com/zhYou/my/internal/app/config"
	"github.com/zhYou/my/internal/base/diagnostics"
	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/base/iostreams"
	"github.com/zhYou/my/internal/base/logging"
	"github.com/zhYou/my/internal/base/messages"
	"github.com/zhYou/my/internal/base/paths"
	"github.com/zhYou/my/internal/base/telemetry"
	"github.com/zhYou/my/internal/locales"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	loggingFailed     = fault.Definition{Code: "session.logging_failed", Kind: fault.Runtime}
	telemetryFailed   = fault.Definition{Code: "session.telemetry_failed", Kind: fault.Runtime}
	diagnosticsFailed = fault.Definition{Code: "session.diagnostics_failed", Kind: fault.Runtime}
)

// Options 显式提供目录、借用资源和构造依赖；省略目录时按应用的标准路径解析。
type Options struct {
	Paths          paths.Options
	Directories    *paths.Directories
	Stderr         io.Writer
	ExternalLogger *logging.Logger
	NewLogger      func(logging.RunInfo, logging.Options, func(error)) *logging.Logger
	Localizer      *messages.Localizer
}

// Session 唯一持有自建资源；业务只使用 Logger、Tracer 和 Go 注册工作。
type Session struct {
	Logger      *logging.Logger
	Tracer      trace.Tracer
	files       *diagnostics.Run
	ownsLogger  bool
	warnLogging func(error)
	telemetry   *telemetry.Session
	span        trace.Span
	once        sync.Once
	mu          sync.Mutex
	finishing   bool
	work        sync.WaitGroup
	localizer   *messages.Localizer
}

// Begin 仅在参数及配置全部校验后调用；诊断失败只告警，不改变业务结果。
func Begin(
	ctx context.Context,
	config appconfig.Config,
	info logging.RunInfo,
	opts Options,
	warn func(error),
) (context.Context, *Session) {
	// 直接嵌入 Session 的回调也可能写原始 stderr；与日志出口使用同一写锁。
	var stderrLock sync.Mutex
	serializedWarn := func(err error) {
		stderrLock.Lock()
		defer stderrLock.Unlock()
		if warn != nil {
			warn(err)
		}
	}
	loggingWarn := reporter(loggingFailed, serializedWarn)
	traceWarn := reporter(telemetryFailed, serializedWarn)
	filesWarn := reporter(diagnosticsFailed, serializedWarn)
	s := &Session{ownsLogger: opts.ExternalLogger == nil, warnLogging: loggingWarn, localizer: opts.Localizer}
	if s.localizer == nil {
		bundle, err := locales.LoadBundle()
		if err != nil {
			serializedWarn(loggingFailed.Wrap(err))
		} else {
			s.localizer = bundle.Localizer("en")
		}
	}
	id := diagnostics.NewID()
	if s.ownsLogger && config.Logging.Output == "file" || config.Telemetry.Exporter == telemetry.File {
		var dirs paths.Directories
		var err error
		if opts.Directories != nil {
			dirs = *opts.Directories
		} else {
			dirs, err = paths.Resolve(opts.Paths)
		}
		if err == nil {
			s.files, err = diagnostics.Open(filepath.Join(dirs.State, "logs"), config.Diagnostics, filesWarn)
		}
		if err != nil {
			filesWarn(err)
		}
		if s.files != nil {
			id = s.files.ID
		}
	}
	var logWriter io.Writer
	if s.ownsLogger {
		switch config.Logging.Output {
		case "file":
			logWriter = s.writer(diagnostics.EventsFile, loggingWarn)
		case "stderr":
			if opts.Stderr != nil {
				logWriter = iostreams.Synchronize(opts.Stderr, &stderrLock)
			}
		case "none":
			opts.ExternalLogger = logging.NewNop()
		}
	}
	factory := opts.NewLogger
	if factory == nil {
		factory = logging.New
	}
	s.Logger = factory(
		info,
		logging.Options{Config: config.Logging, Writer: logWriter, External: opts.ExternalLogger, RunID: id},
		loggingWarn,
	)
	var traceWriter io.Writer
	if config.Telemetry.Exporter == telemetry.File {
		traceWriter = s.writer(diagnostics.TraceFile, traceWarn)
	}
	s.telemetry = telemetry.Start(
		ctx,
		config.Telemetry,
		telemetry.Info{Service: info.Component, Version: info.Version, RunID: id},
		traceWriter,
		traceWarn,
	)
	s.Tracer = s.telemetry.Tracer
	ctx, s.span = s.Tracer.Start(ctx, info.Component+"."+info.Command)
	s.Logger = s.Logger.WithContext(ctx)
	s.Logger.Infow(diagnosticText(s.localizer, "session.started"), "event", "command_started")
	return ctx, s
}

// Go 在关闭前登记并启动工作。返回 false 表示 Finish 已开始，工作不会执行。
// 任务接收调用方显式传入的 Context，并须自行尊重取消；Finish 等待这些工作真正结束后才关闭资源。
func (s *Session) Go(ctx context.Context, work func(context.Context)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finishing || work == nil {
		return false
	}
	s.work.Go(func() {
		work(ctx)
	})
	return true
}

// Finish 幂等地等工作结束、记结果、结束根 span、用独立超时排空 Trace，再同步关闭自有文件。
// 借用 logger 和 IO 不会被同步或关闭；任意自定义阻塞 writer 不保证严格返回期限。
func (s *Session) Finish(ctx context.Context, result logging.CommandResult) {
	s.once.Do(func() {
		s.mu.Lock()
		s.finishing = true
		s.mu.Unlock()
		s.work.Wait()
		result.Message = diagnosticText(s.localizer, "session.finished")
		s.Logger.Record(result)
		if result.ExitCode != 0 || result.Err != nil {
			s.span.SetStatus(codes.Error, diagnosticText(s.localizer, "session.failed"))
			if result.Err != nil {
				s.span.RecordError(result.Err)
			}
		}
		s.span.End()
		//nolint:errcheck // telemetry.Close 已向会话告警回调报告，诊断失败不改变业务结果。
		_ = s.telemetry.Close(ctx)
		if s.ownsLogger {
			s.warnLogging(s.Logger.Sync())
		}
		if s.files != nil {
			//nolint:errcheck // diagnostics.Run.Close 已通过 filesWarn 报告，避免重复告警。
			_ = s.files.Close()
		}
	})
}

func reporter(def fault.Definition, warn func(error)) func(error) {
	var once sync.Once
	return func(err error) {
		if err != nil && warn != nil {
			once.Do(func() {
				warn(def.Wrap(err))
			})
		}
	}
}

func diagnosticText(localizer *messages.Localizer, id string) string {
	if localizer == nil || localizer.Language() == "" {
		return "Operation failed [" + id + "]"
	}
	return localizer.Text(id, nil)
}

func (s *Session) writer(name string, warn func(error)) io.Writer {
	if s.files == nil {
		return nil
	}
	w, err := s.files.Writer(name)
	if err != nil {
		warn(err)
		return nil
	}
	return w
}
