package main

const (
	testdataDirectory      = "testdata"
	miseActionVersionField = "version"
	lintProgram            = "golangci-lint"
	moduleFile             = "go.mod"
	allPackages            = "./..."
	runCommand             = "run"
	lintUsage              = "用法：lint <i18n|config|opencode|agent-rules|doc|style|versions|test>"
	resolveRepositoryError = "解析仓库路径: %w"
	readPathError          = "读取 %s: %w"
)
