package target

const (
	configDataDirectory  = "data"
	codexFilesystemTable = "filesystem"

	stateSchemaVersion = 1
	stateDirectoryMode = 0o700
	stateFileMode      = 0o600
	stateLockFile      = "lock"

	phaseValidate          = "target.validate"
	phaseStateRead         = "target.state_read"
	phaseListRead          = "target.list_read"
	phaseStateWrite        = "target.state_write"
	phaseCompare           = "target.compare"
	codeInvalidInput       = "target.invalid_input"
	codeStateInvalid       = "target.state_invalid"
	codeFileUnsupported    = "target.file_unsupported"
	codeLegacyConflict     = "target.legacy"
	firstTakeoverCode      = "target.first_takeover"
	changedBothCode        = "target.changed_both"
	fileMissingCode        = "target.file_missing"
	recoverUsage           = "cli.recover_usage"
	recoverConfiguration   = "target.recover"
	recoverFileUnsupported = "target.recover_file_unsupported"
)

const (
	effectDataCommit  = "data.commit"
	platformDarwin    = "darwin"
	miseToolsTable    = "tools"
	miseSettingsTable = "settings"
)

const (
	platformLinux   = "linux"
	platformWindows = "windows"
)
