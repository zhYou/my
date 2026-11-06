package fixture

import "github.com/zhYou/my/tools/internal/toolrun"

// PrepareGit 为本次临时副本建立发布配置校验所需的本地 SCM 标识，不访问远端。
func PrepareGit(dir string) error {
	if err := toolrun.Run(dir, nil, "git", "-c", "init.templateDir=", "init", "--quiet"); err != nil {
		return err
	}
	// 此仓库只用于临时检查，不修改调用方的远端。
	// GoReleaser 的 SCM 校验需要 GitHub 格式的 origin，但不会连接该地址。
	return toolrun.Run(dir, nil, "git", "remote", "add", "origin", "https://github.com/example/template-check.git")
}
