package command

// Invocation 保存已构建的程序和独立 argv，不包含执行状态。
type Invocation struct {
	Operation string
	Program   string
	Args      []string
}

// Arg 根据强类型输入生成一段参数，不进行 shell 解析。
type Arg[P any] func(P) []string

// Spec 描述一个工具操作；定义在使用期间保持只读。
type Spec[P any] struct {
	Operation string
	Program   string
	Args      []Arg[P]
	Validate  func(P) error
}

// Build 校验输入并为本次调用构建独立的参数切片。
func (s Spec[P]) Build(input P) (Invocation, error) {
	if s.Program == "" {
		return Invocation{}, programRequired.New()
	}
	if s.Validate != nil {
		if err := s.Validate(input); err != nil {
			return Invocation{}, err
		}
	}
	call := Invocation{Program: s.Program, Operation: s.Operation}
	for _, arg := range s.Args {
		if arg == nil {
			return Invocation{}, argumentRequired.New()
		}
		call.Args = append(call.Args, arg(input)...)
	}
	return call, nil
}

// Literal 固定一段参数；复制输入以免调用方改动定义。
func Literal[P any](values ...string) Arg[P] {
	fixed := append([]string(nil), values...)
	return func(P) []string {
		return append([]string(nil), fixed...)
	}
}

// Value 插入一个参数，空字符串也保留。
func Value[P any](get func(P) string) Arg[P] {
	return func(input P) []string {
		return []string{get(input)}
	}
}

// Values 插入一组参数；Build 将内容复制到本次调用。
func Values[P any](get func(P) []string) Arg[P] {
	return func(input P) []string {
		return get(input)
	}
}

// When 仅在条件满足时插入整个参数片段。
func When[P any](include func(P) bool, parts ...Arg[P]) Arg[P] {
	fragments := append([]Arg[P](nil), parts...)
	return func(input P) []string {
		if !include(input) {
			return nil
		}
		var values []string
		for _, part := range fragments {
			values = append(values, part(input)...)
		}
		return values
	}
}
