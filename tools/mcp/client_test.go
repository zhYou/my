package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServerFailuresUnblockRequests(t *testing.T) {
	bin := mcpProcessFixture(t)
	for _, mode := range []string{"missing", "exit", "idle"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "missing" {
				t.Setenv("PATH", t.TempDir())
			} else {
				t.Setenv("PATH", bin)
			}
			t.Setenv("MCP_FIXTURE_MODE", mode)
			timeout := 10 * time.Second
			if mode == "idle" {
				timeout = 100 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(t.Context(), timeout)
			client, closeClient, err := startMCP(ctx, t.TempDir(), t.TempDir())
			require.NoError(t, err)
			t.Cleanup(func() {
				cancel()
				require.Error(t, closeClient())
			})
			result := make(chan error, 1)
			go func() {
				result <- client.initialize()
			}()
			select {
			case err := <-result:
				require.Error(t, err)
				if mode == "idle" {
					require.ErrorIs(t, err, context.DeadlineExceeded)
				}
			case <-time.After(5 * time.Second):
				t.Error("服务退出或 context 截止后，请求仍阻塞")
			}
		})
	}
}

func TestClientCloseDrainsServerOutput(t *testing.T) {
	bin := mcpProcessFixture(t)
	t.Setenv("PATH", bin)
	t.Setenv("MCP_FIXTURE_MODE", "close-output")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	client, closeClient, err := startMCP(ctx, t.TempDir(), t.TempDir())
	require.NoError(t, err)
	_, err = client.request("probe", map[string]any{})
	require.NoError(t, err)
	require.NoError(t, closeClient())
}

func TestToolResultRejectsProtocolAndToolErrors(t *testing.T) {
	for _, data := range []string{`invalid`, `{"isError":true,"content":[{"type":"text","text":"backend failed"}]}`} {
		_, err := toolText(json.RawMessage(data))
		require.Error(t, err)
	}
	text, err := toolText(
		json.RawMessage(`{"content":[{"type":"text","text":"first"},{"type":"text","text":"second"}]}`),
	)
	require.NoError(t, err)
	require.Equal(t, "first\nsecond", text)
}

func TestSessionInstructionsRequireAnActualSession(t *testing.T) {
	id, err := sessionFromInstructions(
		"<session>\nYour Serena session id is `abc-123`. Pass it as the `session` parameter.\n</session>",
	)
	require.NoError(t, err)
	require.Equal(t, "abc-123", id)
	_, err = sessionFromInstructions("No project is active")
	require.Error(t, err)
}

func mcpProcessFixture(t *testing.T) string {
	t.Helper()
	bin := t.TempDir()
	source := filepath.Join(bin, "main.go")
	require.NoError(t, os.WriteFile(source, []byte(`package main
import ("encoding/json"; "io"; "os"; "strings"; "time")
func main() {
    switch os.Getenv("MCP_FIXTURE_MODE") {
    case "exit": os.Exit(7)
    case "idle": for { time.Sleep(time.Hour) }
    case "close-output":
        var request struct { ID int }
        if json.NewDecoder(os.Stdin).Decode(&request) != nil { os.Exit(2) }
        if json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc":"2.0", "id":request.ID, "result":map[string]any{}}) != nil { os.Exit(3) }
        if _, err := io.Copy(io.Discard, os.Stdin); err != nil { os.Exit(4) }
        if _, err := io.WriteString(os.Stdout, strings.Repeat("shutdown output", 16384)); err != nil { os.Exit(5) }
    }
}
`), 0o600))
	name := "mise"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	//nolint:gosec // G204：只在临时目录编译固定 MCP 进程边界 fixture。
	output, err := exec.CommandContext(t.Context(), "go", "build", "-o", filepath.Join(bin, name), source).
		CombinedOutput()
	require.NoError(t, err, "%s", output)
	return bin
}
