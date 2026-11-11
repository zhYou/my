package shellenv

import (
	"io"
	"strings"
	"unicode/utf8"

	"github.com/zhYou/my/internal/app/machine"
)

// ValidateSelection 校验变量名及机器选择，不读取变量值。
func (o SetOptions) ValidateSelection() error {
	if !validName(o.Name) || dangerousName(o.Name) {
		return invalidInput()
	}
	seen := map[string]bool{}
	for _, id := range o.Machines {
		if machine.ValidateID(id) != nil || seen[id] {
			return invalidInput()
		}
		seen[id] = true
	}
	return nil
}

// Validate 校验所有输入；适用机器是否存在由固定远端快照决定。
func (o SetOptions) Validate() error {
	if err := o.ValidateSelection(); err != nil {
		return err
	}
	if !validValue(o.Value) {
		return invalidInput()
	}
	return nil
}

// Validate 校验待删除变量名。
func (o RemoveOptions) Validate() error {
	if !validName(o.Name) || dangerousName(o.Name) {
		return invalidInput()
	}
	return nil
}

// ReadValue 从借用输入读取有界值，不关闭输入，读取失败的原始原因不进入诊断。
func ReadValue(input io.Reader) ([]byte, error) {
	value, err := io.ReadAll(io.LimitReader(input, MaxValueSize+1))
	if err != nil {
		return nil, problem("shellenv.read_input", phaseValidate, err)
	}
	if !validValue(value) {
		return nil, invalidInput()
	}
	return value, nil
}

func validValue(value []byte) bool {
	return len(value) > 0 && len(value) <= MaxValueSize && utf8.Valid(value) &&
		!strings.ContainsAny(string(value), unsafeValueControls)
}
