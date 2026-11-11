# my

`my` 是个人数据与资源管理 CLI，支持机器登记、配置文件管理与同步，以及 shell 环境变量声明和激活。
代码仓公开，实际数据位于独立私有仓库；敏感数据使用 age 加密，唯一解密凭据由用户在仓外保管。

当前支持私有数据仓初始化、机器登记与查询、本机绑定、退役、配置收录与双向同步、
age 加密认证、shell 环境变量激活，以及只读 doctor 检查。
CLI 提供文本和 JSON 输出、双语消息、严格 TOML 配置、Schema、shell 补全、日志和 Trace。
`greet` 保留为模板验收示例；账号和云服务管理留待后续模块实现。

## 项目定位与数据边界

| 内容                                    | 保存位置                              |
| --------------------------------------- | ------------------------------------- |
| 程序、适配器、校验规则、测试和脱敏示例  | 公开代码仓 `zhYou/my`                 |
| 机器、账号、云服务记录和配置期望值      | 独立私有数据仓；敏感内容使用 age 密文 |
| 唯一 age 解密凭据及访问数据仓的认证凭据 | 用户自行在仓外保管                    |
| 同步基线、缓存、日志和恢复记录          | 本机应用私有目录                      |

配置管理采用 GitOps 的声明式期望状态和版本追踪思路；账号等资料采用版本化记录。
配置按字段声明管理范围并进行结构校验，保留应用自行维护及本机专属的字段。
远端数据通过 GitHub API 访问，本地保存必要状态和缓存，不维护完整数据仓 Git 工作树。
同步使用固定提交三方比较和 CAS 提交，冲突必须显式解决。

软件升级、系统/GUI 环境变量和向运行中应用注入不属于业务范围；
`my activate` 只输出由用户主动加载到当前 shell 的变量代码。

## 本地环境

