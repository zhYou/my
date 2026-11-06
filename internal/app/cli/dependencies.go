package cli

import (
	"context"

	"github.com/zhYou/my/internal/app/config"
	"github.com/zhYou/my/internal/app/doctor"
	"github.com/zhYou/my/internal/app/greeting"
	"github.com/zhYou/my/internal/app/session"
	"github.com/zhYou/my/internal/base/logging"
	"github.com/zhYou/my/internal/base/messages"
	"github.com/zhYou/my/internal/base/validation"
)

// Dependencies 描述命令依赖；构造时复制并补齐默认值，不修改调用者对象。
type Dependencies struct {
	LoadConfig func(context.Context, config.Options) (config.Config, error)
	Doctor     func(context.Context, doctor.Options) (doctor.Result, error)
	Validator  *validation.Validator
	Greet      func(context.Context, greeting.Options) (greeting.Result, error)
	NewLogger  func(logging.RunInfo, logging.Options, func(error)) *logging.Logger
	Logger     *logging.Logger
}
type invocation struct {
	languageFlag   *languageValue
	bundle         *messages.Bundle
	localizer      *messages.Localizer
	commandTexts   []commandTextBinding
	flagTexts      []flagTextBinding
	helpErr        error
	reportExitCode int
	results        resultRegistry
	asJSON         bool
	configPath     string
	logLevel       string
	deps           Dependencies
	logger         *logging.Logger
	session        *session.Session
}

func newInvocation(provided *Dependencies) *invocation {
	var deps Dependencies
	if provided != nil {
		deps = *provided
	}
	if deps.Validator == nil {
		deps.Validator = validation.New()
	}
	if deps.LoadConfig == nil {
		deps.LoadConfig = config.Load
	}
	if deps.Doctor == nil {
		deps.Doctor = doctor.Run
	}
	if deps.Greet == nil {
		deps.Greet = greeting.New(deps.Validator).Run
	}
	if deps.NewLogger == nil {
		deps.NewLogger = logging.New
	}
	return &invocation{deps: deps, logLevel: defaultLogLevel, logger: logging.NewNop()}
}
