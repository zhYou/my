package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/base/paths"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("output unavailable")
}

func TestOutputFailureIsRuntimeError(t *testing.T) {
	var errOut bytes.Buffer
	code := Run(context.Background(), []string{"greet", "--name", "x"}, Options{
		Name:        "testcli",
		Out:         failingWriter{},
		ErrOut:      &errOut,
		ConfigDir:   t.TempDir(),
		Directories: &paths.Directories{State: t.TempDir()},
		LookupEnv: func(string) (string, bool) {
			return "", false
		},
	})
	if code != 1 || !strings.Contains(errOut.String(), "output unavailable") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
}

func TestJSONMarshalFailureLeavesOutputEmpty(t *testing.T) {
	var out bytes.Buffer
	require.Error(t, writeJSON(&out, make(chan int)))
	require.Zero(t, out.Len())
}

func TestTextRenderFailureLeavesOutputEmpty(t *testing.T) {
	var out bytes.Buffer
	require.Error(t, writeText(&out, func(w io.Writer) error {
		if _, err := io.WriteString(w, "局部结果"); err != nil {
			t.Error(err)
		}
		return errors.New("渲染失败")
	}))
	require.Zero(t, out.Len())
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	return len(p) / 2, nil
}

func TestShortWriteIsFailure(t *testing.T) {
	require.ErrorIs(t, writeJSON(shortWriter{}, true), io.ErrShortWrite)
	require.Equal(t, 1, Run(context.Background(), []string{"version", "--json"}, Options{Out: shortWriter{}}))
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) {
	return 0, errors.New("写入失败")
}

func TestHelpWriteFailureIsRuntimeError(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"help", "greet"}, {"version"}, {"schema"}, {"completion", "bash"}} {
		require.Equal(t, 1, Run(context.Background(), args, Options{Out: failedWriter{}}), "%v", args)
	}
}
