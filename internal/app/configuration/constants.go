package configuration

const (
	configDataDirectory  = "data"
	codexFilesystemTable = "filesystem"

	stateSchemaVersion = 1
	stateDirectoryMode = 0o700
	stateFileMode      = 0o600
	stateLockFile      = "lock"

	phaseValidate        = "configuration.validate"
	phaseStateRead       = "configuration.state_read"
	phaseStateWrite      = "configuration.state_write"
	phaseCompare         = "configuration.compare"
	codeInvalidInput     = "configuration.invalid_input"
	codeStateInvalid     = "configuration.state_invalid"
	codeLegacyConflict   = "configuration.legacy"
	firstTakeoverCode    = "configuration.first_takeover"
	changedBothCode      = "configuration.changed_both"
	fileMissingCode      = "configuration.file_missing"
	recoverUsage         = "cli.recover_usage"
	recoverConfiguration = "configuration.recover"
)

const stateModeDirectory = stateDirectoryMode
