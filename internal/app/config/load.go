package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
	"github.com/pelletier/go-toml/v2"
	"github.com/zhYou/my/internal/base/diagnostics"
	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/base/logging"
	"github.com/zhYou/my/internal/base/paths"
	"github.com/zhYou/my/internal/base/telemetry"
	"github.com/zhYou/my/internal/base/validation"
	"github.com/zhYou/my/internal/datarepo"
)

// Config 保存模块组合后的强类型配置。
type Config struct {
	Data        datarepo.Config    `mapstructure:"data"`
	Secrets     Secrets            `mapstructure:"secrets"`
	Logging     logging.Config     `mapstructure:"logging"`
	Diagnostics diagnostics.Config `mapstructure:"diagnostics"`
	Telemetry   telemetry.Config   `mapstructure:"telemetry"`
}

// Options 声明本次调用的显式依赖与覆盖选项。
type Options struct {
	Layout        paths.Layout
	AppName       string
	Path          string
	UserConfigDir string
	LookupEnv     func(string) (string, bool)
	LogLevel      *string
	AgeIdentity   *string
	Validator     *validation.Validator
	Explicit      bool
}

// Defaults 返回本模块的默认配置。
func Defaults() Config {
	return Config{
		Data:        datarepo.Defaults(),
		Secrets:     SecretsDefaults(),
		Logging:     logging.Defaults(),
		Diagnostics: diagnostics.Defaults(),
		Telemetry:   telemetry.Defaults(),
	}
}

// Load 按显式选项 > 非空环境变量 > 唯一 TOML 文件 > 默认值解析。
func Load(ctx context.Context, opts Options) (Config, error) {
	cfg := Defaults()
	if opts.Layout == "" {
		opts.Layout = paths.HomeLayout
	}
	if err := ctx.Err(); err != nil {
		return cfg, err
	}
	if opts.AppName == "" {
		opts.AppName = "my"
	}
	lookup := opts.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	if opts.Explicit && opts.Path == "" {
		return cfg, problem("path", nil, nil)
	}
	file := opts.Path
	// 显式 HOME 即使配合 --config 也必须合法，避免后续资源入口解释不一致。
	home, exists := lookup(paths.EnvPrefix(opts.AppName) + "_HOME")
	if !exists {
		home = ""
	}
	if home != "" || file == "" && opts.UserConfigDir == "" {
		dirs, err := paths.Resolve(paths.Options{AppName: opts.AppName, LookupEnv: lookup, Layout: opts.Layout})
		if err != nil {
			return cfg, err
		}
		if file == "" {
			file = dirs.ConfigFile()
		}
	} else if file == "" {
		file = filepath.Join(opts.UserConfigDir, opts.AppName, "config.toml")
	}
	defaults := map[string]any{
		"data.repository":                cfg.Data.Repository,
		"data.branch":                    cfg.Data.Branch,
		"data.request_timeout":           cfg.Data.RequestTimeout,
		"secrets.identity_file":          cfg.Secrets.IdentityFile,
		loggingLevelKey:                  cfg.Logging.Level,
		"logging.output":                 cfg.Logging.Output,
		"diagnostics.max_size_mib":       cfg.Diagnostics.MaxSizeMiB,
		"diagnostics.max_backups":        cfg.Diagnostics.MaxBackups,
		"diagnostics.retention_days":     cfg.Diagnostics.RetentionDays,
		"diagnostics.max_total_size_mib": cfg.Diagnostics.MaxTotalSizeMiB,
		"telemetry.exporter":             cfg.Telemetry.Exporter,
		"telemetry.export_timeout":       cfg.Telemetry.ExportTimeout,
		"telemetry.shutdown_timeout":     cfg.Telemetry.ShutdownTimeout,
		"telemetry.queue_size":           cfg.Telemetry.QueueSize,
		"telemetry.batch_size":           cfg.Telemetry.BatchSize,
		"telemetry.batch_interval":       cfg.Telemetry.BatchInterval,
	}
	k := koanf.New(".")
	layer := func(values map[string]any) error {
		return k.Load(confmap.Provider(values, "."), nil)
	}
	if err := layer(defaults); err != nil {
		return cfg, problem(decodeProblem, err, nil)
	}
	// 跟随配置符号链接，但拒绝 FIFO、设备等非普通目标，避免同步读取无限等待。
	info, err := os.Stat(file)
	var data []byte
	if err == nil {
		if !info.Mode().IsRegular() {
			return cfg, problem("file_type", nil, fault.Params{pathParameter: file})
		}
		if err := ctx.Err(); err != nil {
			return cfg, err
		}
		//nolint:gosec // G304：用户选择的普通配置文件；不接受特殊文件。
		data, err = os.ReadFile(file)
	}
	if err != nil {
		if opts.Explicit || opts.Path != "" || !errors.Is(err, os.ErrNotExist) {
			return cfg, problem("read", err, fault.Params{pathParameter: file})
		}
	} else {
		var values map[string]any
		if err := toml.Unmarshal(data, &values); err != nil {
			return cfg, problem("parse", err, fault.Params{pathParameter: file})
		}
		// 先校验 TOML 的原始嵌套结构，禁止字面点号键被解释为路径。
		var typed Config
		decoderConfig := strictDecoderConfig()
		decoderConfig.TagName = "mapstructure"
		decoderConfig.Result = &typed
		decoder, err := mapstructure.NewDecoder(decoderConfig)
		if err != nil {
			return cfg, problem(decodeProblem, err, nil)
		}
		if err := decoder.Decode(values); err != nil {
			return cfg, problem(decodeProblem, err, nil)
		}
		// TOML 解析器已经展开合法点号语法；文件数据不再做键展开。
		if err := k.Load(confmap.Provider(values, ""), nil); err != nil {
			return cfg, problem(decodeProblem, err, nil)
		}
	}
	for key, def := range defaults {
		env := paths.EnvPrefix(opts.AppName) + "_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
		value, ok := lookup(env)
		if !ok || value == "" {
			continue
		}
		var typed any = value
		if _, ok := def.(int); ok {
			number, err := strconv.Atoi(value)
			if err != nil {
				return cfg, problem(environmentProblem, err, fault.Params{"Field": env})
			}
			typed = number
		}
		if _, ok := def.(time.Duration); ok {
			duration, err := time.ParseDuration(value)
			if err != nil {
				return cfg, problem(environmentProblem, err, fault.Params{"Field": env})
			}
			typed = duration
		}
		if err := layer(map[string]any{key: typed}); err != nil {
			return cfg, problem(decodeProblem, err, nil)
		}
	}
	if value, ok := lookup(paths.EnvPrefix(opts.AppName) + "_LOG_LEVEL"); ok && value != "" {
		if err := layer(map[string]any{loggingLevelKey: value}); err != nil {
			return cfg, problem(decodeProblem, err, nil)
		}
	}
	if opts.LogLevel != nil {
		if err := layer(map[string]any{loggingLevelKey: *opts.LogLevel}); err != nil {
			return cfg, problem(decodeProblem, err, nil)
		}
	}
	if opts.AgeIdentity != nil {
		if err := layer(map[string]any{"secrets.identity_file": *opts.AgeIdentity}); err != nil {
			return cfg, problem(decodeProblem, err, nil)
		}
	}
	if err := decode(k, &cfg); err != nil {
		return cfg, problem(decodeProblem, err, nil)
	}
	v := opts.Validator
	if v == nil {
		v = validation.New()
	}
	if err := v.Struct(ctx, cfg); err != nil {
		return cfg, problem(invalidProblem, err, nil)
	}
	if err := cfg.Data.ValidateOptional(); err != nil {
		return cfg, problem(invalidProblem, err, nil)
	}
	if err := cfg.Secrets.ValidateOptional(); err != nil {
		return cfg, problem(invalidProblem, err, nil)
	}
	return cfg, ctx.Err()
}

