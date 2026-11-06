package scaffold

// 本包协议、状态与命令标识保持稳定，避免局部拼写漂移。
const (
	mainFile       = "main.go"
	phaseCommitted = "committed"
	originFile     = ".template-origin.json"
	gitShow        = "show"
	moduleFile     = "go.mod"
	journalFile    = "journal.json"
	phasePrepared  = "prepared"
	phaseApplying  = "applying"
)
