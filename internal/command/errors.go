package command

import (
	"github.com/zhYou/my/internal/base/fault"
	"github.com/zhYou/my/internal/base/messages"
	"github.com/zhYou/my/internal/locales"
)

var (
	windowsSuspendInvalid = fault.Definition{Code: "command.windows_suspend_invalid", Kind: fault.Runtime}
	windowsJobNotEmpty    = fault.Definition{Code: "command.windows_job_not_empty", Kind: fault.Runtime}
	programRequired       = fault.Definition{Code: "command.program_required", Kind: fault.Runtime}
	argumentRequired      = fault.Definition{Code: "command.argument_required", Kind: fault.Runtime}
	waitDelayInvalid      = fault.Definition{Code: "command.wait_delay_invalid", Kind: fault.Runtime}
	captureLimitInvalid   = fault.Definition{Code: "command.capture_limit_invalid", Kind: fault.Runtime}
	batchOptionsInvalid   = fault.Definition{Code: "command.batch_options_invalid", Kind: fault.Runtime}
	batchItemFailed       = fault.Definition{Code: "command.batch_item_failed", Kind: fault.Runtime}
	batchItemIncomplete   = fault.Definition{Code: "command.batch_item_incomplete", Kind: fault.Runtime}
)

func commandLocalizer(provided *messages.Localizer) *messages.Localizer {
	if provided != nil {
		return provided
	}
	bundle, err := locales.LoadBundle()
	if err != nil {
		return nil
	}
	return bundle.Localizer("en")
}

func diagnosticText(localizer *messages.Localizer, id string) string {
	if localizer == nil || localizer.Language() == "" {
		return "Operation failed [" + id + "]"
	}
	return localizer.Text(id, nil)
}
