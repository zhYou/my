package command

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"golang.org/x/sys/windows"
)

// 实际创建的进程必须在 Job 内保持挂起；取消后不得执行第一条业务指令。
func TestWindowsCanceledBetweenCreationAndResume(t *testing.T) {
	baseline := processHandleCount(t)
	defer func() {
		if count := processHandleCount(t); count > baseline+4 {
			t.Errorf("取消后句柄泄漏: before=%d after=%d", baseline, count)
		}
	}()
	for range 8 {
		marker := filepath.Join(t.TempDir(), "marker")
		call, options := windowsHelper(t, "marker", marker)
		options.IO = options.IO.WithDefaults()
		child, err := createWindows(t.Context(), call.Program, call.Args, options)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		active, queryErr := jobActiveProcesses(child.job)
		_, markerErr := os.Stat(marker)
		cancel()
		err = child.wait(ctx, DefaultWaitDelay)
		if queryErr != nil || active != 1 {
			t.Fatalf("创建时未绑定 Job: active=%d err=%v", active, queryErr)
		}
		if !errors.Is(markerErr, os.ErrNotExist) {
			t.Fatalf("创建后已执行业务: %v", markerErr)
		}
		var exit *exec.ExitError
		if !errors.Is(err, context.Canceled) || !errors.As(err, &exit) {
			t.Fatalf("丢失取消/退出状态: %v", err)
		}
		if _, err = os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Resume前取消仍执行了业务: %v", err)
		}
	}
}

func TestWindowsEnvironmentAndArgumentEscaping(t *testing.T) {
	args := []string{"", `C:\path with space\`, `a\"b`, "\t", "中文", "a=b"}
	for _, explicitEmpty := range []bool{false, true} {
		call, options := windowsHelper(t, append([]string{"env"}, args...)...)
		env := options.Env[:0]
		for _, item := range options.Env {
			if !strings.HasPrefix(item, "SYSTEMROOT=") {
				env = append(env, item)
			}
		}
		env = append(env, "", "VALUE=old", "vAlUe=new", "")
		options.Env = env
		if explicitEmpty {
			options.Env = append(options.Env, "SystemRoot=")
		}
		r := Capture(t.Context(), call, options, CaptureOptions{})
		if r.Err != nil {
			t.Fatal(r.Err)
		}
		var raw []json.RawMessage
		if err := json.Unmarshal(r.Stdout, &raw); err != nil {
			t.Fatal(err)
		}
		expected := os.Getenv("SYSTEMROOT")
		if explicitEmpty {
			expected = ""
		}
		var root, value string
		var gotArgs []string
		require.NoError(t, json.Unmarshal(raw[0], &root))
		require.NoError(t, json.Unmarshal(raw[2], &value))
		require.NoError(t, json.Unmarshal(raw[3], &gotArgs))
		if root != expected || string(raw[1]) != "true" || value != "new" || !reflect.DeepEqual(gotArgs, args) {
			t.Fatalf("环境/argv: %s", r.Stdout)
		}
	}
}

func TestWindowsHandleWhitelistAndCleanup(t *testing.T) {
	event, err := windows.CreateEvent(
		&windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), InheritHandle: 1},
		0,
		0,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		require.NoError(t, windows.CloseHandle(event))
	}()
	call, options := windowsHelper(t, "handle", strconv.FormatUint(uint64(event), 10))
	if err = Run(t.Context(), call, options); err != nil {
		t.Fatalf("泄漏了无关可继承句柄: %v", err)
	}
	if status, waitErr := windows.WaitForSingleObject(
		event,
		0,
	); waitErr != nil ||
		status != uint32(windows.WAIT_TIMEOUT) {
		t.Fatalf("继承了白名单外事件: status=%d err=%v", status, waitErr)
	}
	baseline := processHandleCount(t)
	for i := range 32 {
		currentOptions := options
		if i%2 == 0 {
			currentOptions.Dir = filepath.Join(t.TempDir(), "missing-directory")
		}
		err := Run(t.Context(), call, currentOptions)
		if (i%2 == 0) != (err != nil) {
			t.Fatalf("准备失败/成功结果与预期不同: %v", err)
		}
	}
	if count := processHandleCount(t); count > baseline+4 {
		t.Fatalf("句柄泄漏: before=%d after=%d", baseline, count)
	}
}
