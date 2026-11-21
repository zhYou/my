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
	codeNotManaged         = "target.not_managed"
	codeOutOfScope         = "target.out_of_scope"
	codeLegacyConflict     = "target.legacy"
	firstTakeoverCode      = "target.first_takeover"
	changedBothCode        = "target.changed_both"
	fileMissingCode        = "target.file_missing"
	recoverUsage           = "cli.recover_usage"
	recoverConfiguration   = "target.recover"
	recoverFileUnsupported = "target.recover_file_unsupported"
	recoverNotManaged      = "target.recover_not_managed"
	recoverOutOfScope      = "target.recover_out_of_scope"
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
