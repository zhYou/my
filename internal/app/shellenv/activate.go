package shellenv

import (
	"sort"
	"strings"
)

// ResolvedVariable 是已选择当前机器后的变量值。
type ResolvedVariable struct {
	Name  string
	Value string
}

// Render 在完整校验后生成指定 shell 的稳定赋值代码。
func Render(shell string, variables []ResolvedVariable) (string, error) {
	switch shell {
	case shellBash, shellZsh, shellFish, shellPowerShell:
	default:
		return "", invalidInput()
	}
	sorted := append([]ResolvedVariable(nil), variables...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	var output strings.Builder
	seen := map[string]struct{}{}
	for _, variable := range sorted {
		if !validName(variable.Name) ||
			dangerousName(variable.Name) ||
			variable.Value == "" ||
			len(variable.Value) > maxValueSize ||
			strings.ContainsAny(variable.Value, unsafeValueControls) {
			return "", invalidInput()
		}
		if _, duplicate := seen[variable.Name]; duplicate {
			return "", invalidInput()
		}
		seen[variable.Name] = struct{}{}
		switch shell {
		case shellBash, shellZsh:
			output.WriteString("export ")
			output.WriteString(variable.Name)
			output.WriteString("='")
			output.WriteString(strings.ReplaceAll(variable.Value, "'", "'\\''"))
			output.WriteString("'\n")
		case shellFish:
			output.WriteString("set -gx ")
			output.WriteString(variable.Name)
			output.WriteString(" '")
			output.WriteString(strings.ReplaceAll(
				strings.ReplaceAll(variable.Value, "\\", "\\\\"),
				"'",
				"\\'",
			))
			output.WriteString("'\n")
		case shellPowerShell:
			output.WriteString("$env:")
			output.WriteString(variable.Name)
			output.WriteString(" = '")
			output.WriteString(strings.ReplaceAll(variable.Value, "'", "''"))
			output.WriteString("'\n")
		}
		if output.Len() > maxOutputSize {
			return "", invalidInput()
		}
	}
	return output.String(), nil
}
