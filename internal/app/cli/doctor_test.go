package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

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
