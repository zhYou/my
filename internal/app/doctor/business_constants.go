package doctor

// 业务协议与消息标识集中维护，避免局部拼写漂移。
const (
	codeBlocked        = "doctor.blocked"
	checkBranch        = "data.branch"
	checkRemoteBinding = "binding.remote"
)

const checkFormat = "data.format"
