package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type archiveMember struct {
	name string
	data []byte
	mode os.FileMode
}

func releaseFixture(t *testing.T) string {
	t.Helper()
	return releaseFixtureMode(t, "", "")
}

func releaseFixtureMode(t *testing.T, selected, mode string) string {
	t.Helper()
	dir := t.TempDir()
	var sums strings.Builder
	for _, target := range []string{"linux_amd64", "linux_arm64", "darwin_amd64", "darwin_arm64", "windows_amd64", "windows_arm64"} {
		ext := ".tar.gz"
		if strings.HasPrefix(target, "windows") {
			ext = ".zip"
		}
		name := fmt.Sprintf("%s_0.1.0_%s%s", appName, target, ext)
		doc := map[string]any{
			"spdxVersion":       "SPDX-2.3",
			"SPDXID":            "SPDXRef-DOCUMENT",
			"name":              name,
			"documentNamespace": "https://example.com/" + name,
			"creationInfo": map[string]any{
				"creators": []string{"Tool: syft"},
				"created":  "2026-10-29T00:00:00Z",
			},
			"packages": []any{map[string]any{"SPDXID": "SPDXRef-root", "name": name}},
		}
		sbom, err := json.Marshal(doc)
		require.NoError(t, err)
		binary := appName
		windows := strings.HasPrefix(target, "windows")
		if windows {
			binary += ".exe"
		}
		archiveMode := "valid"
		if target == selected {
			archiveMode = mode
		}
		archive := releaseArchiveBytes(t, windows, binary, archiveMode)
		for file, data := range map[string][]byte{name: archive, name + ".spdx.json": sbom} {
			require.NoError(t, os.WriteFile(filepath.Join(dir, file), data, 0o600))
			sum := sha256.Sum256(data)
			fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(sum[:]), file)
		}
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(sums.String()), 0o600))
	return dir
}

func releaseArchiveBytes(t *testing.T, windows bool, binary, mode string) []byte {
	t.Helper()
	if mode == "corrupt" {
		return []byte("损坏归档")
	}
	members := []archiveMember{
		{binary, []byte("测试二进制"), 0o755},
		{"README.md", []byte("说明"), 0o644},
		{"config.example.toml", []byte("[logging]\nlevel = \"info\"\n"), 0o644},
	}
	switch mode {
	case "binary":
		members = members[1:]
	case "readme":
		members = append(members[:1], members[2:]...)
	case "config":
		members = members[:2]
	case "empty":
		members[2].data = nil
	case "parent":
		members[2].name = "configs/config.example.toml"
	case "duplicate":
		members = append(members, members[2])
	case "symlink":
		members[2].mode = os.ModeSymlink | 0o644
	case "dot-alias":
		members = append(members, archiveMember{"./config.example.toml", nil, 0o644})
	case "parent-alias":
		members = append(members, archiveMember{"sub/../config.example.toml", nil, 0o644})
	case "windows-alias":
		members = append(members, archiveMember{`.\config.example.toml`, nil, 0o644})
	}
	var out bytes.Buffer
	if windows {
		writer := zip.NewWriter(&out)
		for _, member := range members {
			header := &zip.FileHeader{Name: member.name, Method: zip.Deflate}
			header.SetMode(member.mode)
			file, err := writer.CreateHeader(header)
			require.NoError(t, err)
			_, err = file.Write(member.data)
			require.NoError(t, err)
		}
		require.NoError(t, writer.Close())
	} else {
		compressed := gzip.NewWriter(&out)
		writer := tar.NewWriter(compressed)
		for _, member := range members {
			header := &tar.Header{
				Name:     member.name,
				Mode:     int64(member.mode.Perm()),
				Size:     int64(len(member.data)),
				Typeflag: tar.TypeReg,
			}
			if member.mode&os.ModeSymlink != 0 {
				header.Typeflag = tar.TypeSymlink
				header.Linkname = "outside"
				header.Size = 0
			}
			require.NoError(t, writer.WriteHeader(header))
			if header.Typeflag == tar.TypeReg {
				_, err := writer.Write(member.data)
				require.NoError(t, err)
			}
		}
		require.NoError(t, writer.Close())
		require.NoError(t, compressed.Close())
	}
	return out.Bytes()
}
