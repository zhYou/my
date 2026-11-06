package cli

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
)

func FuzzArgv(f *testing.F) {
	for _, seed := range []struct {
		args   []string
		cancel bool
	}{
		{nil, false},
		{[]string{""}, false},
		{[]string{" "}, false},
		{[]string{"greet", "--name", ""}, false},
		{[]string{"greet", "--name", " "}, false},
		{[]string{"greet", "--name", "你好 世界", "--json"}, false},
		{[]string{"greet", "--", "--name", "x"}, false},
		{[]string{"greet", "--name", "--lang"}, false},
		{[]string{"greet", "--name", "x"}, true},
		{[]string{"schema"}, true},
		{[]string{"completion", "bash"}, false},
	} {
		encoded := ""
		if len(seed.args) > 0 {
			encoded = strings.Join(seed.args, "\x00") + "\x00"
		}
		f.Add(encoded, seed.cancel)
	}
	home := f.TempDir()
	f.Setenv("MY_HOME", home)
	f.Fuzz(func(t *testing.T, raw string, cancelled bool) {
		if len(raw) > 4096 {
			t.Skip()
		}
		var args []string
		if raw != "" {
			args = strings.Split(strings.TrimSuffix(raw, "\x00"), "\x00")
		}
		if len(args) > 64 {
			t.Skip()
		}
		var out, stderr bytes.Buffer
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		if cancelled {
			cancel()
		}
		code := Run(ctx, args, isolatedOptions(home, &out, &stderr))
		switch code {
		case 0, 1, 2, 130:
		default:
			t.Fatalf("非契约退出码 %d，argv=%q", code, args)
		}
		if code != 0 && out.Len() != 0 {
			t.Fatalf("失败时 stdout 非空：code=%d argv=%q output=%q", code, args, out.String())
		}
		if code != 0 && stderr.Len() == 0 {
			t.Fatalf("失败时缺少诊断：code=%d argv=%q", code, args)
		}
		entries, err := os.ReadDir(home)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("隔离注入仍产生状态：%v", entries)
		}
	})
}
