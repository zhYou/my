# 工具链边界

本文说明系统工具、用户工具、mise、按需执行的 npx/uvx 工具和 Go 生态之间的安装与运行关系。
准确版本以 [README 工具表](../README.md#本地环境) 和 [mise.toml](../mise.toml) 为唯一来源；
本地检查、CI 与发布使用同一组声明，不在不同入口维护第二份版本清单。

## 分层模型

| 层                | 所有者与版本来源                      | 安装位置                            | 项目中的用途                                         |
| ----------------- | ------------------------------------- | ----------------------------------- | ---------------------------------------------------- |
| 系统与宿主工具    | 操作系统、开发机或 GitHub runner      | 系统 PATH                           | Git、shell、C 编译器等基础能力；项目只检查必需能力   |
| 用户工具          | 用户自行安装和维护                    | 用户 PATH、用户级客户端目录         | mise、gh、Codex、OpenCode、RTK 和 Matt Pocock skills |
| 项目 mise 工具    | 仓库通过 `mise.toml [tools]` 精确声明 | mise 数据目录，按 checkout 激活     | Go、固定开发 CLI、语言运行时和 Serena 后台依赖       |
| 按需 npx/uvx 工具 | 仓库通过 `mise.toml [vars]` 精确声明  | npm、uv 和 Python 外部缓存          | Prettier、文本检查、Serena、Graphify 及插件          |
| Go 模块依赖       | `go.mod` 声明、`go.sum` 校验          | Go 模块缓存                         | 产品程序、测试和仓库 Go 工具的库依赖                 |
| 仓库 Go 开发工具  | 与产品代码同仓库、同模块              | 由 Go 临时编译并通过 mise task 调用 | 配置检查、契约、模板、MCP、发布和文档规则            |

系统、用户和项目工具的边界按“谁负责升级”划分。项目不安装或复制用户级客户端、技能、模型、凭据和权限，
也不把个人路径写入模板。`mise` 自身是用户工具；仓库只声明最低版本，CI 只固定用于引导的 mise 版本。

## 安装与激活

```text
Git + 用户安装的 mise
  -> 读取当前 checkout 的 mise.toml
     -> [tools]：安装 Go、固定 CLI、Node、Python、uv、Taplo 等
     -> [vars]：为 npx/uvx 选择准确包版本
  -> mise run <task>
     -> 原生工具直接执行
     -> go run ./tools/... 编译仓库开发工具
     -> npx/uvx 在外部缓存准备临时工具环境
```

`mise.toml` 关闭锁文件、任务自动安装和 exec 自动安装。首次准备、工具版本变化或 Go 模块变化后显式运行
`mise run setup`；它安装 `[tools]` 并下载 Go 模块。普通任务不会因缺少工具而隐式下载，失败会直接报告。
`GOTOOLCHAIN=local` 阻止 Go 命令下载另一套工具链。

`[tools]` 和 `[vars]` 都使用准确版本。区别是生命周期的控制点：

- `[tools]` 中的条目由 mise 安装，适合需要稳定 PATH、跨任务复用或原生版本探测的工具。
- `[vars]` 中的条目由 npx/uvx 按调用准备，适合文本格式器、检查器和独立 Python 客户端。
- `npx` 与 `uvx` 可以按需联网，但缓存和隔离工具环境位于用户或 CI 的外部目录，不创建项目内 `node_modules`、`.venv` 或 npm 工程。

## Go 生态

Go 工具链、应用依赖和 Go 开发工具属于三个不同范围：

- Go 工具链由 mise 固定；产品与开发工具都在同一 Go 版本下编译。
- 产品依赖由 `go.mod` 和 `go.sum` 管理，随程序和测试编译。
- gopls、govulncheck 等通过 `go:` 后端由 mise 安装。它们是开发 CLI，不写入产品 `go.mod`，也不成为发布程序依赖。
- `tools/` 下的检查器是仓库 Go 代码，可以复用运行时包；运行时代码和测试不反向依赖开发包。
- GoReleaser 编译六个发布目标，Syft 生成 SPDX；最终二进制不依赖 Go、mise、Node、Python 或 uv。
- 源码安装 `go install <module>/cmd/<name>@<version>` 只要求调用者提供兼容的 Go 工具链。

常用关系如下：

| 工具            | 归属               | 直接消费者                                     |
| --------------- | ------------------ | ---------------------------------------------- |
| Go              | 项目 mise 工具     | 构建、测试、`go run ./tools/...`、源码安装     |
| gopls           | 项目 mise 工具     | Serena 的 Go LSP、当前 checkout 的 Go CLI 查询 |
| govulncheck     | 项目 mise 工具     | Security workflow 和发布前漏洞扫描             |
| GoReleaser      | 项目 mise 工具     | CI snapshot、标签发布和本地归档验收            |
| Syft            | 项目 mise 工具     | GoReleaser 发布流程中的 SBOM                   |
| Node.js         | 项目 mise 工具     | npx、Serena 的 TypeScript/JSON/YAML 后台依赖   |
| Python 与 uv    | 项目 mise 工具     | uvx 入口、文本检查及独立 Python 环境选择       |
| Taplo           | 项目 mise 工具     | TOML 后台服务；避免 Serena 误用宿主旧工具      |
| Serena/Graphify | 按需 uvx 工具      | Codex、OpenCode 和 `mise run mcp:check`        |
| gh              | 用户工具           | Issue、PR、Actions 和发布查询或管理            |
| Codex/OpenCode  | 用户工具           | 启动项目 MCP、加载用户级技能和客户端权限       |
| RTK             | 用户工具，仅 Codex | 受信任项目中的 Codex 命令路由                  |
| Matt skills     | 用户工具           | 用户级工程流程；基础 CI 和本地检查不依赖它们   |

## CI 映射

GitHub Actions 中的 `jdx/mise-action` 负责安装 mise 本体。作业通过 `install_args` 只安装自身需要的
`[tools]` 子集，随后下载 Go 模块；不再在 action 安装完整工具集后重复调用 `mise run setup`。

| CI 作业               | 主要工具与验收                                                    |
| --------------------- | ----------------------------------------------------------------- |
| `source`              | Go、lint、文本工具、GoReleaser 配置；源码、契约和模板副本完整检查 |
| `race`                | Go、Node、Python、uv；竞态测试及测试内调用的文本任务              |
| `fuzz-benchmark`      | Go；短时 fuzz 和 benchmark                                        |
| `package`             | Go、GoReleaser、Syft；六目标 snapshot 与归档验收                  |
| `agent-mcp`           | Ubuntu MCP 验收；接入 Go、Node、uv、Taplo 和 gopls                |
| `agent-mcp-platforms` | macOS/Windows MCP；main/release 运行，依赖相同工具子集            |
| `platform-tests`      | Go、Node、Python、uv；PR 原生关键路径，main/release 全量测试      |
| `platforms`           | 全部 `[tools]`；原生 doctor、文本检查与构建                       |
| `vulnerability`       | Go、govulncheck；PR、main 更新和每日漏洞扫描                      |

`codex`、`opencode`、`rtk`、`gh` 和 Matt Pocock skills 不进入基础 CI 必需工具集。
同一 PR 的新提交会取消旧运行；标题和描述编辑不触发完整重跑，因为检查内容不读取这些字段。
缓存只复用 mise、Go、lint、npx 和 uvx 的下载、工具环境或编译产物，命中缓存不会跳过任何检查。

## 新增或升级工具

先按职责选择归属，再修改版本：

| 需求                                     | 应放置的位置                                 |
| ---------------------------------------- | -------------------------------------------- |
| 跨平台、跨任务执行且需要稳定版本的 CLI   | `mise.toml [tools]`                          |
| 偶尔执行的 npm 或 PyPI CLI、格式器、插件 | `mise.toml [vars]`，通过 npx/uvx 调用        |
| 产品、测试或仓库 Go 工具使用的库         | `go.mod` / `go.sum`                          |
| 仅本仓库使用的静态检查、验收或生成逻辑   | `tools/` 下的 Go 命令，由 mise task 编排     |
| 个人编辑器、智能体、模型、技能或凭据     | 用户级安装；项目只记录最低兼容要求或报告状态 |

升级项目工具时同步 README 受检版本表、doctor 预期、CI 引导和对应测试，再运行 `mise run setup`、
`mise run doctor`、`mise run lint:versions` 以及验证矩阵要求的检查。不得通过放宽版本、关闭自动安装检查
或把用户配置复制进仓库来绕过依赖问题。

## 诊断入口

```sh
mise current
mise run doctor
mise run lint:versions
mise run lint:workflow
```

`mise current` 显示当前 checkout 激活的版本；`doctor` 区分必需工具、MCP 入口和可选的用户工具；
`lint:versions` 核对 TOML、README、Go 声明和 CI 引导；`lint:workflow` 检查 Actions YAML 语义。
真实 Serena、Graphify 和后台依赖仍由 `mise run mcp:check` 验收，配置存在或 CLI 可执行不能替代连接测试。
