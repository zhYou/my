package fault

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Kind 表示错误对应的调用、配置或运行分类。
type Kind string

const (
	// Usage 表示无效调用。
	Usage Kind = "usage"
	// Configuration 表示配置加载或校验失败。
	Configuration Kind = "configuration"
	// Runtime 表示命令执行失败。
	Runtime Kind = "runtime"
)

// Code 是保持机器兼容的领域错误标识。
type Code string

// Params 保存供消息目录渲染的结构化参数。
type Params map[string]any

// Definition 由拥有错误的模块定义；同一错误码不依赖全局注册表。
type Definition struct {
	Code Code
	Kind Kind
}

// Error 保存错误分类、消息和原始原因。
type Error struct {
	Code           Code
	Kind           Kind
	Params         Params
	Message        string
	Cause          error
	Diagnostic     Diagnostic
	sensitiveCause bool
	details        []*Error
}

// Option 在构造错误时补充调用级信息。
type Option func(*Error)

// WithParams 快照参数映射；嵌套值应为不可变值，每个错误拥有独立映射。
func WithParams(params Params) Option {
	snapshot := maps.Clone(params)
	return func(e *Error) {
		e.Params = maps.Clone(snapshot)
	}
}

// New 创建领域错误，未指定参数时保持独立状态。
func (d Definition) New(opts ...Option) *Error {
	e := &Error{Code: d.Code, Kind: d.Kind}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Wrap 保留原始原因并补充错误分类。
func (d Definition) Wrap(cause error, opts ...Option) error {
	if cause == nil {
		return nil
	}
	e := d.New(opts...)
	e.Cause = cause
	return e
}

// Error 组合领域标识、参数与原始原因，返回诊断文本。
func (e *Error) Error() string {
	if !e.sensitiveCause && len(e.details) > 0 {
		texts := make([]string, 0, len(e.details))
		for _, detail := range e.details {
			texts = append(texts, detail.Error())
		}
		return strings.Join(texts, "; ")
	}
	text := e.Message
	if text == "" {
		text = string(e.Code)
		if len(e.Params) > 0 {
			text += fmt.Sprint(e.Params)
		}
	}
	if cause := e.DisplayCause(); cause != nil {
		if text == "" {
			return cause.Error()
		}
		return text + "：" + cause.Error()
	}
	return text
}

// Unwrap 返回原始原因，支持标准错误链检查。
func (e *Error) Unwrap() error {
	return e.Cause
}

// New 和 Wrap 保留无消息码的外部错误适配入口。
func New(kind Kind, message string) error {
	return &Error{Kind: kind, Message: message}
}

// Wrap 保留原始原因并补充错误分类。
func Wrap(kind Kind, message string, cause error) error {
	return &Error{Kind: kind, Message: message, Cause: cause}
}

// Details 展开聚合错误，保留每项的最外层分类及原始原因。
func Details(err error) []*Error {
	if err == nil {
		return nil
	}
	//nolint:errorlint // 此处逐层遍历保留最外层分类，errors.As 会跳过聚合结构。
	switch e := err.(type) {
	case *Error:
		if !e.sensitiveCause && len(e.details) > 0 {
			return slices.Clone(e.details)
		}
		if !e.sensitiveCause {
			children := Details(e.Cause)
			if len(children) > 1 {
				result := make([]*Error, 0, len(children))
				for _, child := range children {
					branch := *e
					branch.Cause = child
					result = append(result, &branch)
				}
				return result
			}
		}
		return []*Error{e}
	case interface{ Unwrap() []error }:
		var result []*Error
		for _, child := range e.Unwrap() {
			result = append(result, Details(child)...)
		}
		return result
	default:
		if next := errors.Unwrap(err); next != nil {
			return Details(next)
		}
		return []*Error{{Kind: Runtime, Cause: err}}
	}
}

// KindOf 聚合时运行/配置错误优先于调用错误。
func KindOf(err error) Kind {
	kind := Usage
	details := Details(err)
	if len(details) == 0 {
		return Runtime
	}
	for _, e := range details {
		switch e.Kind {
		case Runtime:
			return Runtime
		case Configuration:
			kind = Configuration
		case Usage:
		default:
			return Runtime
		}
	}
	return kind
}
