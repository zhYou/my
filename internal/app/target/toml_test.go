package target

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPatchTOMLPreservesFormattingAndUnmanagedFields(t *testing.T) {
	input := []byte(`# 文件头
model = "gpt-5" # 本机模型

[features]
web_search = true # 长驻设置

[permissions.development]
extends = ":workspace"
`)
	output, err := PatchTOML(input, []TOMLPatch{
		{Path: []string{"model"}, Value: stringValue("gpt-6")},
		{Path: []string{"features", "web_search"}, Value: boolValue(false)},
		{Path: []string{"features", "network_proxy"}, Value: boolValue(false)},
		{Path: []string{"permissions", "development", "network", "enabled"}, Value: boolValue(true)},
	})
	require.NoError(t, err)
	text := string(output)
	require.Contains(t, text, "# 文件头")
	require.Contains(t, text, `model = 'gpt-6' # 本机模型`)
	require.Contains(t, text, `web_search = false # 长驻设置`)
	require.Contains(t, text, "network_proxy = false")
	require.Contains(t, text, "[permissions.development.network]")
	require.Contains(t, text, "enabled = true")
	require.Contains(t, text, `extends = ":workspace"`)
}

func TestPatchTOMLDeletesOnlyTargetKey(t *testing.T) {
	input := []byte(`[tools]
node = "24" # Node 版本
go = "1.27.3"
`)
	output, err := PatchTOML(input, []TOMLPatch{
		{Path: []string{"tools", "node"}},
	})
	require.NoError(t, err)
	require.NotContains(t, string(output), "node")
	require.Contains(t, string(output), `go = "1.27.3"`)
}

func TestPatchTOMLAddsCommentForNewManagedField(t *testing.T) {
	output, err := PatchTOML([]byte("[features]\nweb_search = true\n"), []TOMLPatch{
		{
			Path:    []string{"features", "network_proxy"},
			Value:   boolValue(false),
			Comment: "不启用 Codex 网络过滤代理。",
		},
	})
	require.NoError(t, err)
	require.Contains(t, string(output), "# 不启用 Codex 网络过滤代理。\nnetwork_proxy = false")
}

func TestTOMLValueReadsNestedAndQuotedKeys(t *testing.T) {
	data := []byte(`[permissions.development.filesystem]
"~/.gradle" = "write"

[tools]
node = ["22", "24"]
`)
	value, err := TOMLValue(data, []string{"permissions", "development", "filesystem", "~/.gradle"})
	require.NoError(t, err)
	require.Equal(t, stringValue("write").Data, value.Data)

	value, err = TOMLValue(data, []string{"tools", "node"})
	require.NoError(t, err)
	require.True(t, value.Exists)
	require.NotEmpty(t, value.Digest)
}

func TestPatchTOMLRejectsInvalidDocument(t *testing.T) {
	_, err := PatchTOML([]byte("[broken\n"), []TOMLPatch{
		{Path: []string{"value"}, Value: boolValue(true)},
	})
	require.Error(t, err)
}
