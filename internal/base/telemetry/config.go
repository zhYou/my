package telemetry

import "time"

const (
	// None 禁用 Trace SDK 和导出。
	None = "none"
	// File 将 span 写入入口提供的诊断 writer。
	File = "file"
)

// Config 描述一次运行的 Trace 出口及有界批量导出参数。
type Config struct {
	Exporter        string        `mapstructure:"exporter" validate:"oneof=none file"`
	ExportTimeout   time.Duration `mapstructure:"export_timeout" validate:"gt=0"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout" validate:"gt=0"`
	QueueSize       int           `mapstructure:"queue_size" validate:"gt=0,lte=65536"`
	BatchSize       int           `mapstructure:"batch_size" validate:"gt=0,ltefield=QueueSize"`
	BatchInterval   time.Duration `mapstructure:"batch_interval" validate:"gt=0"`
}

// Defaults 返回默认关闭、启用时有界导出的配置。
func Defaults() Config {
	return Config{
		Exporter:        None,
		ExportTimeout:   time.Second,
		ShutdownTimeout: time.Second,
		QueueSize:       2048,
		BatchSize:       512,
		BatchInterval:   5 * time.Second,
	}
}
