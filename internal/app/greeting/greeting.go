package greeting

import (
	"context"

	"github.com/zhYou/my/internal/base/messages"
	"github.com/zhYou/my/internal/base/validation"
	"github.com/zhYou/my/internal/locales"
)

// Options 声明本次调用的显式依赖与覆盖选项。
type Options struct {
	Localizer *messages.Localizer `validate:"-"`
	Name      string              `validate:"required,notblank" label:"--name"`
}

// Service 持有可复用的业务校验依赖。
type Service struct {
	validator *validation.Validator
}

// Result 保存命令成功时的有类型结果。
type Result struct {
	Message string `json:"message"`
}

// New 创建使用给定校验器的问候服务；nil 使用默认规则。
func New(v *validation.Validator) *Service {
	if v == nil {
		v = validation.New()
	}
	return &Service{validator: v}
}

// Run 执行一次业务调用，先校验输入再生成结果。
func Run(ctx context.Context, opts Options) (Result, error) {
	return New(validation.New()).Run(ctx, opts)
}

// Validate 使用调用者的校验器检查业务输入。
func (o Options) Validate(ctx context.Context, v *validation.Validator) error {
	return v.Struct(ctx, o)
}

// Run 执行一次业务调用，先校验输入再生成结果。
func (s *Service) Run(ctx context.Context, opts Options) (Result, error) {
	if err := opts.Validate(ctx, s.validator); err != nil {
		return Result{}, err
	}

	l := opts.Localizer
	if l == nil {
		b, err := locales.LoadBundle()
		if err != nil {
			return Result{}, err
		}
		l = b.Localizer("en")
	}

	message, err := l.Format("greeting.message", messages.FormatOptions{Params: map[string]any{"Name": opts.Name}})
	if err != nil {
		return Result{}, err
	}
	return Result{Message: message}, nil
}
