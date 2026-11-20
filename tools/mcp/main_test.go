package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEntryRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{"extra"}} {
		require.ErrorContains(t, run(args), "mise run mcp:check")
	}
}

func TestRunMCPChecksRunsConcurrentlyAndJoinsErrors(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	checks := []mcpCheck{
		{
			name: "first",
			run: func(context.Context, string, io.Writer) error {
				started <- struct{}{}
				<-release
				return errors.New("first failed")
			},
		},
		{
			name: "second",
			run: func(context.Context, string, io.Writer) error {
				started <- struct{}{}
				<-release
				return errors.New("second failed")
			},
		},
	}
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	root := t.TempDir()
	go func() {
		done <- runMCPChecks(ctx, root, checks)
	}()
	for range checks {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			cancel()
			select {
			case err := <-done:
				if checkErr, ok := errors.AsType[*mcpCheckError](err); ok {
					require.NoError(t, os.RemoveAll(checkErr.logDir))
				}
				if err == nil {
					t.Fatal("MCP 验收未并行启动但未返回预期错误")
				}
			case <-time.After(time.Second):
				t.Fatal("MCP 验收取消后未完成收尾")
			}
			t.Fatal("MCP 验收未并行启动")
		}
	}
	close(release)
	err := <-done
	var checkErr *mcpCheckError
	require.ErrorAs(t, err, &checkErr)
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(checkErr.logDir))
	})
	require.ErrorContains(t, err, "验收 first：first failed")
	require.ErrorContains(t, err, "验收 second：second failed")
}

func TestRunMCPChecksPreservesFailureLogs(t *testing.T) {
	checks := []mcpCheck{{
		name: "fixture check",
		run: func(_ context.Context, _ string, log io.Writer) error {
			_, err := io.WriteString(log, "fixture detail")
			return errors.Join(err, errors.New("fixture failed"))
		},
	}}

	err := runMCPChecks(context.Background(), t.TempDir(), checks)
	var checkErr *mcpCheckError
	require.ErrorAs(t, err, &checkErr)
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(checkErr.logDir))
	})
	require.DirExists(t, checkErr.logDir)
	entries, err := os.ReadDir(checkErr.logDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	data, err := os.ReadFile(filepath.Join(checkErr.logDir, entries[0].Name()))
	require.NoError(t, err)
	require.Contains(t, string(data), "fixture detail")
}

func TestRunMCPChecksRemovesLogsOnSuccess(t *testing.T) {
	tempParent := t.TempDir()
	t.Setenv("TMPDIR", tempParent)
	t.Setenv("TMP", tempParent)
	t.Setenv("TEMP", tempParent)
	checks := []mcpCheck{{
		name: "fixture check",
		run: func(_ context.Context, _ string, log io.Writer) error {
			_, err := io.WriteString(log, "successful detail")
			return err
		},
	}}

	require.NoError(t, runMCPChecks(context.Background(), t.TempDir(), checks))
	entries, err := os.ReadDir(tempParent)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestRunMCPChecksKeepsSlowCheckLogOpen(t *testing.T) {
	checks := []mcpCheck{
		{
			name: "slow failure",
			run: func(_ context.Context, _ string, log io.Writer) error {
				time.Sleep(50 * time.Millisecond)
				_, err := io.WriteString(log, "late detail")
				return errors.Join(err, errors.New("slow failed"))
			},
		},
		{
			name: "fast failure",
			run: func(context.Context, string, io.Writer) error {
				return errors.New("fast failed")
			},
		},
	}

	err := runMCPChecks(context.Background(), t.TempDir(), checks)
	var checkErr *mcpCheckError
	require.ErrorAs(t, err, &checkErr)
	t.Cleanup(func() {
		require.NoError(t, os.RemoveAll(checkErr.logDir))
	})
	entries, err := os.ReadDir(checkErr.logDir)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	var contents strings.Builder
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(checkErr.logDir, entry.Name()))
		require.NoError(t, err)
		contents.Write(data)
	}
	require.Contains(t, contents.String(), "late detail")
}
