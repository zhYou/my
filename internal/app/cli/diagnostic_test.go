package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/zhYou/my/internal/app/config"
	"github.com/zhYou/my/internal/app/doctor"
	"github.com/zhYou/my/internal/base/logging"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/app/greeting"
	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/base/messages"
)

// 移除结构化错误出口、错误覆盖JSON预选或泄漏原始输入都会使此测试失败。
func TestJSONDiagnosticsCoverEarlyFailures(t *testing.T) {
	for _, args := range [][]string{
		{"--json", "unknown"},
		{"unknown", "--json"},
		{"greet", "--json"},
		{"greet", "--unknown", "--json"},
		{"--json", "--lang", "invalid", "greet"},
		{"greet", "--name", "fixture", "--config", "absent.toml", "--json"},
	} {
		code, out, stderr := invoke(t, args...)
		require.NotZero(t, code)
		require.Empty(t, out)
		var record struct {
			Type          string
			ExitCode      int `json:"exit_code"`
			SchemaVersion int `json:"schema_version"`
			Diagnostics   []struct {
				Code  string
				Phase string
			}
		}
		require.NoError(t, json.Unmarshal([]byte(stderr), &record), stderr)
		require.Equal(t, "error", record.Type)
		require.Equal(t, code, record.ExitCode)
		require.Equal(t, 1, record.SchemaVersion)
		require.NotEmpty(t, record.Diagnostics)
		require.NotEmpty(t, record.Diagnostics[0].Code)
		require.NotEmpty(t, record.Diagnostics[0].Phase)
	}
}

func TestJSONDiagnosticModeRespectsValuesAndSeparator(t *testing.T) {
	for _, tc := range []struct {
		args []string
		json bool
	}{
		{[]string{"--json", "--unknown", "--json=false"}, false},
		{[]string{"--json=false", "--unknown", "--json"}, true},
		{[]string{"greet", "--name", "--json", "extra"}, false},
		{[]string{"greet", "--", "--json"}, false},
		{[]string{"--json", "--json=invalid"}, true},
	} {
		_, _, stderr := invoke(t, tc.args...)
		require.Equal(t, tc.json, json.Valid([]byte(stderr)), stderr)
	}
}

func TestJSONPartialCompletionPreservesDiagnosticsAndCause(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(strconv.FormatBool(cancelled), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			home := t.TempDir()
			marker := filepath.Join(home, "saved")
			cause := errors.New("private-fixture-token")
			var failure error
			var out, stderr bytes.Buffer
			code := Run(ctx, []string{"greet", "--name", "fixture", "--json", "--lang", "zh-CN"}, Options{
				Out:       &out,
				ErrOut:    &stderr,
				LookupEnv: func(key string) (string, bool) { return home, key == "MY_HOME" },
				Dependencies: &Dependencies{Greet: func(context.Context, greeting.Options) (greeting.Result, error) {
					require.NoError(t, os.WriteFile(marker, []byte("saved"), 0o600))
					if cancelled {
						cancel()
						cause = errors.Join(cause, ctx.Err())
					}
					failure = (fault.Definition{Code: "fixture.partial", Kind: fault.Runtime}).Wrap(
						cause,
						fault.WithSensitiveCause(),
						fault.WithDiagnostic(
							fault.Diagnostic{
								Phase:            "fixture.publish",
								CompletedEffects: []string{"fixture.saved"},
								Recovery:         &fault.Recovery{Code: "config.recover"},
							},
						),
					)
					return greeting.Result{}, failure
				}},
			})
			want := 1
			if cancelled {
				want = 130
			}
			require.Equal(t, want, code)
			require.FileExists(t, marker)
			require.ErrorIs(t, failure, cause)
			require.Empty(t, out.String())
			require.NotContains(t, stderr.String(), "private-fixture-token")
			var record struct {
				Type        string
				RunID       string `json:"run_id"`
				Diagnostics []struct {
					Code      string
					Phase     string
					Completed []string `json:"completed_effects"`
					Recovery  struct {
						Code    string
						Message string
					}
				}
			}
			require.NoError(t, json.Unmarshal(stderr.Bytes(), &record))
			require.Equal(t, "error", record.Type)
			require.NotEmpty(t, record.RunID)
			require.Equal(t, "fixture.publish", record.Diagnostics[0].Phase)
			require.Equal(t, []string{"fixture.saved"}, record.Diagnostics[0].Completed)
			require.Equal(t, "config.recover", record.Diagnostics[0].Recovery.Code)
			require.Contains(t, record.Diagnostics[0].Recovery.Message, "重试")
			for _, name := range []string{"events.jsonl", "trace.jsonl"} {
				files, err := filepath.Glob(filepath.Join(home, "state", "logs", "*", name))
				require.NoError(t, err)
				for _, file := range files {
					// #nosec G304 -- 路径来自本测试独占临时目录内的诊断文件清单。
					data, err := os.ReadFile(file)
					require.NoError(t, err)
					require.NotContains(t, string(data), "private-fixture-token")
				}
			}
		})
	}
}

