package cli

// 业务协议与消息标识集中维护，避免局部拼写漂移。
const (
	paramCommit                   = "Commit"
	schemaList                    = "list"
	schemaChange                  = "change"
	messageRecordResult           = "machine.record_result"
	flagKind                      = "kind"
	flagMachine                   = "machine"
	flagUnit                      = "unit"
	flagFrom                      = "from"
	flagSecret                    = "secret"
	paramChanged                  = "Changed"
	phaseExecute                  = "execute"
	shellenvStateDirectory        = "shellenv"
	schemaEnumKey                 = "enum"
	schemaAdditionalPropertiesKey = "additionalProperties"
)

const (
	schemaPropertiesKey = "properties"
	schemaStringType    = "string"
	schemaObjectType    = "object"
	flagNote            = "note"
)

const (
	codeBindingConflict = "machine.binding_conflict"
)
