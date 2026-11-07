package doctor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/zhYou/my/internal/app/config"
	"github.com/zhYou/my/internal/app/machine"
	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/base/messages"
	"github.com/zhYou/my/internal/base/paths"
	"github.com/zhYou/my/internal/datarepo"
)

// Options 显式注入只读检查依赖；Remote 才允许访问配置的数据仓。
type Options struct {
	Remote     bool
	Timeout    time.Duration
	Paths      func() (paths.Directories, error)
	LoadConfig func(context.Context) (config.Config, error)
	CheckGH    func(context.Context) error
	Credential func(context.Context) (string, error)
	NewClient  func(context.Context, datarepo.Config, string) (*datarepo.Client, error)
	State      func(string) machine.State
	Localizer  *messages.Localizer
}

// Validate 在任何检查之前拒绝非法限时和调用方取消。
func (o Options) Validate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if o.Timeout <= 0 {
		return (fault.Definition{Code: "doctor.invalid_timeout", Kind: fault.Usage}).New(
			fault.WithDiagnostic(
				fault.Diagnostic{
					Phase:            "arguments",
					CompletedEffects: []string{},
					Recovery:         &fault.Recovery{Code: "cli.recover_usage"},
				},
			),
		)
	}
	return nil
}

const (
	// StatusAvailable 表示检查已完成且可用。
	StatusAvailable = "available"
	// StatusFailed 表示检查已完成且不可用。
	StatusFailed = "failed"
	// StatusSkipped 表示未启用、不适用或前置条件阻断。
	StatusSkipped = "skipped"
)

