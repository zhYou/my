package command

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPathUsesInvocationEnvironment(t *testing.T) {
	call, options := helper(t, "bytes")
	name := "helper" + filepath.Ext(call.Program)
	data, err := os.ReadFile(call.Program)
	if err != nil {
		t.Fatal(err)
	}
	//nolint:gosec // G306：临时测试目录或可执行 fixture 明确模拟项目文件权限，不保存用户数据。
	if err = os.WriteFile(filepath.Join(options.Dir, name), data, 0o700); err != nil {
		t.Fatal(err)
	}
	call.Program = name
	options.Env = append(options.Env, "PATH="+options.Dir)
	if r := Capture(t.Context(), call, options, CaptureOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	options.Env = append(options.Env, "PATH=")
	if err := Run(t.Context(), call, options); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("空 PATH: %v", err)
	}
}

func TestErrDot(t *testing.T) {
	call, options := helper(t, "bytes")
	// Windows 工作区和 go-build 临时目录可能不在同一卷，不能计算两者的相对路径。
	// 相对 PATH 以父进程 cwd 为基准；在真实测试二进制目录内命中它，测试结束自动恢复。
	t.Chdir(filepath.Dir(call.Program))
	options.Env = append(options.Env, "PATH=.")
	call.Program = filepath.Base(call.Program)
	var output bytes.Buffer
	options.IO.Out, options.IO.ErrOut = &output, &output
	if err := Run(t.Context(), call, options); !errors.Is(err, exec.ErrDot) {
		t.Fatalf("需要 ErrDot: %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("拒绝相对 PATH 前执行了 helper: %q", output.Bytes())
	}
}

func TestWindowsExtensionsUseOnlyInvocationEnvironment(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "helper")
	if err := os.WriteFile(base+".tool", []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := resolveWindowsExecutable(base, []string{"PATHEXT=exe;;tool;"})
	if err != nil || got != base+".tool" {
		t.Fatalf("本次 PATHEXT 未生效：%q %v", got, err)
	}
	if _, err = resolveWindowsExecutable(base, []string{"PATHEXT=.exe"}); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("不应使用其他扩展 %v", err)
	}
	got, err = resolveWindowsExecutable(base+".tool", []string{"PATHEXT=.exe"})
	if err != nil || got != base+".tool" {
		t.Fatalf("显式扩展被改写 %q %v", got, err)
	}
}
