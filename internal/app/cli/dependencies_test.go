package cli

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/zhYou/my/internal/app/config"
	"github.com/zhYou/my/internal/app/greeting"
	"github.com/zhYou/my/internal/base/logging"
)

func TestTwoInvocationsDoNotShareDependencies(t *testing.T) {
	var outA, outB bytes.Buffer
	var levels []string
	makeRoot := func(label string, out *bytes.Buffer) *invocationFixture {
		deps := Dependencies{
			LoadConfig: func(_ context.Context, o config.Options) (config.Config, error) {
				level := "warn"
				if o.LogLevel != nil {
					level = *o.LogLevel
				}
				cfg := config.Defaults()
				cfg.Logging = logging.Config{Level: level, Output: "stderr"}
				return cfg, nil
			},
			Greet: func(_ context.Context, o greeting.Options) (greeting.Result, error) {
				return greeting.Result{Message: label + o.Name}, nil
			},
			NewLogger: func(info logging.RunInfo, o logging.Options, warn func(error)) *logging.Logger {
				levels = append(levels, o.Config.Level)
				return logging.New(info, o, warn)
			},
		}
		opts := Options{Out: out, ErrOut: &bytes.Buffer{}, Dependencies: &deps}
		require.Nil(t, deps.Validator)
		return &invocationFixture{run: func(args ...string) {
			require.Zero(t, Run(context.Background(), args, opts))
		}}
	}
	a := makeRoot("甲", &outA)
	b := makeRoot("乙", &outB)
	a.run("greet", "--name", "A", "--json", "--log-level", "debug")
	b.run("greet", "--name", "B")
	require.JSONEq(t, fmt.Sprintf("{%q:%q}", "message", "甲A"), outA.String())
	require.Equal(t, "乙B\n", outB.String())
	require.Equal(t, []string{"debug", "warn"}, levels)
}

type invocationFixture struct {
	run func(...string)
}

func TestInvalidArgsNeverLoadConfigOrRunBusiness(t *testing.T) {
	for _, args := range [][]string{{"greet"}, {"greet", "--name", " "}, {"greet", "--name", "x", "extra"}, {"greet", "--unknown"}} {
		count := 0
		deps := Dependencies{
			LoadConfig: func(context.Context, config.Options) (config.Config, error) {
				count++
				return config.Config{}, nil
			},
			Greet: func(context.Context, greeting.Options) (greeting.Result, error) {
				count++
				return greeting.Result{}, nil
			},
		}
		require.Equal(t, 2, Run(context.Background(), args, Options{Dependencies: &deps}))
		require.Zero(t, count)
	}
}

func TestDiscoveryNeverLoadsConfiguration(t *testing.T) {
	count := 0
	deps := Dependencies{LoadConfig: func(context.Context, config.Options) (config.Config, error) {
		count++
		return config.Config{}, nil
	}}
	for _, args := range [][]string{{"help", "greet"}, {"greet", "--help"}, {"version"}, {"--version"}, {"schema"}, {"completion", "bash"}, {"__complete", "greet", ""}} {
		require.Zero(t, Run(context.Background(), args, Options{Dependencies: &deps}), "%v", args)
	}
	require.Zero(t, count)
}
