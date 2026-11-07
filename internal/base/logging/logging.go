package logging

import (
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"github.com/zhYou/my/internal/base/fault"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Options 描述已由入口准备的日志输出；Writer 和 External 均不由日志包关闭。
type Options struct {
	Config   Config
	Writer   io.Writer
	External *Logger
	RunID    string
}

// RunInfo 保存本次运行共有的日志上下文。
type RunInfo struct {
	Component string
	Command   string
	Version   string
	Commit    string
}

// New 从入口传入的输出构造日志，只派生公共字段，不创建目录或持有文件所有权。
func New(info RunInfo, options Options, warn func(error)) *Logger {
	report := func(err error) {
		if err != nil && warn != nil {
			warn(err)
		}
	}
	logger := options.External
	if logger == nil {
		var level zapcore.Level
		if err := level.UnmarshalText([]byte(options.Config.Level)); err != nil {
			report(err)
			return NewNop()
		}
		if options.Writer == nil {
			report(writerunavailable.New())
			return NewNop()
		}
		cfg := zap.NewProductionEncoderConfig()
		cfg.TimeKey = "ts"
		cfg.EncodeTime = func(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
			enc.AppendString(t.UTC().Format(time.RFC3339Nano))
		}
		core := zapcore.NewCore(zapcore.NewJSONEncoder(cfg), zapcore.Lock(zapcore.AddSync(options.Writer)), level)
		logger = &Logger{sugar: zap.New(core, zap.ErrorOutput(zapcore.AddSync(warningWriter{report}))).Sugar()}
	}
	logger = logger.With(
		fieldRunID,
		options.RunID,
		fieldComponent,
		info.Component,
		fieldCommand,
		info.Command,
		fieldPID,
		os.Getpid(),
	)
	if info.Version != "" {
		logger = logger.With(fieldVersion, info.Version)
	}
	if info.Commit != "" {
		logger = logger.With(fieldCommit, info.Commit)
	}
	return logger
}

// Sync 同步日志后端，不关闭输出；仅由拥有该 logger 的应用入口调用。
func (l *Logger) Sync() error {
	return l.sugar.Sync()
}

type warningWriter struct {
	warn func(error)
}

func (w warningWriter) Write(data []byte) (int, error) {
	w.warn(errors.New(strings.TrimSpace(string(data))))
	return len(data), nil
}

// errorFields 保留稳定错误标识、参数和原始原因。
func errorFields(err error) []zap.Field {
	fields := []zap.Field{zap.Error(err)}
	if err == nil {
		return fields
	}
	type diagnosticDetail struct {
		Code             fault.Code `json:"code"`
		Kind             fault.Kind `json:"kind"`
		Phase            string     `json:"phase"`
		CompletedEffects []string   `json:"completed_effects"`
		RecoveryCode     string     `json:"recovery_code,omitempty"`
	}
	var diagnostics []diagnosticDetail
	for _, detail := range fault.Details(err) {
		d, _ := fault.DiagnosticOf(detail)
		row := diagnosticDetail{
			Code:             detail.Code,
			Kind:             detail.Kind,
			Phase:            d.Phase,
			CompletedEffects: d.CompletedEffects,
		}
		if d.Recovery != nil {
			row.RecoveryCode = d.Recovery.Code
		}
		diagnostics = append(diagnostics, row)
	}
	fields = append(fields, zap.Any("error_details", diagnostics))
	details := fault.Details(err)
	if len(details) > 0 {
		codes := make([]string, 0, len(details))
		for _, detail := range details {
			codes = append(codes, string(detail.Code))
		}
		fields = append(
			fields,
			zap.String(fieldErrorCode, string(details[0].Code)),
			zap.Strings(fieldErrorCodes, codes),
			zap.Any(fieldErrorParams, details[0].Params),
		)
	}
	return fields
}
