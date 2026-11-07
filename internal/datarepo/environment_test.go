package datarepo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAPIEnvironmentIsolation(t *testing.T) {
	if os.Getenv("MY_TEST_API_CHILD") == "1" {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			//nolint:errcheck // 客户端可主动断开。
			_, _ = w.Write([]byte("private-response-marker"))
		}))
		defer srv.Close()
		c, err := New(
			t.Context(),
			Config{Repository: "owner/data", Branch: "main", RequestTimeout: time.Second},
			Options{
				Token:     func(context.Context) (string, error) { return "private-token-marker", nil },
				Transport: routeTransport{srv.URL},
			},
		)
		require.NoError(t, err)
		_, err = c.Read(t.Context())
		require.Error(t, err)
		require.NotContains(t, err.Error(), "private-")
		return
	}
	binary, err := os.Executable()
	require.NoError(t, err)
	child := exec.CommandContext(
		t.Context(),
		binary,
		"-test.run=^TestAPIEnvironmentIsolation$",
	) // #nosec G204 -- 当前测试二进制与固定参数。
	env := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GH_") && !strings.HasPrefix(entry, "MY_") {
			env = append(env, entry)
		}
	}
	env = append(env, "MY_TEST_API_CHILD=1", "MY_HOME="+t.TempDir(), "GH_CONFIG_DIR="+t.TempDir(), "GH_DEBUG=api")
	child.Env = env
	output, err := child.CombinedOutput()
	require.NoError(t, err, string(output))
	require.NotContains(t, string(output), "private-token-marker")
	require.NotContains(t, string(output), "private-response-marker")
	require.NotContains(t, string(output), "Authorization:")
}
