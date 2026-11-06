package diagnostics

// Config 描述日志和本地 Trace 共用的文件策略，0 禁用对应删除限制。
type Config struct {
	MaxSizeMiB      int `mapstructure:"max_size_mib" validate:"gt=0,lte=1048576"`
	MaxBackups      int `mapstructure:"max_backups" validate:"gte=0"`
	RetentionDays   int `mapstructure:"retention_days" validate:"gte=0,lte=36500"`
	MaxTotalSizeMiB int `mapstructure:"max_total_size_mib" validate:"gte=0,lte=1048576"`
}

// Defaults 返回独立的默认文件策略。
func Defaults() Config {
	return Config{MaxSizeMiB: 10, MaxBackups: 5, RetentionDays: 14, MaxTotalSizeMiB: 200}
}

const (
	// EventsFile 是结构化日志的当前分片。
	EventsFile = "events.jsonl"
	// TraceFile 是本地 Trace 的当前分片。
	TraceFile                   = "trace.jsonl"
	mebibyte              int64 = 1024 * 1024
	metadataName                = ".run.json"
	leaseName                   = ".lock"
	maintenanceName             = ".cleanup.lock"
	metadataVersion             = 1
	metadataTemporaryName       = ".run.tmp"
)
