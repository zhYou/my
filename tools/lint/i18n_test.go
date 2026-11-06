package main

import (
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/locales"
)

func TestEmbeddedCatalog(t *testing.T) {
	require.NoError(t, checkI18n(locales.Catalog()))
}

func TestCheckAcceptsParameterUnionAcrossPluralForms(t *testing.T) {
	catalog := pair(`["items.count"]
 description = "items"
 one = "{{.Name}}"
 other = "{{.Count}}"
`, `["items.count"]
 other = "{{.Count}} {{.Name}}"
`)
	require.NoError(t, checkI18n(catalog))
}

func TestCheckFindsNestedTemplateParameters(t *testing.T) {
	catalog := pair(`["account"]
other = '{{if .Active}}{{.Name}}{{else}}{{index . "Fallback"}}{{end}}'
`, `["account"]
other = "{{.Fallback}} {{.Active}} {{.Name}}"
`)
	require.NoError(t, checkI18n(catalog))
}

func TestCheckRejectsInvalidOrMismatchedCatalogs(t *testing.T) {
	valid := `["sample.message"]
other = "Hello {{.Name}}"
`
	tests := map[string]fstest.MapFS{
		"empty directory":      {},
		"missing language":     {"active.en.toml": {Data: []byte(valid)}},
		"empty catalog":        pair("", valid),
		"duplicate id":         pair(valid+valid, valid),
		"unknown field":        pair(valid+"typo = \"x\"\n", valid),
		"missing other":        pair("[\"sample.message\"]\none = \"hi\"\n", valid),
		"empty message":        pair("[\"sample.message\"]\nother = \"   \"\n", valid),
		"empty plural form":    pair(valid+"one = \"\"\n", valid),
		"invalid template":     pair("[\"sample.message\"]\nother = \"{{.Name\"\n", valid),
		"unquoted dotted id":   pair("[sample.message]\nother = \"hi\"\n", valid),
		"different ids":        pair(valid, "[\"other.message\"]\nother = \"Hello {{.Name}}\"\n"),
		"different parameters": pair(valid, "[\"sample.message\"]\nother = \"Hello {{.Other}}\"\n"),
		"extra language file": {
			"active.en.toml":    {Data: []byte(valid)},
			"active.zh-CN.toml": {Data: []byte(valid)},
			"active.fr.toml":    {Data: []byte(valid)},
		},
	}
	for name, catalog := range tests {
		t.Run(name, func(t *testing.T) {
			require.Error(t, checkI18n(catalog))
		})
	}
}

func TestCheckAcceptsRootCatalogFiles(t *testing.T) {
	catalog := fstest.MapFS{
		"active.en.toml":    {Data: []byte("[\"hello\"]\nother = \"Hello {{.Name}}\"\n")},
		"active.zh-CN.toml": {Data: []byte("[\"hello\"]\nother = \"你好 {{.Name}}\"\n")},
	}
	require.NoError(t, checkI18n(catalog))
}

func TestCheckUnderstandsChangedTemplateDot(t *testing.T) {
	catalog := pair(`["profile"]
other = '{{with .User}}{{.Name}}{{end}} {{$.Count}}'
`, `["profile"]
other = "{{.User.Name}} {{.Count}}"
`)
	require.NoError(t, checkI18n(catalog))
}

func TestCheckIgnoresNestedIndexAsRootParameter(t *testing.T) {
	catalog := pair(`["profile"]
other = '{{with .User}}{{index . "Name"}}{{end}}'
`, `["profile"]
other = "{{.User.Name}}"
`)
	require.NoError(t, checkI18n(catalog))
}

func TestCheckComparesParametersAccessedThroughRootAliases(t *testing.T) {
	catalog := pair(`["profile"]
other = '{{$root := .}}{{$root.Name}}'
`, `["profile"]
other = '{{$root := .}}{{$root.Title}}'
`)
	require.Error(t, checkI18n(catalog))
}

func TestCheckComparesParametersWithinRootPreservingWith(t *testing.T) {
	catalog := pair(`["sample"]
other = '{{with .}}{{.Name}}{{end}}'
`, `["sample"]
other = '{{with .}}{{.Title}}{{end}}'
`)
	require.Error(t, checkI18n(catalog))
}

func TestCheckComparesParametersInCalledTemplate(t *testing.T) {
	catalog := pair(`["sample"]
other = '{{define "part"}}{{.Name}}{{end}}{{template "part" .}}'
`, `["sample"]
other = '{{define "part"}}{{.Title}}{{end}}{{template "part" .}}'
`)
	require.Error(t, checkI18n(catalog))
}

func TestCheckUnderstandsCalledTemplateContext(t *testing.T) {
	catalog := pair(`["sample"]
other = '{{define "part"}}{{.Name}}{{end}}{{template "part" .User}}'
`, `["sample"]
other = '{{.User.Name}}'
`)
	require.NoError(t, checkI18n(catalog))
}

func TestCheckAcceptsCalledTemplateWithMatchingRootParameters(t *testing.T) {
	catalog := pair(`["sample"]
other = '{{define "part"}}{{.Name}}{{end}}{{template "part" .}}'
`, `["sample"]
other = '{{with .}}{{.Name}}{{end}}'
`)
	require.NoError(t, checkI18n(catalog))
}

