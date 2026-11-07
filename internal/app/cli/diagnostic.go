package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"

	resultschema "github.com/zhYou/my/internal/base/schema"

	"github.com/spf13/cobra"
	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/base/messages"
)

const (
	diagnosticError          = "error"
	diagnosticWarning        = "warning"
	diagnosticWarningMessage = "cli.warning"
	schemaTypeKey            = "type"
	schemaConstKey           = "const"
	schemaRequiredKey        = "required"
)

type diagnosticRecord struct {
	Type          string            `json:"type"`
	SchemaVersion int               `json:"schema_version"`
	ExitCode      *int              `json:"exit_code,omitempty"`
	RunID         string            `json:"run_id,omitempty"`
	Diagnostics   []errorDiagnostic `json:"diagnostics"`
}

type errorDiagnostic struct {
	Code             string              `json:"code"`
	Kind             fault.Kind          `json:"kind"`
	Message          string              `json:"message"`
	Phase            string              `json:"phase"`
	CompletedEffects []string            `json:"completed_effects"`
	Recovery         *recoveryDiagnostic `json:"recovery,omitempty"`
}

type recoveryDiagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (call *invocation) reportDiagnostic(out io.Writer, category string, err error, code int) {
	view := call.diagnosticView()
	record := diagnosticRecord{Type: category, SchemaVersion: 1, Diagnostics: view.diagnosticDetails(err)}
	if category == diagnosticError {
		record.ExitCode = &code
	}
	if view.session != nil {
		record.RunID = view.session.RunID
	}
	if view.jsonDiagnostics {
		//nolint:errcheck // 诊断出口失败不可覆盖业务结果，也不能递归告警。
		_ = writeJSON(out, record)
		return
	}
	var lines []string
	for _, detail := range record.Diagnostics {
		text := detail.Message
		if detail.Phase != "" {
			text += view.diagnosticText("cli.failure_phase", map[string]any{"Phase": detail.Phase})
		}
		if detail.CompletedEffects != nil {
			text += view.diagnosticText(
				"cli.completed_effects",
				map[string]any{"Effects": strings.Join(detail.CompletedEffects, ", ")},
			)
		}
		if detail.Recovery != nil {
			text += view.diagnosticText("cli.recovery", map[string]any{"Advice": detail.Recovery.Message})
		}
		lines = append(lines, text)
	}
	id := "cli.error"
	if category == diagnosticWarning {
		id = diagnosticWarningMessage
	}
	//nolint:errcheck // 错误或告警写入失败保留已确定的业务退出状态。
	_, _ = fmt.Fprintln(out, view.diagnosticText(id, map[string]any{paramMessage: strings.Join(lines, "; ")}))
}

func (call *invocation) diagnosticDetails(err error) []errorDiagnostic {
	details := []errorDiagnostic{}
	for _, problem := range fault.Details(err) {
		code := string(problem.Code)
		if code == "" {
			switch {
			case errors.Is(problem, context.DeadlineExceeded):
				code = codeDeadline
			case errors.Is(problem, context.Canceled):
				code = codeCancelled
			case problem.Kind == fault.Usage:
				code = codeUsage
			default:
				code = codeRuntime
			}
		}
		message := call.diagnosticText(code, problem.Params)
		if problem.Code != "" || code == codeRuntime || code == codeUsage {
			if call.localizer != nil {
				message = call.localizer.Render(problem)
			}
		}
		d, _ := fault.DiagnosticOf(problem)
		if d.Phase == "" {
			d.Phase = call.phase
		}
		item := errorDiagnostic{
			Code:             code,
			Kind:             problem.Kind,
			Message:          message,
			Phase:            d.Phase,
			CompletedEffects: d.CompletedEffects,
		}
		if code == codeCancelled || code == codeDeadline {
			item.Kind = fault.Runtime
		}
		if item.Phase == "" {
			item.Phase = "bootstrap"
		}
		if d.Recovery != nil {
			item.Recovery = &recoveryDiagnostic{
				Code:    d.Recovery.Code,
				Message: call.diagnosticText(d.Recovery.Code, d.Recovery.Params),
			}
		}
		details = append(details, item)
	}
	return details
}

func (call *invocation) diagnosticText(code string, params map[string]any) string {
	if call.localizer != nil {
		if message, err := call.localizer.Format(code, messages.FormatOptions{Params: params}); err == nil {
			return message
		}
	}
	if code == "cli.error" || code == diagnosticWarningMessage {
		prefix := "Error: "
		if code == diagnosticWarningMessage {
			prefix = "Warning: "
		}
		if message, ok := params[paramMessage].(string); ok {
			return prefix + message
		}
	}
	return "Operation failed [" + code + "]"
}

// selectJSON 只读取元数据；参数错误即使先于 --json 发生，也遵守最后一个合法布尔设置。
func selectJSON(root *cobra.Command, args []string) bool {
	if index, completion := completionRequest(root, args); completion {
		end := len(args)
		if end > index+1 {
			end--
		}
		complete := append([]string(nil), args[:index]...)
		args = append(complete, args[index+1:end]...)
	}
	//nolint:errcheck // 正式执行路径负责命令查找错误，预选不执行 Set 或 hook。
	target, scan, _ := root.Find(args)
	if target == nil {
		target = root
		scan = args
	}
	selected := false
	for i := 0; i < len(scan); i++ {
		arg := scan[i]
		if arg == "--" {
			break
		}
		if nameValue, ok := strings.CutPrefix(arg, "--"); ok {
			name, value, equals := strings.Cut(nameValue, "=")
			flag := commandFlag(target, name, false)
			if flag == nil {
				continue
			}
			if !equals && flag.NoOptDefVal == "" {
				i++
				continue
			}
			if name == flagJSON {
				if !equals {
					value = flag.NoOptDefVal
				}
				if parsed, err := strconv.ParseBool(value); err == nil {
					selected = parsed
				}
			}
		} else if strings.HasPrefix(arg, "-") && arg != "-" {
			for j := 1; j < len(arg); j++ {
				flag := commandFlag(target, arg[j:j+1], true)
				if flag != nil && (flag.NoOptDefVal == "" || j+1 < len(arg) && arg[j+1] == '=') {
					if flag.NoOptDefVal == "" && j+1 == len(arg) {
						i++
					}
					break
				}
			}
		}
	}
	return selected
}

func diagnosticSchema() (json.RawMessage, error) {
	raw, err := resultschema.FromType(reflect.TypeFor[diagnosticRecord](), nil)
	if err != nil {
		return nil, err
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	properties, ok := document["properties"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("诊断 Schema 缺少 properties")
	}
	properties["schema_version"] = map[string]any{schemaConstKey: 1}
	properties[schemaTypeKey] = map[string]any{"enum": []string{diagnosticError, diagnosticWarning}}
	items, ok := properties["diagnostics"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("诊断 Schema 缺少 diagnostics")
	}
	items["minItems"] = 1
	items[schemaTypeKey] = "array"
	properties["exit_code"] = map[string]any{schemaTypeKey: "integer", "enum": []int{1, 2, 130}}
	document["oneOf"] = []any{
		map[string]any{
			"properties":      map[string]any{schemaTypeKey: map[string]any{schemaConstKey: diagnosticError}},
			schemaRequiredKey: []string{"exit_code"},
		},
		map[string]any{
			"properties": map[string]any{schemaTypeKey: map[string]any{schemaConstKey: diagnosticWarning}},
			"not":        map[string]any{schemaRequiredKey: []string{"exit_code"}},
		},
	}
	override, err := json.Marshal(document)
	if err != nil {
		return nil, err
	}
	return resultschema.FromType(nil, override)
}
