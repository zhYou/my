package messages

import (
	"errors"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/base/fault"
)

func TestBundleIsolationFallbackAndErrorCauses(t *testing.T) {
	fs := fstest.MapFS{
		"active.en.toml": {Data: []byte(`["sample"]
other = "Hello, {{.Name}}!"
`)},
		"active.zh-CN.toml": {Data: []byte(`["sample"]
other = "你好，{{.Name}}！"
`)},
	}
	b, err := NewBundle(fs)
	require.NoError(t, err)
	en, zh := b.Localizer("en"), b.Localizer("zh-CN")
	require.Equal(t, "Hello, Ada!", en.Text("sample", map[string]any{"Name": "Ada"}))
	require.Equal(t, "你好，Ada！", zh.Text("sample", map[string]any{"Name": "Ada"}))
	require.NotContains(t, en.Text("missing", nil), "log")
	cause := errors.New("disk full")
	e := (fault.Definition{Code: "sample", Kind: fault.Runtime}).Wrap(
		cause,
		fault.WithParams(fault.Params{"Name": "Ada"}),
	)
	require.Contains(t, en.Render(e), "Hello, Ada!")
	require.Contains(t, en.Render(e), "disk full")
	require.NotContains(t, en.Text("sample", nil), "<no value>")
	require.Equal(t, "Hello, <no value>!", en.Text("sample", map[string]any{"Name": "<no value>"}))
}

func TestBundleLoadsTOMLAndRejectsEmptyOrCorruptCatalog(t *testing.T) {
	t.Run("TOML message", func(t *testing.T) {
		catalog := fstest.MapFS{
			"active.en.toml": {Data: []byte(`["sample.message"]
other = "Hello, {{.Name}}!"
`)},
		}
		bundle, err := NewBundle(catalog)
		require.NoError(t, err)
		require.Equal(t, "Hello, Ada!", bundle.Localizer("en").Text("sample.message", map[string]any{"Name": "Ada"}))
	})
	tests := map[string]fstest.MapFS{
		"empty directory": {},
		"empty file":      {"active.en.toml": {Data: nil}},
		"unknown field": {"active.en.toml": {Data: []byte(`["sample"]
other = "text"
typo = "ignored"
`)}},
		"missing other": {"active.en.toml": {Data: []byte(`["sample"]
one = "text"
`)}},
	}
	for name, catalog := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := NewBundle(catalog)
			require.Error(t, err)
		})
	}
}

func TestFormatRequiresCompleteRenderingAndSupportsPluralCount(t *testing.T) {
	catalog := fstest.MapFS{
		"active.en.toml": {Data: []byte(`["files.count"]
one = "{{.Count}} file for {{.Name}}"
other = "{{.Count}} files for {{.Name}}"
["files.default_count"]
other = "{{.PluralCount}} files"
`)},
		"active.zh-CN.toml": {Data: []byte(`["files.count"]
other = "{{.Name}} 的 {{.Count}} 个文件"
`)},
	}
	bundle, err := NewBundle(catalog)
	require.NoError(t, err)
	en, zh := bundle.Localizer("en"), bundle.Localizer("zh")
	one, err := en.Format("files.count", FormatOptions{
		Params:      map[string]any{"Count": 1, "Name": "Ada"},
		PluralCount: 1,
	})
	require.NoError(t, err)
	require.Equal(t, "1 file for Ada", one)
	many, err := en.Format("files.count", FormatOptions{
		Params:      map[string]any{"Count": 2, "Name": "Ada"},
		PluralCount: 2,
	})
	require.NoError(t, err)
	require.Equal(t, "2 files for Ada", many)
	count, err := en.Format("files.default_count", FormatOptions{
		PluralCount: 2,
		Params:      map[string]any{"Unused": "value"},
	})
	require.NoError(t, err)
	require.Equal(t, "2 files", count)
	translated, err := zh.Format("files.count", FormatOptions{
		Params:      map[string]any{"Count": 2, "Name": "Ada"},
		PluralCount: 2,
	})
	require.NoError(t, err)
	require.Equal(t, "Ada 的 2 个文件", translated)

	partial, err := en.Format("files.count", FormatOptions{
		Params:      map[string]any{"Count": 1},
		PluralCount: 1,
	})
	require.Error(t, err)
	require.Empty(t, partial)
	require.Equal(t, "Operation failed [files.count]", en.Text("files.count", map[string]any{"Count": 1}))
	partial, err = zh.Format("files.default_count", FormatOptions{PluralCount: 2})
	require.Error(t, err)
	require.Empty(t, partial)
	partial, err = en.Format("missing", FormatOptions{})
	require.Error(t, err)
	require.Empty(t, partial)
}

