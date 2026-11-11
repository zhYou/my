# 维护清单

本文记录目标仓库须由维护者落实的托管、CI、发布和诊断操作。配置文件存在不代表平台功能已启用。
日常开发见[开发说明](development.md)，工作区和交付遵循[协作约定](../AGENTS.md)。
探索想法仅按[待办约定](backlog.md#约定)记录，当前实施任务不自动新增待办。

## 责任与审查

维护责任人、发布负责人和安全报告接收人：**zhYou**。私密报告入口及启用状态以 [SECURITY](../SECURITY.md) 为准。

填写 [.github/CODEOWNERS.example](../.github/CODEOWNERS.example) 后复制为 CODEOWNERS，确认团队有仓库访问权限；
默认不启用占位账号。为 CLI 契约、初始化事务、安全 workflow 和发布流程指定审查责任。

## 仓库管理员设置

- 为 main 设置规则集，要求 PR、审查、分支更新及必需检查。按实际运行名称启用 `check`、`platforms (macos-latest)`、`platforms (windows-latest)`、`vulnerability`，首次运行后核对名称。
- 保护 `v*` 标签，限制创建和修改权限；采用不可变发布资产策略。
- 按 [SECURITY](../SECURITY.md) 填写私密报告入口，启用对应功能，公布支持版本和响应安排。
- 安装或授权 Renovate。现有配置覆盖 Go、Actions SHA、mise 工具及 npm/PyPI 版本变量，禁用自动合并及预发布更新；工具升级方法见[开发说明](development.md#修改模板与升级工具)。
- 确认 release job 可申请 OIDC 和 attestations 写权限，并满足 GitHub 套餐及组织、仓库策略。普通 PR 和定时扫描只读，不使用发布凭据。
- 指定定时安全扫描失败的接收人；停用或归档前交接维护责任。

配置完成后记录实际设置及证据，不仅凭仓库文件宣称托管设置生效。

## CI 与验收证据

CI 和 Security 检查目标为 main 的 PR 及 main 更新，开发分支普通 push 和分支间 PR 不执行。
PR 改目标、标题或描述会重新检查；同一 PR 新运行取消旧运行，其他运行按各自 ID 隔离。
每日 Security 独立运行；推送 `v*` 标签时 Release 先复用同一提交的完整 CI 与 Security，再执行发布。

Linux 源码、race、fuzz/benchmark、snapshot/安装及 MCP 验收分组并行；Linux/macOS/Windows 均真实运行 Serena、Graphify MCP，
macOS/Windows 保留原生任务。三平台实际验证 npx/uvx、XML/TOML 插件、五类文本与冷/热缓存，不能用命令替换或跨编译代替。
CI 的 `check` 汇总全部作业，任一失败、取消或跳过都不能通过。平台运行、MCP、漏洞和发布验收独立于本地 `mise run check`。
基础 CI 无需安装 Codex、RTK 或 superpowers；MCP job 复用统一 setup，再通过 uvx 按需准备 Serena、Graphify 与后台依赖。
源码任务通过统一 lint 运行 actionlint 工作流语义检查，模板副本验收同样复用该入口。

Go 缓存区分 OS、架构、Go 版本和依赖摘要，编译缓存再区分任务及提交；lint 独立缓存，项目工具复用 mise 缓存，npx/uvx 与独立 Python 仅缓存外部工具目录，按 OS、架构及版本声明区分。
Windows 只跨运行缓存模块，编译结果在当前 runner 内复用。缓存不包含应用状态、诊断文件或发布资产，命中不能跳过验证。
排查性能时记录冷、热缓存耗时和命中情况；具体键与任务以 workflow 为准。

交付分别列出已完成的 Linux 测试、macOS/Windows 原生 CI、六目标交叉构建及宿主安装。
未完成的项目写为待验证；三 OS 配置或交叉编译不能替代原生运行证据。

## 升级和发布

升级模板先按[旧事务恢复要求](template-upgrades.md#升级前完成旧事务)完成恢复，再人工合并；
机器接口变化按[兼容性规则](compatibility.md)维护记录。模板版本与应用发布版本独立，记录本身不创建标签。

发布前按[示例隔离要求](development.md#测试与示例隔离)设置本次验证的应用 HOME，完成：

```sh
mise run check
mise exec -- go test -race ./...
mise exec -- govulncheck ./...
```

在最终提交后生成 snapshot，再验证归档及宿主安装：

```sh
mise exec -- goreleaser release --snapshot --clean --parallelism 2
mise run release:verify
```

核对 Linux/macOS 的 tar.gz、Windows 的 zip，共六目标归档及根目录配置示例、SPDX 和最终 checksums。
Syft 关闭 enrichment、远程许可证查询和更新检查，SBOM 完成后才生成最终摘要。
release:verify 核对版本、HEAD、归档清单和字节，使用隔离 mise 验证错误 SHA 失败、正确 SHA 安装成功，
再从绝对安装路径运行二进制；不改全局 mise，不把应用 HOME 当安装目录。后续提交须重建用于发布验收的 snapshot。

维护者确认后创建并推送版本标签，例如 `git tag v0.1.0`、`git push origin v0.1.0`；版本号按本次发布确定。
目标仓库须先配置自己的 GitHub origin。本地 snapshot 不发布或上传证明；Release workflow 对最终发布资产生成来源证明。
若证明失败，workflow 失败；重试必须使用同一批已发布资产的原始字节，不重构建或覆盖资产。

下载同一 Release 的资产后，验证摘要和来源证明：

```sh
sha256sum -c checksums.txt
# macOS 可用 shasum -a 256 -c checksums.txt
gh attestation verify <asset> --repo <owner/repo>
```

将占位符替换为实际资产及仓库。SPDX 描述构建依赖，不等于许可证合规结论；目标项目自行选择许可证。

## 诊断排查

日志与 Trace 的格式、轮转和清理规则见[资源所有权](design-guidelines.md#日志trace-和异步工作的所有权)。
告警时检查路径、权限和空间，保留业务退出码，不为消除告警清空用户状态目录。
调查只收集必要且脱敏的事件字段，不公开密钥、完整 argv、环境或工具原始输出。
