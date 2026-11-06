package validation

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/zhYou/my/internal/base/fault"
)

// Validator 持有构造后冻结的类型校验规则。
type Validator struct {
	engine *validator.Validate
}

// New 在并发使用前完成规则注册；可通过 callback 组合条件和业务规则。
func New(register ...func(*validator.Validate)) *Validator {
	engine := validator.New(validator.WithRequiredStructEnabled())
	engine.RegisterTagNameFunc(func(f reflect.StructField) string {
		if name := f.Tag.Get("label"); name != "" {
			return name
		}
		if name := f.Tag.Get("mapstructure"); name != "" {
			return name
		}
		return f.Name
	})
	if err := engine.RegisterValidation("notblank", func(f validator.FieldLevel) bool {
		return strings.TrimSpace(f.Field().String()) != ""
	}); err != nil {
		panic(err)
	}
	for _, configure := range register {
		configure(engine)
	}
	return &Validator{engine: engine}
}

// Struct 使用调用上下文校验有类型的结构体。
func (v *Validator) Struct(ctx context.Context, value any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := v.engine.StructCtx(ctx, value); err != nil {
		if fields, ok := errors.AsType[validator.ValidationErrors](err); ok {
			var problems []error
			for _, field := range fields {
				code := "validation." + field.Tag()
				switch field.Tag() {
				case "required", "notblank", "oneof":
				default:
					code = "validation.rule"
				}
				problems = append(
					problems,
					(fault.Definition{Code: fault.Code(code), Kind: fault.Usage}).Wrap(
						err,
						fault.WithParams(
							fault.Params{"Field": field.Field(), "Rule": field.Tag(), "Param": field.Param()},
						),
					),
				)
			}
			return errors.Join(problems...)
		}
		return (fault.Definition{Code: "validation.definition", Kind: fault.Runtime}).Wrap(err)
	}
	return ctx.Err()
}
