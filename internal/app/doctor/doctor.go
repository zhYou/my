package doctor

import "context"

// Options 预留调用级检查选项；仅在增加实际检查时扩展字段。
type Options struct{}

// Validate 检查调用者是否已取消；空入口没有需要校验的参数。
func (Options) Validate(ctx context.Context) error {
	return ctx.Err()
}

// 稳定状态用于机器判断，用户可见说明放在 Detail 中。
const (
	StatusAvailable = "available"
	StatusFailed    = "failed"
)

// Check 描述一项检查的结果；Required 表示其失败是否影响整体状态。
type Check struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Required bool   `json:"required"`
}

// Result 保存完整检查报告；即使必需项不可用也属于已完成的报告。
type Result struct {
	RequiredAvailable bool    `json:"required_available"`
	Checks            []Check `json:"checks"`
}

// Run 返回空报告，派生项目可在此添加自己的检查。
func Run(ctx context.Context, opts Options) (Result, error) {
	if err := opts.Validate(ctx); err != nil {
		return Result{}, err
	}
	return Result{
		RequiredAvailable: true,
		Checks:            []Check{},
	}, nil
}
