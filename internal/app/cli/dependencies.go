package cli

import (
	"context"
	"sync"

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
	diagnosticMu    sync.RWMutex
	languageFlag    *languageValue
	bundle          *messages.Bundle
	localizer       *messages.Localizer
	commandTexts    []commandTextBinding
	flagTexts       []flagTextBinding
	helpErr         error
	reportExitCode  int
	results         resultRegistry
	asJSON          bool
	jsonDiagnostics bool
	phase           string
	configPath      string
	logLevel        string
	deps            Dependencies
	logger          *logging.Logger
	session         *session.Session
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
	return &invocation{phase: phaseArguments, deps: deps, logLevel: defaultLogLevel, logger: logging.NewNop()}
}

func (call *invocation) setPhase(phase string) {
	call.diagnosticMu.Lock()
	defer call.diagnosticMu.Unlock()
	call.phase = phase
}

func (call *invocation) setSession(owned *session.Session) {
	call.diagnosticMu.Lock()
	defer call.diagnosticMu.Unlock()
	call.phase = "execute"
	call.session = owned
}

func (call *invocation) diagnosticView() *invocation {
	call.diagnosticMu.RLock()
	defer call.diagnosticMu.RUnlock()
	return &invocation{
		phase:           call.phase,
		session:         call.session,
		localizer:       call.localizer,
		jsonDiagnostics: call.jsonDiagnostics,
	}
}
