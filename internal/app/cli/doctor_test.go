package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zhYou/my/internal/app/config"
	"github.com/zhYou/my/internal/app/doctor"
	"github.com/zhYou/my/internal/base/messages"

	"github.com/stretchr/testify/require"
)

func TestDoctorEmptyReportAndDiscovery(t *testing.T) {
	code, out, stderr := invoke(t, "doctor", "--json")
	require.Zero(t, code, stderr)
	require.Empty(t, stderr)
	require.JSONEq(t, `{"required_available":true,"checks":[]}`, out)
	for _, lang := range []string{"en", "zh-CN"} {
		code, out, stderr = invoke(t, "--lang", lang, "doctor")
		require.Zero(t, code, stderr)
		require.Empty(t, stderr)
		if lang == "en" {
			require.Contains(t, out, "No checks")
		} else {
			require.Contains(t, out, "尚未配置检查")
		}
		code, out, stderr = invoke(t, "--lang", lang, "doctor", "--help")
		require.Zero(t, code, stderr)
		require.Empty(t, stderr)
		require.Contains(t, out, "testcli doctor --json")
	}
	code, out, stderr = invoke(t, "schema", "doctor")
	require.Zero(t, code, stderr)
	var schema struct {
		Commands []commandSchema `json:"commands"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &schema))
	require.Len(t, schema.Commands, 1)
	require.Contains(t, string(schema.Commands[0].ResultSchema), "required_available")
	code, out, stderr = invoke(t, "doctor", "extra")
	require.Equal(t, 2, code)
	require.Empty(t, out)
	require.NotEmpty(t, stderr)
}

func TestDoctorCompletedFailureKeepsReport(t *testing.T) {
	for _, asJSON := range []bool{false, true} {
		for _, required := range []bool{false, true} {
			var out, stderr bytes.Buffer
			args := []string{"doctor"}
			if asJSON {
				args = append(args, "--json")
			}
			code := Run(context.Background(), args, Options{
				Out:       &out,
				ErrOut:    &stderr,
				LookupEnv: noFixtureEnv,
				Dependencies: &Dependencies{Doctor: func(context.Context, doctor.Options) (doctor.Result, error) {
					return doctor.Result{
						RequiredAvailable: !required,
						Checks: []doctor.Check{
							{Name: "fixture", Status: doctor.StatusFailed, Detail: "unavailable", Required: required},
						},
					}, nil
				}},
			})
			want := 0
			if required {
				want = 1
			}
			require.Equal(t, want, code)
			require.Empty(t, stderr.String())
			require.Contains(t, out.String(), "fixture")
			if asJSON {
				require.True(t, json.Valid(out.Bytes()))
				require.Contains(t, out.String(), `"checks":[`)
			}
		}
	}
}

func TestDoctorFailuresDoNotCommitOutput(t *testing.T) {
	for _, failure := range []error{errors.New("execution failed"), context.Canceled} {
		var out, stderr bytes.Buffer
		code := Run(context.Background(), []string{"doctor", "--json"}, Options{
			Out:       &out,
			ErrOut:    &stderr,
			LookupEnv: noFixtureEnv,
			Dependencies: &Dependencies{Doctor: func(context.Context, doctor.Options) (doctor.Result, error) {
				return doctor.Result{RequiredAvailable: false}, failure
			}},
		})
		want := 1
		if errors.Is(failure, context.Canceled) {
			want = 130
		}
		require.Equal(t, want, code)
		require.Empty(t, out.String())
		require.NotEmpty(t, stderr.String())
	}
	bundle := catalogFixture(t, "en", "doctor.empty", `other = "{{.Missing}}"`+"\n")
	var out, stderr bytes.Buffer
	code := run(
		context.Background(),
		[]string{"doctor"},
		Options{Out: &out, ErrOut: &stderr, LookupEnv: noFixtureEnv},
		func() (*messages.Bundle, error) {
			return bundle, nil
		},
	)
	require.Equal(t, 1, code)
	require.Empty(t, out.String())
	require.NotEmpty(t, stderr.String())
}

func TestDoctorDoesNotReadConfigCreateStateOrRunForSchema(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	for _, args := range [][]string{{"doctor", "--json"}, {"doctor", "--help"}, {"schema", "doctor"}} {
		var out, stderr bytes.Buffer
		code := Run(
			context.Background(),
			append([]string{"--config", filepath.Join(root, "missing.toml")}, args...),
			Options{
				Name:   "sample",
				Out:    &out,
				ErrOut: &stderr,
				LookupEnv: func(key string) (string, bool) {
					return root, key == "SAMPLE_HOME"
				},
				Dependencies: &Dependencies{LoadConfig: func(context.Context, config.Options) (config.Config, error) {
					t.Fatal("doctor must not load config")
					return config.Config{}, nil
				}},
			},
		)
		require.Zero(t, code, stderr.String())
		require.Empty(t, stderr.String())
		_, err := os.Stat(root)
		require.ErrorIs(t, err, os.ErrNotExist)
	}
	var out bytes.Buffer
	code := Run(context.Background(), []string{"schema", "doctor"}, Options{
		Out:       &out,
		LookupEnv: noFixtureEnv,
		Dependencies: &Dependencies{Doctor: func(context.Context, doctor.Options) (doctor.Result, error) {
			t.Fatal("schema must not execute doctor")
			return doctor.Result{}, nil
		}},
	})
	require.Zero(t, code)
	var stderr bytes.Buffer
	code = Run(context.Background(), []string{"doctor", "--json"}, Options{
		Out:       failingWriter{},
		ErrOut:    &stderr,
		LookupEnv: noFixtureEnv,
	})
	require.Equal(t, 1, code)
	require.NotEmpty(t, stderr.String())
}

func TestDoctorReadOnlyOptInNetworkAndTimeoutFixture(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(home, "input")
	require.NoError(t, os.WriteFile(file, []byte("private-fixture-content"), 0o600))
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/slow" {
			<-r.Context().Done()
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	for _, tc := range []struct {
		network  bool
		required bool
		path     string
		code     int
	}{
		{false, true, "/ok", 0}, {true, true, "/ok", 0}, {true, false, "/slow", 0}, {true, true, "/slow", 1},
	} {
		var out, stderr bytes.Buffer
		before := calls.Load()
		code := Run(
			t.Context(),
			[]string{"doctor", "--json"},
			Options{
				Out:       &out,
				ErrOut:    &stderr,
				LookupEnv: noFixtureEnv,
				Dependencies: &Dependencies{
					Doctor: fixtureDoctor(file, server.URL+tc.path, tc.network, tc.required, 30*time.Millisecond),
				},
			},
		)
		require.Equal(t, tc.code, code, stderr.String())
		require.Empty(t, stderr.String())
		require.True(t, json.Valid(out.Bytes()))
		require.NotContains(t, out.String(), "private-fixture")
		if !tc.network {
			require.Equal(t, before, calls.Load())
		}
		// #nosec G304 -- file 是本测试创建并持有的临时输入。
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		require.Equal(t, "private-fixture-content", string(data))
		entries, err := os.ReadDir(home)
		require.NoError(t, err)
		require.Len(t, entries, 1)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var out, stderr bytes.Buffer
	code := Run(
		ctx,
		[]string{"doctor", "--json"},
		Options{
			Out:          &out,
			ErrOut:       &stderr,
			LookupEnv:    noFixtureEnv,
			Dependencies: &Dependencies{Doctor: fixtureDoctor(file, server.URL, true, true, time.Second)},
		},
	)
	require.Equal(t, 130, code)
	require.Empty(t, out.String())
	require.True(t, json.Valid(stderr.Bytes()))
}

// fixtureDoctor 展示派生项目的最小接线；只读文件，联网由调用者显式允许。
// 探测超时属于完成的负向报告，父取消属于执行失败；不输出原始文件、响应或错误。
func fixtureDoctor(
	file, endpoint string,
	network, required bool,
	timeout time.Duration,
) func(context.Context, doctor.Options) (doctor.Result, error) {
	return func(ctx context.Context, opts doctor.Options) (doctor.Result, error) {
		if err := opts.Validate(ctx); err != nil {
			return doctor.Result{}, err
		}
		report := doctor.Result{RequiredAvailable: true, Checks: []doctor.Check{}}
		// #nosec G304 -- 派生示例读取调用者明确注入的测试文件，不接受网络路径。
		_, err := os.ReadFile(file)
		status := doctor.StatusAvailable
		if err != nil {
			status = doctor.StatusFailed
			report.RequiredAvailable = false
		}
		report.Checks = append(
			report.Checks,
			doctor.Check{Name: "fixture.local", Status: status, Required: true, Detail: "Read-only local input check"},
		)
		if !network {
			return report, nil
		}
		probe, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		request, err := http.NewRequestWithContext(probe, http.MethodGet, endpoint, nil)
		if err != nil {
			return doctor.Result{}, err
		}
		response, err := (&http.Client{}).Do(request)
		available := err == nil
		if response != nil {
			available = response.StatusCode == http.StatusOK && available
			if closeErr := response.Body.Close(); closeErr != nil {
				available = false
			}
		}
		if err := ctx.Err(); err != nil {
			return doctor.Result{}, err
		}
		status = doctor.StatusAvailable
		if !available {
			status = doctor.StatusFailed
			if required {
				report.RequiredAvailable = false
			}
		}
		report.Checks = append(
			report.Checks,
			doctor.Check{
				Name:     "fixture.network",
				Status:   status,
				Required: required,
				Detail:   "Check endpoint availability, then retry the read-only probe",
			},
		)
		return report, nil
	}
}