// Check 包含可安全输出的解释，不包含捕获正文、token 或错误链。
type Check struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Detail     string `json:"detail,omitempty"`
	Required   bool   `json:"required"`
	Code       string `json:"code,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

// Result 是已完成的报告；必需项失败不是执行或渲染异常。
type Result struct {
	RequiredAvailable bool    `json:"required_available"`
	Checks            []Check `json:"checks"`
}

// Run 串行执行选定检查，不创建诊断文件、缓存、锁或修复状态。
func Run(ctx context.Context, options Options) (Result, error) {
	if err := options.Validate(ctx); err != nil {
		return Result{}, err
	}
	result := Result{RequiredAvailable: true, Checks: []Check{}}
	aborted := false
	text := func(code string) string {
		if options.Localizer != nil {
			return options.Localizer.Text(code, nil)
		}
		return code
	}
	run := func(name string, required bool, check func(context.Context) error) bool {
		if aborted {
			return false
		}
		started := time.Now()
		probe, cancel := context.WithTimeout(ctx, options.Timeout)
		err := check(probe)
		if err == nil {
			err = probe.Err()
		}
		cancel()
		if ctx.Err() != nil {
			aborted = true
			return false
		}
		code := "doctor.check_available"
		status := StatusAvailable
		if err != nil {
			code = "doctor.check_failed"
			status = StatusFailed
			details := fault.Details(err)
			if len(details) > 0 && details[0].Code != "" {
				code = string(details[0].Code)
			}
			if required {
				result.RequiredAvailable = false
			}
		}
		detail := text("doctor.detail." + name)
		if status == StatusFailed {
			detail = text("doctor.remedy." + name)
		}
		result.Checks = append(
			result.Checks,
			Check{
				Name:       name,
				Status:     status,
				Required:   required,
				Code:       code,
				Detail:     detail,
				DurationMS: time.Since(started).Milliseconds(),
			},
		)
		return status == StatusAvailable
	}
	skip := func(name string, required bool, code string) {
		if aborted {
			return
		}
		result.Checks = append(
			result.Checks,
			Check{Name: name, Status: StatusSkipped, Required: required, Code: code, Detail: text(code)},
		)
		if required {
			result.RequiredAvailable = false
		}
	}
	var dirs paths.Directories
	pathsOK := run("paths", true, func(context.Context) error {
		if options.Paths == nil {
			return errors.New("paths unavailable")
		}
		var err error
		dirs, err = options.Paths()
		if err != nil {
			return err
		}
		for _, directory := range []string{dirs.Config, dirs.Data, dirs.State, dirs.Cache} {
			if !filepath.IsAbs(directory) {
				return errors.New("absolute application directory required")
			}
			info, err := os.Lstat(directory)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return errors.New("unsafe directory type")
			}
			if directory == dirs.State && runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
				return errors.New("unsafe state permissions")
			}
		}
		return nil
	})
	var cfg config.Config
	configOK := run("config", true, func(probe context.Context) error {
		if options.LoadConfig == nil {
			return errors.New("configuration unavailable")
		}
		var err error
		cfg, err = options.LoadConfig(probe)
		if err != nil {
			return err
		}
		return cfg.Data.Validate()
	})
	ghOK := run("gh", true, func(probe context.Context) error {
		if options.CheckGH == nil {
			return errors.New("gh unavailable")
		}
		return options.CheckGH(probe)
	})
	var token string
	authOK := false
	if ghOK && configOK {
		authOK = run("auth.local", true, func(probe context.Context) error {
			if options.Credential == nil {
				return errors.New("credential reader unavailable")
			}
			var err error
			token, err = options.Credential(probe)
			if err == nil && strings.TrimSpace(token) == "" {
				return errors.New("credential unavailable")
			}
			return err
		})
	} else {
		skip("auth.local", true, codeBlocked)
	}
	var local machine.Binding
	bound := false
	bindingOK := false
	if pathsOK && configOK {
		bindingOK = run("binding.local", true, func(context.Context) error {
			var state machine.State = machine.NewFileState(dirs.State)
			if options.State != nil {
				state = options.State(dirs.State)
			}
			var err error
			local, bound, err = state.Read()
			if err != nil {
				return err
			}
			if bound && (!strings.EqualFold(local.Repository, cfg.Data.Repository) || local.Branch != cfg.Data.Branch) {
				return errors.New("binding does not match configuration")
			}
			return nil
		})
		if bindingOK && !bound {
			result.Checks[len(result.Checks)-1].Detail = text("doctor.unbound")
		}
	} else {
		skip("binding.local", true, codeBlocked)
	}
	if options.Remote && !aborted {
		remoteChecks(ctx, options, cfg, token, configOK && authOK, bindingOK && bound, local, run, skip)
	}
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	return result, nil
}

func remoteChecks(
	ctx context.Context, options Options, cfg config.Config, token string, ready, bound bool, local machine.Binding,
	run func(string, bool, func(context.Context) error) bool,
	skip func(string, bool, string),
) {
	if !ready {
		for _, name := range []string{"data.repository", checkBranch, checkFormat} {
			skip(name, true, codeBlocked)
		}
		skip(checkRemoteBinding, bound, codeBlocked)
		return
	}
	var client *datarepo.Client
	var metadata datarepo.Metadata
	repoOK := run("data.repository", true, func(probe context.Context) error {
		if options.NewClient == nil {
			return errors.New("repository client unavailable")
		}
		var err error
		client, err = options.NewClient(probe, cfg.Data, token)
		if err != nil {
			return err
		}
		metadata, err = client.RepositoryInfo(probe)
		return err
	})
	branchOK := false
	if repoOK {
		branchOK = run(checkBranch, true, func(probe context.Context) error {
			var err error
			metadata, err = client.Head(probe, metadata)
			return err
		})
	} else {
		skip(checkBranch, true, codeBlocked)
	}
	var records map[string]machine.Record
	formatOK := false
	if branchOK {
		formatOK = run(checkFormat, true, func(probe context.Context) error {
			snapshot, err := client.ReadAt(probe, metadata)
			if err != nil {
				return err
			}
			records, err = machine.Records(snapshot)
			return err
		})
	} else {
		skip(checkFormat, true, codeBlocked)
	}
	if !bound {
		skip(checkRemoteBinding, false, "doctor.unbound")
		return
	}
	if !formatOK {
		skip(checkRemoteBinding, true, codeBlocked)
		return
	}
	run(checkRemoteBinding, true, func(context.Context) error {
		record, ok := records[local.MachineID]
		if !ok || local.RepositoryID != metadata.RepositoryID || record.Status != machine.Active ||
			record.OS != runtime.GOOS ||
			record.Arch != runtime.GOARCH {
			return errors.New("remote binding is not active and consistent")
		}
		return nil
	})
	_ = ctx
}
