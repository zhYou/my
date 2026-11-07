package machine

// 业务协议与消息标识集中维护，避免局部拼写漂移。
const (
	phaseValidate       = "machine.validate"
	phaseRead           = "machine.read"
	codeRecordInvalid   = "machine.record_invalid"
	codeNotFound        = "machine.not_found"
	codeStateInvalid    = "machine.state_invalid"
	phaseBindingRead    = "machine.binding.read"
	phaseBindingWrite   = "machine.binding.write"
	phaseBindingClear   = "machine.binding.clear"
	phaseLock           = "machine.lock"
	recoverLocal        = "machine.recover_local"
	recoverRecord       = "machine.recover_record"
	codeBindingConflict = "machine.binding_conflict"
	effectCommit        = "data.commit"
	outcomeFailed       = "failed"
	platformWindows     = "windows"
)
