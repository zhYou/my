package toolchain

const (
	exactVersionPattern = `^[0-9]+\.[0-9]+\.[0-9]+$`
	miseProgram         = "mise"
	miseConfig          = "mise.toml"
	goplsTool           = "go:golang.org/x/tools/gopls"
	govulncheckTool     = "go:golang.org/x/vuln/cmd/govulncheck"
	versionFlag         = "--version"
	govulncheckProgram  = "govulncheck"
	lintProgram         = "golangci-lint"
	actionlintProgram   = "actionlint"
	plainVersionPattern = `(?m)^([0-9]+\.[0-9]+\.[0-9]+)(?:[ \t\r\n]|$)`
	goplsProgram        = "gopls"
	versionCommand      = "version"
	releaseProgram      = "goreleaser"
)
