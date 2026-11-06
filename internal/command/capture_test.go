package command

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCapturePreservesArgsEnvironmentDirectoryAndBytes(t *testing.T) {
	call, options := helper(t, "echo", "", "a b", "$HOME", "\"quoted\"")
	options.Env = append(options.Env, "VALUE=old", "VALUE=new")
	result := Capture(t.Context(), call, options, CaptureOptions{})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	var data []json.RawMessage
	if err := json.Unmarshal(result.Stdout, &data); err != nil {
		t.Fatal(err)
	}
	var args []string
	require.NoError(t, json.Unmarshal(data[0], &args))
	if !reflect.DeepEqual(args, call.Args[3:]) {
		t.Fatalf("argv %q", args)
	}
	if string(data[1]) != `"new"` {
		t.Fatalf("env/cwd %s", result.Stdout)
	}
	var directory string
	if err := json.Unmarshal(data[2], &directory); err != nil {
		t.Fatal(err)
	}
	actual, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.Stat(options.Dir)
	if err != nil {
		t.Fatal(err)
	}
	// macOS 的 /var 与 /private/var 等路径别名允许不同拼写，但必须指向同一目录。
	if !os.SameFile(actual, expected) {
		t.Fatalf("工作目录不同: got=%q want=%q", directory, options.Dir)
	}
	call.Args = []string{"-test.run=^TestHelperProcess$", "--", "bytes"}
	result = Capture(t.Context(), call, options, CaptureOptions{})
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	if !bytes.Equal(result.Stdout, []byte{0, 255, '\n'}) || !bytes.Equal(result.Stderr, []byte{254, 0, 'x'}) {
		t.Fatalf("原始输出 %q %q", result.Stdout, result.Stderr)
	}
	original := append([]string(nil), options.Env...)
	updated := options.WithEnv("VALUE=other")
	updated.Env[0] = "mutated"
	if !reflect.DeepEqual(original, options.Env) {
		t.Fatal("共享环境切片")
	}
}

func TestCaptureDrainsAfterLimitAndPreservesExitError(t *testing.T) {
	call, options := helper(t, "large")
	r := Capture(t.Context(), call, options, CaptureOptions{Limit: 31})
	if r.Err != nil || len(r.Stdout) != 31 || len(r.Stderr) != 31 || !r.StdoutTruncated || !r.StderrTruncated {
		t.Fatalf("截断结果 %+v", r)
	}
	call, options = helper(t, "exit")
	r = Capture(t.Context(), call, options, CaptureOptions{})
	var exit *exec.ExitError
	if !errors.As(r.Err, &exit) || r.ExitCode != 7 || r.Status != Failed {
		t.Fatalf("失败结果 %+v", r)
	}
}