func TestBootstrapFailureUsesJSONFallback(t *testing.T) {
	var out, stderr bytes.Buffer
	code := run(
		t.Context(),
		[]string{"--json", "greet", "--name", "fixture"},
		Options{Out: &out, ErrOut: &stderr, LookupEnv: noFixtureEnv},
		func() (*messages.Bundle, error) { return nil, errors.New("private-catalog-data") },
	)
	require.Equal(t, 1, code)
	require.Empty(t, out.String())
	require.True(t, json.Valid(stderr.Bytes()), stderr.String())
	require.NotContains(t, stderr.String(), "private-catalog-data")
}

func TestJSONCancellationIsRuntimeRatherThanInvalidUsage(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	code, out, stderr := invokeContext(ctx, t, "--json", "greet", "--name", "fixture")
	require.Equal(t, 130, code)
	require.Empty(t, out)
	var record struct {
		Diagnostics []struct {
			Code string
			Kind string
		}
	}
	require.NoError(t, json.Unmarshal([]byte(stderr), &record))
	require.Equal(t, "cli.cancelled", record.Diagnostics[0].Code)
	require.Equal(t, "runtime", record.Diagnostics[0].Kind)
}

func TestRuntimeFallbackSharedWithLogsAndTrace(t *testing.T) {
	home := t.TempDir()
	var out, stderr bytes.Buffer
	code := Run(
		t.Context(),
		[]string{"--json", "greet", "--name", "fixture"},
		Options{Out: &out, ErrOut: &stderr, LookupEnv: func(key string) (string, bool) {
			if key == "MY_TELEMETRY_EXPORTER" {
				return "file", true
			}
			return home, key == "MY_HOME"
		}, Dependencies: &Dependencies{Greet: func(context.Context, greeting.Options) (greeting.Result, error) {
			return greeting.Result{}, errors.New("fixture failure")
		}}},
	)
	require.Equal(t, 1, code)
	require.Contains(t, stderr.String(), `"code":"cli.runtime"`)
	for _, name := range []string{"events.jsonl", "trace.jsonl"} {
		files, err := filepath.Glob(filepath.Join(home, "state", "logs", "*", name))
		require.NoError(t, err)
		require.Len(t, files, 1)
		// #nosec G304 -- 文件来自本测试独占的临时诊断目录。
		data, err := os.ReadFile(files[0])
		require.NoError(t, err)
		require.Contains(t, string(data), "cli.runtime")
		require.Contains(t, string(data), "greeting.run")
	}
}

func TestDiagnosticWarningsUseSynchronizedInvocationState(t *testing.T) {
	for range 16 {
		done := make(chan struct{})
		code := Run(
			t.Context(),
			[]string{"greet", "--name", "fixture", "--json"},
			Options{Out: io.Discard, ErrOut: io.Discard, LookupEnv: noFixtureEnv, Dependencies: &Dependencies{
				LoadConfig: func(context.Context, config.Options) (config.Config, error) {
					cfg := config.Defaults()
					cfg.Logging.Output = "none"
					return cfg, nil
				},
				NewLogger: func(_ logging.RunInfo, _ logging.Options, warn func(error)) *logging.Logger {
					go func() { defer close(done); warn(errors.New("fixture warning")) }()
					return logging.NewNop()
				},
				Greet: func(context.Context, greeting.Options) (greeting.Result, error) {
					<-done
					return greeting.Result{Message: "fixture"}, nil
				},
			}},
		)
		require.Zero(t, code)
	}
}

