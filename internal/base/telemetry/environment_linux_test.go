//go:build linux

package telemetry

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const environmentHelper = "MY_TELEMETRY_ENV_TEST"

func TestIsolatedEnvironmentHelper(t *testing.T) {
	enabled := os.Getenv(environmentHelper)
	if enabled == "" {
		return
	}
	cfg := Defaults()
	cfg.Exporter = File
	var out bytes.Buffer
	s := Start(t.Context(), cfg, Info{}, &out, func(error) {
		fmt.Fprintln(os.Stderr, "sanitized_trace_warning")
	})
	_, span := s.Tracer.Start(t.Context(), "fixture")
	fmt.Println("recording", span.IsRecording())
	span.End()
	require.NoError(t, s.Close(t.Context()))
}

func TestLocalTraceIgnoresRemoteExporterEnvironment(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "certificate")
	require.NoError(t, syscall.Mkfifo(fifo, 0o600))
	vars := []string{
		"OTEL_EXPORTER_OTLP_HEADERS=Authorization=private-fixture-header%zz",
		"OTEL_EXPORTER_OTLP_TRACES_HEADERS=Authorization=private-fixture-trace%zz",
		"OTEL_EXPORTER_OTLP_ENDPOINT=ht\ttp://private-fixture-endpoint",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=ht\ttp://private-fixture-traces",
	}
	for _, key := range []string{"OTEL_EXPORTER_OTLP_CERTIFICATE", "OTEL_EXPORTER_OTLP_TRACES_CERTIFICATE", "OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE", "OTEL_EXPORTER_OTLP_TRACES_CLIENT_CERTIFICATE", "OTEL_EXPORTER_OTLP_CLIENT_KEY", "OTEL_EXPORTER_OTLP_TRACES_CLIENT_KEY"} {
		vars = append(vars, key+"="+fifo)
	}
	output := runEnvironmentHelper(t, vars...)
	require.Contains(t, output, "recording true")
	require.NotContains(t, output, "private-fixture")
	require.NotContains(t, output, "sanitized_trace_warning")
}

func TestMalformedSDKEnvironmentDegradesWithoutRawOutput(t *testing.T) {
	for _, env := range []string{"OTEL_RESOURCE_ATTRIBUTES=private-fixture-resource", "OTEL_RESOURCE_ATTRIBUTES=key=private-fixture%zz", "OTEL_TRACES_SAMPLER=private-fixture-sampler", "OTEL_BSP_MAX_QUEUE_SIZE=private-fixture-size"} {
		t.Run(strings.SplitN(env, "=", 2)[0], func(t *testing.T) {
			output := runEnvironmentHelper(t, env)
			require.NotContains(t, output, "private-fixture")
			require.Contains(t, output, "recording false")
			require.Equal(t, 1, strings.Count(output, "sanitized_trace_warning"))
		})
	}
}

func childEnvironment(extra ...string) []string {
	var env []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "OTEL_") {
			env = append(env, entry)
		}
	}
	return append(env, extra...)
}

func runEnvironmentHelper(t *testing.T, extra ...string) string {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	// #nosec G204 -- 在独立测试进程内捕获 SDK 真正的 stderr，不修改父进程全局 logger 或环境。
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestIsolatedEnvironmentHelper$")
	cmd.Env = childEnvironment(append(extra, environmentHelper+"=1")...)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	return string(output)
}
