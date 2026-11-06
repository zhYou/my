package cli

import (
	"io"
	"testing"

	"github.com/zhYou/my/internal/locales"
)

func BenchmarkBuildRoot(b *testing.B) {
	opts := isolatedOptions(b.TempDir(), io.Discard, io.Discard)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, _, err := buildRoot(opts, locales.LoadBundle); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRunGreet(b *testing.B) {
	opts := isolatedOptions(b.TempDir(), io.Discard, io.Discard)
	args := []string{"greet", "--name", "Codex", "--json"}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if code := Run(b.Context(), args, opts); code != 0 {
			b.Fatalf("退出码=%d", code)
		}
	}
}