func TestDoctorExecutionFailureHasExecutionPhase(t *testing.T) {
	var out, stderr bytes.Buffer
	code := Run(
		t.Context(),
		[]string{"doctor", "--json"},
		Options{
			Out:       &out,
			ErrOut:    &stderr,
			LookupEnv: noFixtureEnv,
			Dependencies: &Dependencies{Doctor: func(context.Context, doctor.Options) (doctor.Result, error) {
				return doctor.Result{}, errors.New("probe failed")
			}},
		},
	)
	require.Equal(t, 1, code)
	require.Empty(t, out.String())
	var record diagnosticRecord
	require.NoError(t, json.Unmarshal(stderr.Bytes(), &record))
	require.Equal(t, "doctor.run", record.Diagnostics[0].Phase)
	require.Nil(t, record.Diagnostics[0].CompletedEffects)
}

func TestMixedJoinedErrorsKeepKnownBranchAndOriginalIdentity(t *testing.T) {
	partial := (fault.Definition{Code: "fixture.partial", Kind: fault.Runtime}).New(
		fault.WithDiagnostic(
			fault.Diagnostic{
				Phase:            "fixture.publish",
				CompletedEffects: []string{"fixture.saved"},
				Recovery:         &fault.Recovery{Code: "config.recover"},
			},
		),
	)
	original := errors.Join(fault.New(fault.Runtime, "first failure"), partial)
	classified := classifyExecutionError(original, "execute")
	require.ErrorIs(t, classified, original)
	require.ErrorIs(t, classified, partial)
	branches := fault.Details(classified)
	require.Len(t, branches, 2)
	require.Equal(t, fault.Code("cli.runtime"), branches[0].Code)
	require.Equal(t, fault.Code("fixture.partial"), branches[1].Code)
	diagnostic, ok := fault.DiagnosticOf(branches[1])
	require.True(t, ok)
	require.Equal(t, "fixture.publish", diagnostic.Phase)
	require.Equal(t, []string{"fixture.saved"}, diagnostic.CompletedEffects)
	require.Equal(t, "config.recover", diagnostic.Recovery.Code)
}

func TestCompletionFlagValuesDoNotSelectJSONDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		args []string
		json bool
	}{
		{[]string{"__complete", "greet", "--name", "--json", ""}, false},
		{[]string{"--json", "__complete", "greet", "--name", "fixture", ""}, true},
	} {
		var out, stderr bytes.Buffer
		code := run(
			t.Context(),
			tc.args,
			Options{Out: &out, ErrOut: &stderr, LookupEnv: noFixtureEnv},
			func() (*messages.Bundle, error) { return nil, errors.New("private bootstrap data") },
		)
		require.Equal(t, 1, code)
		require.Empty(t, out.String())
		require.Equal(t, tc.json, json.Valid(stderr.Bytes()), stderr.String())
		require.NotContains(t, stderr.String(), "private bootstrap data")
	}
}

func TestAdoptedDiagnosticContractHandlesEarlyUsageError(t *testing.T) {
	code, out, stderr := invoke(t, "greet", "--json")
	require.Equal(t, 2, code)
	require.Empty(t, out)
	var record struct {
		Type        string `json:"type"`
		ExitCode    int    `json:"exit_code"`
		Diagnostics []struct {
			Phase string `json:"phase"`
		} `json:"diagnostics"`
	}
	require.NoError(t, json.Unmarshal([]byte(stderr), &record))
	require.Equal(t, "error", record.Type)
	require.Equal(t, 2, record.ExitCode)
	require.NotEmpty(t, record.Diagnostics)
	require.NotEmpty(t, record.Diagnostics[0].Phase)
}