func TestRenderSelectsPluralFromFaultCount(t *testing.T) {
	catalog := fstest.MapFS{
		"active.en.toml": {Data: []byte(`["files.count"]
one = "{{.Count}} argument"
other = "{{.Count}} arguments"
["legacy.count"]
other = "count {{.Count}}"
`)},
	}
	bundle, err := NewBundle(catalog)
	require.NoError(t, err)
	localizer := bundle.Localizer("en")
	definition := fault.Definition{Code: "files.count", Kind: fault.Runtime}
	require.Equal(t, "1 argument", localizer.Render(definition.New(fault.WithParams(fault.Params{"Count": 1}))))
	require.Equal(t, "2 arguments", localizer.Render(definition.New(fault.WithParams(fault.Params{"Count": 2}))))
	require.Equal(t, "count 1", localizer.Text("legacy.count", map[string]any{"Count": 1}))
}

func TestBundleLoadsRootCatalogFiles(t *testing.T) {
	catalog := fstest.MapFS{"active.en.toml": {Data: []byte("[\"sample\"]\nother = \"hello\"\n")}}
	bundle, err := NewBundle(catalog)
	require.NoError(t, err)
	require.Equal(t, "hello", bundle.Localizer("en").Text("sample", nil))
}

func TestFormatRejectsMissingIndexedMapKey(t *testing.T) {
	catalog := fstest.MapFS{"active.en.toml": {Data: []byte(`["sample"]
other = 'Hello {{index . "Name"}}'
`)}}
	bundle, err := NewBundle(catalog)
	require.NoError(t, err)
	localizer := bundle.Localizer("en")
	text, err := localizer.Format("sample", FormatOptions{Params: map[string]any{"Name": "Ada"}})
	require.NoError(t, err)
	require.Equal(t, "Hello Ada", text)
	text, err = localizer.Format("sample", FormatOptions{Params: map[string]any{}})
	require.Error(t, err)
	require.Empty(t, text)
	require.Equal(t, "Operation failed [sample]", localizer.Text("sample", map[string]any{}))
}

func TestFormatIndexesNestedMapsAndSequences(t *testing.T) {
	catalog := fstest.MapFS{"active.en.toml": {Data: []byte(`["nested"]
other = '{{index . "User" "Names" 1}}'
`)}}
	bundle, err := NewBundle(catalog)
	require.NoError(t, err)
	localizer := bundle.Localizer("en")
	params := map[string]any{"User": map[string]any{"Names": []string{"Grace", "Ada"}}}
	text, err := localizer.Format("nested", FormatOptions{Params: params})
	require.NoError(t, err)
	require.Equal(t, "Ada", text)
	params["User"] = map[string]any{"Names": []string{"Grace"}}
	text, err = localizer.Format("nested", FormatOptions{Params: params})
	require.Error(t, err)
	require.Empty(t, text)
}

func TestPluralFallbackUsesOtherWithoutHidingTemplateErrors(t *testing.T) {
	catalog := fstest.MapFS{"active.en.toml": {Data: []byte(`["fallback"]
other = "{{.Count}} items"
["incomplete"]
one = "{{.Name}} has one item"
other = "{{.Count}} items"
`)}}
	bundle, err := NewBundle(catalog)
	require.NoError(t, err)
	localizer := bundle.Localizer("en")
	text, err := localizer.Format("fallback", FormatOptions{
		Params:      map[string]any{"Count": 1},
		PluralCount: 1,
	})
	require.NoError(t, err)
	require.Equal(t, "1 items", text)
	require.Equal(t, "Operation failed [incomplete]", localizer.Text("incomplete", map[string]any{"Count": 1}))
}

func TestFormatRejectsUnavailableLanguage(t *testing.T) {
	catalog := fstest.MapFS{"active.en.toml": {Data: []byte("[\"sample\"]\nother = \"Hello\"\n")}}
	bundle, err := NewBundle(catalog)
	require.NoError(t, err)
	text, err := bundle.Localizer("zh-CN").Format("sample", FormatOptions{})
	require.Error(t, err)
	require.Empty(t, text)
}

func TestBundlePreservesReservedTOMLMessageIDs(t *testing.T) {
	for _, id := range []string{"translation", "other", "id"} {
		t.Run(id, func(t *testing.T) {
			catalog := fstest.MapFS{
				"active.en.toml": {Data: []byte("[\"" + id + "\"]\none = \"One\"\nother = \"Many\"\n")},
			}
			bundle, err := NewBundle(catalog)
			require.NoError(t, err)
			localizer := bundle.Localizer("en")
			text, err := localizer.Format(id, FormatOptions{PluralCount: 1})
			require.NoError(t, err)
			require.Equal(t, "One", text)
			text, err = localizer.Format("", FormatOptions{})
			require.Error(t, err)
			require.Empty(t, text)
		})
	}
}

// 旧 JSON 目录不能再被当作消息源加载。
func TestBundleRejectsLegacyJSONCatalog(t *testing.T) {
	catalog := fstest.MapFS{"active.en.json": {Data: []byte(`{"legacy":{"other":"obsolete"}}`)}}
	_, err := NewBundle(catalog)
	require.Error(t, err)
}
