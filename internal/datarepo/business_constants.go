package datarepo

// 业务协议与消息标识集中维护，避免局部拼写漂移。
const (
	phaseAuthenticate  = "data.authenticate"
	codeAuthentication = "data.authentication"
	codeAccess         = "data.access"
	recoverAuth        = "data.recover_auth"
	recoverAccess      = "data.recover_access"
	recoverConfig      = "data.recover_config"
	recoverFormat      = "data.recover_format"
	recoverVerify      = "data.recover_verify"
	phaseBranch        = "data.branch"
	phaseRead          = "data.read"
	codeFormat         = "data.format"
	phaseCommit        = "data.commit"
)
