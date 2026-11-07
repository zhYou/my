package paths

// 本包协议、状态与命令标识保持稳定，避免局部拼写漂移。
const (
	dataDirectory   = "data"
	homeEnvironment = "HOME"

	stateDirectory = "state"
	cacheDirectory = "cache"
)

// Layout 指定应用私有目录的组织方式，空值保留底层平台布局。
type Layout string

const (
	// HomeLayout 将配置、数据、状态和缓存集中到用户主目录的隐藏应用目录。
	HomeLayout Layout = "home"
	// PlatformLayout 使用操作系统约定的目录。
	PlatformLayout Layout = "platform"
)
