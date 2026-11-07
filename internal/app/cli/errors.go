package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"
	"github.com/zhYou/my/internal/base/fault"
)

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return 130
	}
	if fault.KindOf(err) == fault.Usage {
		return 2
	}
	return 1
}

func classifyExecutionError(err error, phase string) error {
	if err == nil {
		return nil
	}
	details := fault.Details(err)
	normalized := make([]*fault.Error, 0, len(details))
	changed := false
	_, typed := errors.AsType[*fault.Error](err)
	for _, problem := range details {
		if problem.Code != "" {
			normalized = append(normalized, problem)
			continue
		}
		kind := problem.Kind
		if phase == phaseArguments && !typed {
			kind = fault.Usage
		}
		code := codeUsage
		if kind != fault.Usage {
			code = codeRuntime
		}
		if errors.Is(problem, context.Canceled) {
			code = codeCancelled
			kind = fault.Runtime
		}
		if errors.Is(problem, context.DeadlineExceeded) {
			code = codeDeadline
			kind = fault.Runtime
		}
		d, known := fault.DiagnosticOf(problem)
		if d.Phase == "" {
			d.Phase = phase
		}
		if !known && phase == phaseArguments {
			d.CompletedEffects = []string{}
		}
		if kind == fault.Usage && d.Recovery == nil {
			d.Recovery = &fault.Recovery{Code: "cli.recover_usage"}
		}
		classified := (fault.Definition{Code: fault.Code(code), Kind: kind}).New(fault.WithDiagnostic(d))
		classified.Cause = problem
		normalized = append(normalized, classified)
		changed = true
	}
	if !changed {
		return err
	}
	return (fault.Definition{Kind: fault.KindOf(err)}).Wrap(err, fault.WithDetails(normalized))
}

func wrapRun(run func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	if run == nil {
		return nil
	}
	return func(cmd *cobra.Command, args []string) error {
		err := run(cmd, args)
		if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if _, ok := errors.AsType[*fault.Error](err); ok {
			return err
		}
		return fault.Wrap(fault.Runtime, "", err)
	}
}

// protectCommand 固定参数错误与执行错误的边界，避免依赖 hook 执行顺序。
func protectCommand(cmd *cobra.Command) {
	if args := cmd.Args; args != nil {
		cmd.Args = func(c *cobra.Command, a []string) error {
			if err := args(c, a); err != nil {
				return fault.Wrap(fault.Usage, "", err)
			}
			return nil
		}
	}
	cmd.RunE = wrapRun(cmd.RunE)
	cmd.PreRunE = wrapRun(cmd.PreRunE)
	cmd.PersistentPreRunE = wrapRun(cmd.PersistentPreRunE)
	cmd.PostRunE = wrapRun(cmd.PostRunE)
	cmd.PersistentPostRunE = wrapRun(cmd.PersistentPostRunE)
	for _, child := range cmd.Commands() {
		protectCommand(child)
	}
}
