package command

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWindowsNativeIOAndBorrowedFiles(t *testing.T) {
	call, options := windowsHelper(t, "stdio")
	options.IO.In = strings.NewReader("输入\x00字节")
	result := Capture(t.Context(), call, options, CaptureOptions{})
	if result.Err != nil || string(result.Stdout) != "输入\x00字节" || string(result.Stderr) != "诊断" {
		t.Fatalf("复制标准流: %+v", result)
	}
	options.IO.In = nil
	if err := Run(t.Context(), call, options); err != nil {
		t.Fatalf("默认 EOF/discard: %v", err)
	}
	file, err := os.Create(filepath.Join(t.TempDir(), "borrowed"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		require.NoError(t, file.Close())
	}()
	options.IO.Out = file
	options.IO.ErrOut = file
	if err = Run(t.Context(), call, options); err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteString("still open"); err != nil {
		t.Fatalf("关闭了借用句柄: %v", err)
	}
	var shared bytes.Buffer
	options.IO.Out = &shared
	options.IO.ErrOut = &shared
	options.IO.In = strings.NewReader("输入")
	if err = Run(t.Context(), call, options); err != nil {
		t.Fatal(err)
	}
	if shared.String() != "输入诊断" {
		t.Fatalf("共享输出流: %q", shared.String())
	}
}

type windowsFailWriter struct {
	err error
}

func (writer windowsFailWriter) Write([]byte) (int, error) {
	return 0, writer.err
}

func TestWindowsCopyFailureIsReturned(t *testing.T) {
	call, options := windowsHelper(t, "stdio")
	sentinel := errors.New("写入被拒绝")
	options.IO.ErrOut = windowsFailWriter{sentinel}
	if err := Run(t.Context(), call, options); !errors.Is(err, sentinel) {
		t.Fatalf("丢失借用 writer 错误: %v", err)
	}
}

func TestWindowsBorrowedInputFileRemainsOpen(t *testing.T) {
	input, err := os.Create(filepath.Join(t.TempDir(), "input"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		require.NoError(t, input.Close())
	}()
	if _, err = input.WriteString("borrowed input"); err != nil {
		t.Fatal(err)
	}
	if _, err = input.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	call, options := windowsHelper(t, "stdio")
	options.IO.In = input
	r := Capture(t.Context(), call, options, CaptureOptions{})
	if r.Err != nil || string(r.Stdout) != "borrowed input" {
		t.Fatalf("文件输入: %+v", r)
	}
	if _, err = input.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("关闭了借用输入文件: %v", err)
	}
}