func TestCheckRejectsUnanalyzableTemplateCalls(t *testing.T) {
	tests := map[string]string{
		"undefined":        `{{template "missing" .}}`,
		"recursive":        `{{define "part"}}{{if .Next}}{{template "part" .Next}}{{end}}{{end}}{{template "part" .}}`,
		"computed context": `{{with printf "%s" .}}{{.Name}}{{end}}`,
		"computed alias":   `{{$value := printf "%s" .}}{{with $value}}{{.Name}}{{end}}`,
		"reassigned alias": `{{$value := .}}{{$value = printf "%s" .}}{{with $value}}{{.Name}}{{end}}`,
	}
	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			content := "[\"sample\"]\nother = '" + text + "'\n"
			require.Error(t, checkI18n(pair(content, content)))
		})
	}
}

// 不同语言需要的根参数必须被提取，不能因表达式形态被漏掉。
func TestCheckFindsParametersInAliasesAndPipelines(t *testing.T) {
	cases := map[string]string{
		"literal variable index": `{{$key := "Name"}}{{index . $key}}`,
		"parenthesized field":    `{{(.).Name}}`,
		"parenthesized alias":    `{{$root := (.)}}{{$root.Name}}`,
		"or root alias":          `{{$root := or . .}}{{$root.Name}}`,
		"pipeline index":         `{{"Name" | index .}}`,
		"direct reassignment":    `{{$root := .User}}{{$root = .}}{{$root.Name}}`,
	}
	for name, en := range cases {
		t.Run(name, func(t *testing.T) {
			content := func(text string) string {
				return "[\"sample\"]\nother = " + strconv.Quote(text) + "\n"
			}
			// 正例用独立写法表达同一组参数，防止双边同时漏检而假通过。
			reference := `{{.Name}}`
			if name == "direct reassignment" {
				reference = `{{.User}}{{.Name}}`
			}
			require.NoError(t, checkI18n(pair(content(en), content(reference))))
			zh := strings.ReplaceAll(en, "Name", "Title")
			require.Error(t, checkI18n(pair(content(en), content(zh))))
		})
	}
}

// 无法证明根键或分支赋值后的别名时，即使两边写法相同也必须拒绝。
func TestCheckRejectsUncertainRootDependencies(t *testing.T) {
	cases := map[string]string{
		"dynamic index key":           `{{$key := .Key}}{{index . $key}}`,
		"dynamic pipeline key":        `{{.Key | index .}}`,
		"unknown alias field":         `{{$root := or . .User}}{{$root.Name}}`,
		"branch assignment":           `{{$root := .User}}{{if .Active}}{{$root = $}}{{end}}{{$root.Name}}`,
		"branch condition assignment": `{{$root := .User}}{{if $root = .}}{{end}}{{$root.Name}}`,
		"range assignment":            `{{$root := .User}}{{range .Items}}{{$root.Name}}{{$root = $}}{{end}}`,
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			content := "[\"sample\"]\nother = " + strconv.Quote(text) + "\n"
			require.Error(t, checkI18n(pair(content, content)))
		})
	}
}

// range 的 else 分支在零次迭代时保留管道原值，而不是循环元素。
func TestCheckTracksRangeVariablesInElse(t *testing.T) {
	en := `["sample"]
other = '{{range $value := .}}{{else}}{{$value.Name}}{{end}}'
`
	zh := `["sample"]
other = '{{range $value := .}}{{else}}{{$value.Title}}{{end}}'
`
	require.Error(t, checkI18n(pair(en, zh)))
}

// 括号管道中的声明和赋值有副作用，不能沿用外层变量的旧值。
func TestCheckRejectsNestedPipelineDeclarations(t *testing.T) {
	tests := map[string]string{
		"nested assignment":           `{{$root := .User}}{{$unused := ($root = .)}}{{$root.Name}}`,
		"nested branch assignment":    `{{$root := .User}}{{if .Active}}{{$unused := ($root = $)}}{{end}}{{$root.Name}}`,
		"nested declaration":          `{{$root := .User}}{{$unused := ($root := .)}}{{$root.Name}}`,
		"nested condition assignment": `{{$root := .User}}{{if ($root = .)}}{{$root.Name}}{{end}}`,
	}
	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			content := "[\"sample\"]\nother = " + strconv.Quote(text) + "\n"
			require.ErrorContains(t, checkI18n(pair(content, content)), "嵌套")
		})
	}
	// 普通括号、声明和分支外重赋值保持可用。
	en := `["sample"]
other = '{{$root := (.User)}}{{$root = (.)}}{{$root.Name}}'
`
	zh := `["sample"]
other = '{{.User}}{{.Name}}'
`
	require.NoError(t, checkI18n(pair(en, zh)))
}

func pair(en, zh string) fstest.MapFS {
	return fstest.MapFS{
		"active.en.toml":    {Data: []byte(en)},
		"active.zh-CN.toml": {Data: []byte(zh)},
	}
}
