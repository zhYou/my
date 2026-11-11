package shellenv

const (
	schemaVersion = 1
	// MaxValueSize 是单个环境变量值允许的最大字节数。
	MaxValueSize        = 8 << 10
	maxOutputSize       = 32 << 10
	unsafeValueControls = "\r\n\x00"

	targetOrdinary = "shell-env"
	targetSecret   = "shell-env-secret"

	configManifest     = "configs/manifest.toml"
	targetsRoot        = "configs/targets"
	targetMetadataFile = "target.toml"
	dataDirectory      = "data"
	envFileName        = "env.toml"
	envCipherName      = "env.toml.age"

	shellBash       = "bash"
	shellZsh        = "zsh"
	shellFish       = "fish"
	shellPowerShell = "powershell"
)

const (
	effectDataCommit = "data.commit"
	phaseCacheWrite  = "shellenv.cache_write"
)