[安装 mise](https://mise.jdx.dev/getting-started.html)。mise、Git、gh、Codex、RTK 和 superpowers 由用户维护；
mise 安装声明的运行时、Go 和原生工具；文本工具、Serena 与 Graphify 通过固定版本的 npx/uvx 在外部缓存按需准备。

<!-- tool-versions:start -->

预装工具的版本门槛：

| 工具                              | 固定版本   |
| --------------------------------- | ---------- |
| mise（本地最低版本、CI 固定版本） | 2026.10.21 |

开发工具：

| 工具                 | 固定版本 |
| -------------------- | -------- |
| Go                   | 1.27.4   |
| golangci-lint        | 2.15.0   |
| actionlint           | 1.7.12   |
| GoReleaser           | 2.19.0   |
| govulncheck          | 1.9.0    |
| Syft                 | 1.55.0   |
| gopls                | 0.24.0   |
| Node.js              | 26.13.0  |
| Python               | 3.15.2   |
| uv                   | 0.13.7   |
| Taplo                | 0.10.0   |
| Prettier             | 3.9.10   |
| @prettier/plugin-xml | 3.4.2    |
| prettier-plugin-toml | 4.1.0    |
| markdownlint-cli2    | 0.24.0   |
| yamllint             | 1.38.0   |
| pre-commit-hooks     | 6.0.0    |
| Serena               | 2.1.0    |
| Serena Python        | 3.14.10  |
| Graphify             | 0.9.106  |
| Graphify Python      | 3.14.10  |

<!-- tool-versions:end -->

克隆后在仓库根目录准备开发环境：

```sh
mise trust
mise run setup
mise run doctor
mise run check
```

首次准备环境、工具版本或 Go 模块变更后执行 `mise run setup`，再运行 `mise run doctor`；声明未变时无需重复 setup。
mise 普通任务和 exec 关闭运行时自动安装，`GOTOOLCHAIN=local` 防止隐式下载 Go 工具链。
npx/uvx 可按需下载固定版本，检查不改写项目文件，不创建本地 `node_modules`、`.venv` 或 npm 工程。
项目不使用 mise 锁文件，`go.sum` 校验模块。doctor 的必需项和可选报告见[开发说明](docs/development.md#协作与环境)。

```sh
mise run build
MY_HOME="$(mktemp -d)" mise exec -- go run ./cmd/my greet --name Codex --json
```

输出：

```json
{"message":"Hello, Codex!"}
```

发布程序不依赖 mise 或 Go。源码安装入口是 `cmd/my`：

```sh
go install github.com/zhYou/my/cmd/my@latest
```

## 给智能体调用

```sh
./bin/my --help
./bin/my schema
./bin/my schema greet
./bin/my doctor --json
./bin/my target diff --json
./bin/my target sync --json
./bin/my env list --json
./bin/my activate bash
./bin/my help completion bash
MY_HOME="$(mktemp -d)" ./bin/my greet --name Codex --json
./bin/my version --json
```

- 默认文本，`--json` 不由终端、配置或环境隐式开启；缺少必需参数立即失败，不交互询问。
- 成功结果写 stdout，错误和告警写 stderr；`--json` 时诊断为带 type 的 JSONL，日志使用独立出口。执行或序列化失败时 stdout 为空；底层写入失败可能留下部分字节，须先检查退出码。
- `schema [命令路径...]` 始终输出 JSON，包含命令树、参数、默认值和退出码；各业务命令及 greet、version、doctor 提供 Draft 2020-12 结果结构。结果允许新增字段，配置文件仍拒绝未知键；diagnostic_schema 描述结构化错误和告警。
- 帮助是文本，补全是原生 shell 脚本，不受 `--json` 影响。验证真实退出码请用构建后的程序，`go run` 会包装非零退出码。

| 退出码 | 含义                |
| ------ | ------------------- |
| 0      | 成功                |
| 1      | 运行或配置错误      |
| 2      | 命令或参数错误      |
| 130    | 信号或 context 取消 |

## doctor 与 shell 补全

`mise run doctor` 检查开发环境；产品 CLI 的 `doctor` 默认离线、只读，检查路径、配置、gh、
本地凭据可读性和本机绑定。`doctor --remote` 显式访问配置的数据仓，追加私有仓、分支、数据格式与远端绑定检查。
`--timeout` 为每项检查的限时，默认 10s，须为正 duration。检查串行执行，不登录、刷新凭据、初始化或修复，
也不创建 Session 日志、Trace、缓存或状态锁。`auth.local` 只证明本地凭据可读；联网只读检查不证明写权限。

检查状态为 available、failed、skipped；未启用的远端项不出现在离线报告中。前置条件不足时 skipped，
未绑定机器属于合法状态。必需项失败或被阻断时输出完整报告并退出 1，可选项不改变结果；
父调用取消保持空 stdout、退出 130。报告的 code 为安全稳定标识，duration_ms 为耗时；不输出原始错误正文。

支持 `completion bash`、`zsh`、`fish`、`powershell`，可对具体 shell 请求 help 或 schema；`completion <shell>` 调用保持可用。
Bash 先加载系统已有的 bash-completion；其他 shell 按其要求加载依赖。CLI 只生成脚本，不安装或修改 shell 配置：

```sh
./bin/my completion bash > my.bash
source ./my.bash
```

## 配置、语言和诊断

只读取一个 `config.toml`，示例见 [configs/config.example.toml](configs/config.example.toml)。
`--config` 指定显式文件，否则使用应用私有配置目录。默认文件缺失可继续，显式文件缺失、非普通文件、未知键、类型或范围错误会失败；指向普通文件的符号链接仍可使用。
配置优先级为显式 flag > 非空环境变量 > TOML > 默认值，不自动读取或合并 YAML。

```toml
[logging]
level = "info"
output = "file" # file、stderr、none

[telemetry]
exporter = "none" # none、file
```

模块环境变量如 `MY_LOGGING_LEVEL`、`MY_LOGGING_OUTPUT`、`MY_TELEMETRY_EXPORTER`。
兼容 `MY_LOG_LEVEL` 优先于新的级别变量，显式 `--log-level` 优先于两者；未给出的 flag 不覆盖文件或环境值。

语言独立于配置：`--lang` > `MY_LANG` > en，中文使用 zh-CN 或别名 zh，不读取系统区域。
帮助、schema 描述、结果、错误及日志使用所选语言；命令、选项、JSON 字段、枚举和退出码保持稳定，第三方诊断可保留原文。
帮助、version、completion、schema 不读取配置或创建诊断目录，坏配置下仍可发现命令。不支持运行时翻译覆盖或 `[i18n]` 配置。

所有平台默认采用统一 HOME 布局：Linux/macOS 为 `~/.my`，Windows 为 `%USERPROFILE%\.my`。
根下 `config.toml`、`data/`、`state/`、`cache/` 使用同一路径策略；非空 `MY_HOME` 可覆盖为绝对根目录。
`--config` 仅改变配置文件位置。相对 MY_HOME 会报错；不再自动使用系统 XDG、Application Support 或 AppData 路径。
旧路径中的配置和状态须由用户按[兼容迁移](docs/compatibility.md)明确处理，不自动迁移旧 machine-config 数据。

数据仓配置：

```toml
[data]
repository = "example/private-data" # 使用数据业务或就绪 doctor 前必填，必须为私有仓
branch = "main"
request_timeout = "30s"
```

对应环境变量为 `MY_DATA_REPOSITORY`、`MY_DATA_BRANCH`、`MY_DATA_REQUEST_TIMEOUT`，空值视为未设置。
超时须为正 duration。缺少数据仓不阻塞 help、schema、version 或示例 greet；数据业务在创建 Session 前校验。
认证通过外部 `gh` 的现有登录状态获得，仅保留于本次调用内存。my 不登录、不保存 token，支持时使用 `gh auth token --no-refresh`。
API 客户端固定使用 go-gh/v2 v2.17.0，禁用环境调试输出和 HTTP 缓存；`GH_DEBUG` 不绕过项目输出策略。

age 身份路径只在秘密目标实际读写时加载，必须由用户保存在仓外：

```toml
[secrets]
identity_file = "/absolute/path/to/age-identity.txt" # 也可用 MY_SECRETS_IDENTITY_FILE 或 --age-identity
```

支持单个原生 X25519 或 Hybrid 身份，不支持 SSH、插件或口令，也不自动生成或复制凭据。

## 数据仓与机器生命周期

用户先创建私有 GitHub 仓库、初始提交及目标分支并通过 gh 完成认证，再配置仓名与分支：

```sh
my data init
my machine register workstation --kind desktop --note "Synthetic example"
my machine list --json
my machine show workstation --json
my machine current --json
my machine bind workstation
my machine retire workstation
my machine bind workstation --reactivate --update-platform
```

`data init` 幂等写入根 `my.toml` 的 schema_version=1；缺少标记而已有 machines 时拒绝接管。
机器文件位于 `machines/<id>.toml`：ID 为 1–32 位小写字母、数字和连字符，首尾须为字母或数字；
kind 为 laptop、desktop、server、vm；os/arch 由当前平台取得，note 为非空单行，登记时间为 UTC。
状态为 active 或 retired，退役记录保留登记资料并增加 retired_at。备注不会加密，勿写入秘密。

相同 active 资料重复登记不提交新版本，并可补齐本机绑定；资料不同或已退役则拒绝登记。
`bind` 保留原 kind、note 和登记时间；退役机器要求 `--reactivate`，平台变化要求 `--update-platform`。
两个条件同时存在时须同时给出。绑定保存仓名、仓数字 ID、分支与机器 ID，不覆盖其他身份。仓名比较忽略大小写，其余身份字段严格比较。
`current` 只读本机，其他查询读取固定远端 HEAD；退役只清除精确匹配的本机绑定。

远端变更以 expectedHeadOid 作 CAS，不自动重试。并发 HEAD 变化先重新查询；提交结果未知时，
先用 show/list、doctor --remote 核实远端状态，再决定重跑登记或绑定。远端已成功而本机写入失败时，
诊断列出 data.commit 和已确认版本，修复本机路径后 bind 补齐，不需重发提交。
`completed_effects=null` 表示未知，`[]` 表示确认没有业务副作用；按 phase 和 recovery 处理。
单次请求超时退出 1，父调用取消或截止退出 130。不得根据超时直接推断远端未提交。

日志默认写 `<state>/logs/<run_id>/events.jsonl`；Trace 默认关闭，file 写同次运行的 `trace.jsonl`，只供本地诊断，不承诺 OTLP 或重放。
诊断故障告警并继续业务。错误包含阶段、已确认副作用和恢复建议；JSONL 逐行按 type 识别 error/warning，日志仍按 event 识别。轮转和清理边界见[诊断资源所有权](docs/design-guidelines.md#日志trace-和异步工作的所有权)。
验证示例时隔离应用配置与状态：

```sh
example_home=$(mktemp -d)
cp configs/config.example.toml "$example_home/config.toml"
MY_HOME="$example_home" ./bin/my greet --name Codex --lang zh-CN --json
```

## 目标同步与环境激活

目标用于管理配置文件、结构化字段及认证快照等个人资源。机器、目标、环境分别由 `machine`、`target`、`env` 管理。
每个目标共享一份内容，按机器集合限定范围；Windows、WSL、remote dev 使用独立机器身份和本机状态。
平台适配映射路径和纳管单位，不增加通用覆盖层或环境分组。

配置首次进入数据仓必须显式收录；`capture --all` 不包含秘密目标：

```sh
my target capture codex-settings codex-rules mise
my target capture --all
my target diff
my target sync
my target resolve codex-settings --unit field:features.network_proxy --from remote
my target remove topgrade
```

内建目标为 `codex-settings`、`codex-rules`、`codex-deepseek`、`codex-auth`、`mise`、
`topgrade`、`clash-global`、`clash-lan`、`shell-env` 和 `shell-env-secret`。
`clash-lan` 默认只适用于执行 capture 的当前机器，其余文件目标默认适用全部 active 机器，
`--machine` 可缩小范围。

数据仓布局为：

```text
configs/
  manifest.toml
  targets/<target>/target.toml
  targets/<target>/data/...
```

字段同步只修改声明单位，保留未纳管字段、注释和排版；完整文件按整份内容比较。
`diff` 只读并返回稳定单位 ID。`sync` 遇到任何冲突时，本轮不写本机、不提交远端；
解决一个冲突后重新运行 `sync`。远端提交使用 expectedHeadOid CAS，不自动重试。

Codex 认证目标把完整 `auth.json` 保存为 age 密文。只有显式 `capture codex-auth`
或冲突选择 local 时才发布新的登录版本；相同版本同步不会覆盖 Codex 在各机器本地刷新的 token。
普通同步只在远端认证版本变化时解密认证快照；没有秘密变化时不读取认证密文内容。

环境变量以数据仓为权威来源，值只从 stdin 读取，避免命令行历史和进程参数泄漏：

```sh
printf '%s' 'nvim' | my env set EDITOR
printf '%s' 'secret-value' | my env set DEEPSEEK_API_KEY --secret
my env list
my env remove EDITOR
```

`sync` 只下载普通声明或秘密密文到 `<state>/shellenv` 缓存，不要求 age 身份；
`activate` 离线读取缓存，在需要时解密秘密变量。支持四种 shell：

```sh
eval "$(my activate bash)"
eval "$(my activate zsh)"
my activate fish | source
my activate powershell | Out-String | Invoke-Expression
```

`my` 不修改 RC、fish 配置、PowerShell profile、系统环境或 LaunchAgent，启动文件中的 activate
调用由用户手工加入。`activate --json` 无效；成功后 stdout 只包含 shell 赋值代码，不创建 Session
或持久日志。危险变量名（如 `PATH`、`BASH_ENV`、`LD_*`、`NODE_*`）会被拒绝。
自定义脚本环境和向运行中应用注入不支持。

## Codex 开发

环境准备完成后，从仓库根目录启动 `codex`。当前仅支持 Codex，RTK 和 superpowers 使用用户级安装。
受信任项目通过 `.codex/config.toml` 和 `mcp:serve`、`graphify:serve` 启动 Serena 与 Graphify，服务启动失败不阻塞 Codex。
Serena 使用独立兼容 Python 并关闭 dashboard，覆盖 Go、JavaScript、JSON、YAML、TOML 和 Markdown。
Graphify 使用独立兼容 Python 建立本地代码图，CLI 负责建图和更新，MCP 查询关系；不需要 API Key。
创建或切换 worktree 后，以绝对路径激活 Serena 并核对根目录；Graphify 每次查询传当前 checkout 的绝对 `project_path`。
未覆盖的 Go 能力使用当前 checkout 的 gopls CLI，精确文本使用 rg；职责与回退见[开发说明](docs/development.md#graphify-与工具分工)。

需要代码图时执行 `mise run graphify:build`，修改后使用 `mise run graphify:update`；索引位于被忽略的 `graphify-out/`，各 checkout 独立维护。
单次会话不需要这两个 MCP 时：

```sh
codex -c 'mcp_servers.serena.enabled=false' -c 'mcp_servers.graphify.enabled=false'
```

连接验收见[开发说明](docs/development.md#实现与检查)，协作、分支及交付规则见 [AGENTS.md](AGENTS.md)。

## 智能体文档上下文

项目整体架构、具体设计、后续规划及个人使用安排保存在独立私有仓。仍可从 CLI 仓库启动 Codex；
本仓 [AGENTS.md](AGENTS.md#私有文档上下文) 通过共享本地 Git 配置中的 `my.privateDocsRepository` 和
`my.privateDocsBranch` 发现私有文档，先读取私有协作约定与索引，再按任务读取固定提交下的相关设计。
具体映射由维护者在本地配置，同仓 worktree 复用，不进入公开文件，也不修改用户级 Codex 配置。

公开仓保留通用命令、数据格式、内建目标、兼容性与开发规范。安装、使用、构建、测试和 CI 不要求访问私有设计；
依赖私有设计的任务须先取得对应上下文，访问失败时按协作约定处理。

## 开发命令

优先使用已有 task；直接调用项目纳管工具使用 `mise exec -- <命令>`，Git、gh 等其他命令直接执行。
task 内由 mise 提供工具环境，无需额外包装。

| 命令                                                         | 用途                                             |
| ------------------------------------------------------------ | ------------------------------------------------ |
| `mise run setup` / `mise run doctor`                         | 显式准备并检查环境                               |
| `mise run build`                                             | 构建到 bin，注入版本信息                         |
| `mise run fmt`                                               | 显式格式化 Go 和 JSON、XML、TOML、Markdown、YAML |
| `mise run lint`                                              | 只读检查 Go、五类文本、工作流语义、资源和声明    |
| `mise run check`                                             | 完整源码、测试、构建、契约与模板验收             |
| `mise run template:init --module <模块路径> --name <命令名>` | 初始化模板身份                                   |

完整任务表、直接工具命令和按改动选择的检查见[开发说明](docs/development.md#开发命令)。
本地检查不等于全部 CI；平台运行、发布、安全和托管要求见[维护清单](docs/maintenance.md)。

## 发布

维护者确认发布后推送 `v*` 标签，Release 先运行完整 CI 和 Security，再发布 Linux、macOS、Windows 的 amd64/arm64 产物。
归档附带配置示例、SPDX、校验和及来源证明；验证命令、权限和失败重试见[发布流程](docs/maintenance.md#升级和发布)。
目标项目发布前须配置自己的 GitHub origin、安全报告入口和托管规则。模板没有预选许可证，按项目需要添加。

## 文档导航

| 文档                                  | 内容                                 |
| ------------------------------------- | ------------------------------------ |
| [协作约定](AGENTS.md)                 | 智能体入口、命令、工作区、交付与保护 |
| [设计指南](docs/design-guidelines.md) | 架构、CLI 契约、API 示例与所有权     |
| [开发说明](docs/development.md)       | 完整命令、验证、测试组织与工具维护   |
| [兼容性规则](docs/compatibility.md)   | 机器接口与变更记录                   |
| [模板升级](docs/template-upgrades.md) | 来源记录、旧事务和迁移步骤           |
| [维护清单](docs/maintenance.md)       | 托管配置、CI、发布与验收证据         |
| [安全报告](SECURITY.md)               | 目标项目须填写的私密报告入口         |
| [项目待办](docs/backlog.md)           | 明确要求记录的探索想法               |

本项目最初基于 [go-cli-starter](https://github.com/zhYou/go-cli-starter) 0.4.0，
初始化来源 `065fc74a424020155f9d849709b0baaa819ca685` 保存在 [.template-origin.json](.template-origin.json)。
当前已人工同步模板 0.4.1，来源提交为 `0ba85f418512dd6eae1b3b8a0066354f2042e03d`。
模板版本与应用发布版本独立；同步范围见[模板升级](docs/template-upgrades.md#my-的模板同步记录)，不运行初始化覆盖业务。

身份路径按显式 `--age-identity`、非空环境变量、配置文件的顺序选择，仅在实际加解密时读取。
目标基线和环境缓存按机器隔离；复制状态文件到另一逻辑机器不能复用绑定或缓存。
远端提交成功而本机失败时，错误报告确认的副作用和数据版本；先检查 `target diff` 与恢复建议。
认证替换中断时保留 pending 状态，修复本机写入问题后重试 `target sync`。
环境写入已确认、缓存更新失败时，先运行 `target sync` 刷新缓存，再执行 `activate`。
