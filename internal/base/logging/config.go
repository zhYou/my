package logging

// Config 描述日志级别和出口，具体资源由调用入口托管。
type Config struct {
	Level  string `mapstructure:"level" validate:"oneof=debug info warn error"`
	Output string `mapstructure:"output" validate:"oneof=file stderr none"`
}

// Defaults 返回本模块的默认配置。
func Defaults() Config {
	return Config{Level: "info", Output: "file"}
}
