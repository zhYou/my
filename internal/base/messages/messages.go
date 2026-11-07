package messages

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"strings"
	texttemplate "text/template"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"github.com/nicksnyder/go-i18n/v2/i18n/template"
	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/base/messages/catalog"
	"golang.org/x/text/language"
)

// Bundle 构造后不再暴露修改接口，可并发创建 Localizer。
type Bundle struct {
	bundle *i18n.Bundle
}

// 固定函数集不会随调用变化，可以安全复用已解析的模板。
type strictTextParser struct {
	template.TextParser
}

var strictParser = strictTextParser{template.TextParser{
	Option: "missingkey=error",
	Funcs:  texttemplate.FuncMap{"index": strictIndex},
}}

// Localizer 提供本次调用的只读语言视图。
type Localizer struct {
	language  string
	localizer *i18n.Localizer
}

// FormatOptions 指定模板参数和用于选择复数形式的计数。
type FormatOptions struct {
	Params      map[string]any
	PluralCount any
}

// NewBundle 加载并冻结消息目录；零参数保留为旧调用的诊断兜底。
func NewBundle(catalogs ...fs.FS) (*Bundle, error) {
	b := i18n.NewBundle(language.English)
	seen := make(map[string]map[string]bool)
	for _, source := range catalogs {
		names, err := catalog.Files(source)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			data, err := fs.ReadFile(source, name)
			if err != nil {
				return nil, fmt.Errorf("读取 %s：%w", name, err)
			}
			entries, err := catalog.Parse(data)
			if err != nil {
				return nil, fmt.Errorf("%s：%w", name, err)
			}
			// 仅借用上游文件名的语言解析，消息 ID 和字段以严格解析结果为准。
			file, err := i18n.ParseMessageFileBytes(nil, name, nil)
			if err != nil {
				return nil, fmt.Errorf("加载 %s：%w", name, err)
			}
			for id, fields := range entries {
				file.Messages = append(file.Messages, &i18n.Message{
					ID:          id,
					Description: fields["description"],
					Zero:        fields["zero"],
					One:         fields["one"],
					Two:         fields["two"],
					Few:         fields["few"],
					Many:        fields["many"],
					Other:       fields["other"],
				})
			}

			if len(file.Messages) == 0 {
				return nil, fmt.Errorf("消息文件 %s 为空", name)
			}
			tag := file.Tag.String()
			if seen[tag] == nil {
				seen[tag] = make(map[string]bool)
			}
			for _, message := range file.Messages {
				if seen[tag][message.ID] {
					return nil, fmt.Errorf("语言 %s 的消息 ID %q 重复", tag, message.ID)
				}
				seen[tag][message.ID] = true
				fillPluralForms(message)
			}
			if err := b.AddMessages(file.Tag, file.Messages...); err != nil {
				return nil, fmt.Errorf("加载 %s：%w", name, err)
			}
		}
	}
	return &Bundle{bundle: b}, nil
}

// Localizer 创建指定语言的独立只读视图。
func (b *Bundle) Localizer(lang string) *Localizer {
	switch lang {
	case "zh":
		lang = "zh-CN"
	case "":
		lang = "en"
	}
	return &Localizer{language: lang, localizer: i18n.NewLocalizer(b.bundle, lang)}
}

// Language 返回当前语言标识。
func (l *Localizer) Language() string {
	return l.language
}

// Format 只返回完整渲染的消息；查找、复数选择或模板执行失败时返回错误。
func (l *Localizer) Format(id string, opts FormatOptions) (string, error) {
	if l == nil || l.localizer == nil {
		return "", fmt.Errorf("无法渲染消息 %q：Localizer 未初始化", id)
	}

	params := opts.Params
	if opts.PluralCount != nil {
		params = make(map[string]any, len(opts.Params)+1)
		maps.Copy(params, opts.Params)
		params["PluralCount"] = opts.PluralCount
	}

	text, matched, err := l.localizer.LocalizeWithTag(&i18n.LocalizeConfig{
		MessageID:      id,
		TemplateData:   params,
		PluralCount:    opts.PluralCount,
		TemplateParser: &strictParser,
	})
	if err != nil {
		return "", fmt.Errorf("渲染消息 %q：%w", id, err)
	}

	requested, err := language.Parse(l.language)
	if err != nil {
		return "", fmt.Errorf("无效的语言 %q：%w", l.language, err)
	}
	if matched != requested {
		return "", fmt.Errorf("语言 %s 的消息 %q 不可用", requested, id)
	}
	return text, nil
}

// Text 渲染目录消息；失败时返回可诊断的默认文本。
func (l *Localizer) Text(id string, params map[string]any) string {
	opts := FormatOptions{Params: params}
	if count, ok := params["Count"]; ok {
		opts.PluralCount = count
	}
	if text, err := l.Format(id, opts); err == nil {
		return text
	}
	if l != nil && l.language == "en" {
		return "Operation failed [" + id + "]"
	}
	return "操作失败 [" + id + "]"
}

// Render 按错误结构组合领域消息与外部原因。
func (l *Localizer) Render(err error) string {
	var lines []string
	for _, detail := range fault.Details(err) {
		var text string
		if detail.Code != "" {
			text = l.Text(string(detail.Code), detail.Params)
		} else {
			text = detail.Message
		}
		if causeError := detail.DisplayCause(); causeError != nil &&
			!strings.HasPrefix(string(detail.Code), "validation.") {
			cause := ""
			// 未分类叶子错误保留系统或第三方诊断。
			if _, ok := errors.AsType[*fault.Error](causeError); ok {
				cause = l.Render(causeError)
			} else {
				cause = fmt.Sprint(causeError)
			}
			if text != "" {
				text += ": "
			}
			text += cause
		}
		lines = append(lines, text)
	}
	return strings.Join(lines, "; ")
}

func (strictTextParser) Cacheable() bool {
	return true
}

// 缺省复数形式使用必需的 other，避免上游返回可用文本时仍附带错误。
func fillPluralForms(message *i18n.Message) {
	if message.Zero == "" {
		message.Zero = message.Other
	}
	if message.One == "" {
		message.One = message.Other
	}
	if message.Two == "" {
		message.Two = message.Other
	}
	if message.Few == "" {
		message.Few = message.Other
	}
	if message.Many == "" {
		message.Many = message.Other
	}
}