func decode(k *koanf.Koanf, cfg *Config) error {
	return k.UnmarshalWithConf("", cfg, koanf.UnmarshalConf{Tag: "mapstructure", DecoderConfig: strictDecoderConfig()})
}

func strictDecoderConfig() *mapstructure.DecoderConfig {
	return &mapstructure.DecoderConfig{
		WeaklyTypedInput: false,
		ErrorUnused:      true,
		DecodeHook:       strictHook,
		MatchName: func(a, b string) bool {
			return a == b
		},
	}
}

func strictHook(from, to reflect.Type, value any) (any, error) {
	if to == reflect.TypeFor[time.Duration]() {
		switch v := value.(type) {
		case time.Duration:
			return v, nil
		case string:
			return time.ParseDuration(v)
		default:
			return nil, problem("duration", nil, nil)
		}
	}
	if to.Kind() == reflect.Int {
		if from.Kind() != reflect.Int && from.Kind() != reflect.Int64 {
			return nil, problem("integer", nil, nil)
		}
		number := reflect.ValueOf(value).Int()
		if reflect.New(to).Elem().OverflowInt(number) {
			return nil, problem("integer", nil, nil)
		}
	}
	return value, nil
}

func problem(code string, cause error, params fault.Params) error {
	d := fault.Definition{Code: fault.Code("config." + code), Kind: fault.Configuration}
	if cause != nil {
		options := []fault.Option{fault.WithParams(params), fault.WithDiagnostic(configDiagnostic(code, params))}
		if code == "parse" || code == decodeProblem || code == environmentProblem {
			options = append(options, fault.WithSensitiveCause())
		}
		return d.Wrap(cause, options...)
	}
	return d.New(fault.WithParams(params), fault.WithDiagnostic(configDiagnostic(code, params)))
}

func configDiagnostic(code string, params fault.Params) fault.Diagnostic {
	recovery := &fault.Recovery{Code: "config.recover"}
	if code == environmentProblem {
		recovery = &fault.Recovery{Code: "config.recover_environment", Params: params}
	}
	return fault.Diagnostic{Phase: "config." + code, CompletedEffects: []string{}, Recovery: recovery}
}
