package cli

// 本包协议、状态与命令标识保持稳定，避免局部拼写漂移。
const (
	paramMessage         = "Message"
	paramName            = "Name"
	paramValue           = "Value"
	paramFlag            = "Flag"
	languageChinese      = "zh-CN"
	languageEnglish      = "en"
	flagJSON             = "json"
	outputJSON           = "json"
	flagHelp             = "help"
	flagVersion          = "version"
	flagConfig           = "config"
	defaultLogLevel      = "info"
	flagLanguage         = "lang"
	annotationEnabled    = "true"
	commandCompletion    = "completion"
	annotationSkipConfig = "skip-config"
	flagName             = "name"
	commandHelp          = "help"
	commandVersion       = "version"

	flagLogLevel = "log-level"
)

const (
	phaseArguments = "arguments"
	codeUsage      = "cli.usage"
	codeRuntime    = "cli.runtime"
	codeCancelled  = "cli.cancelled"
	codeDeadline   = "cli.deadline"
	phaseGreeting  = "greeting.run"
)
