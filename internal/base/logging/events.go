package logging

import (
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	eventCommandStarted  = "command_started"
	eventCommandFinished = "command_finished"
	fieldComponent       = "component"
	fieldCommand         = "command"
	fieldRunID           = "run_id"
	fieldPID             = "pid"
	fieldVersion         = "version"
	fieldCommit          = "commit"
	fieldExitCode        = "exit_code"
	fieldDuration        = "duration"
	fieldTool            = "tool"
	fieldErrorCode       = "error_code"
	fieldErrorCodes      = "error_codes"
	fieldErrorParams     = "error_params"
)

// CommandResult 保存命令结束事件，时长沿用 logger 的编码规则。
type CommandResult struct {
	Message  string
	ExitCode int
	Duration time.Duration
	Err      error
}

type commandResultEncoder CommandResult

// ToolCall 保存外部工具调用结果，不包含参数、环境或输出内容。
type ToolCall struct {
	Message   string
	Operation string
	Tool      string
	Outcome   string
	ExitCode  int
	Duration  time.Duration
	Err       error
}

// Started 记录命令已完成配置与日志初始化。
func (l *Logger) Started() {
	l.Infow(eventCommandStarted)
}

// Emit 以固定的命令结束事件写入顶层字段。
func (e CommandResult) Emit(logger *Logger) {
	if e.ExitCode != 0 || e.Err != nil {
		logger.sugar.Errorw(e.message(), zap.Inline(commandResultEncoder(e)))
		return
	}
	logger.sugar.Infow(e.message(), zap.Inline(commandResultEncoder(e)))
}

func (e CommandResult) message() string {
	if e.Message != "" {
		return e.Message
	}
	return eventCommandFinished
}

// MarshalLogObject 显式编码结果及错误，避免反射改变时长和错误格式。
func (e commandResultEncoder) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	for _, field := range errorFields(e.Err) {
		field.AddTo(enc)
	}
	enc.AddString("event", eventCommandFinished)
	enc.AddInt(fieldExitCode, e.ExitCode)
	enc.AddDuration(fieldDuration, e.Duration)
	return nil
}

// Emit 记录工具结束事件，不包含原始参数和输出。
func (e ToolCall) Emit(l *Logger) {
	fields := []any{
		"event",
		"tool_finished",
		"operation",
		e.Operation,
		fieldTool,
		e.Tool,
		"outcome",
		e.Outcome,
		fieldExitCode,
		e.ExitCode,
		fieldDuration,
		e.Duration,
	}
	for _, f := range errorFields(e.Err) {
		fields = append(fields, f)
	}
	if e.Err != nil {
		l.sugar.Errorw(e.message(), fields...)
	} else {
		l.sugar.Infow(e.message(), fields...)
	}
}

func (e ToolCall) message() string {
	if e.Message != "" {
		return e.Message
	}
	return "tool_finished"
}
