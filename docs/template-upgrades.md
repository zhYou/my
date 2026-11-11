# 模板升级

模板版本在 `.template-version`，当前为 0.4.1，与应用发布版本独立；同版本内的调整还须按来源提交识别。
初始化只对原始模板改名，不升级既有业务。新副本的 `.template-origin.json` 记录原模板、版本、目标身份和可确认的来源提交；
未知来源使用 unknown，同参数初始化保留来源字节，不拿派生项目 HEAD 冒充来源。无元数据的旧项目仍可构建检查。

## 升级前完成旧事务

在旧 checkout、旧工具和原参数下，先完成所有未终结的 `.template-init/` 事务，涵盖所有历史支持格式，
包括 format 2；不能仅因为新旧都叫 format 2 就用新工具续做。迁移受管文件路径会使旧 journal
不再对应新白名单。格式 1 同样先用旧工具恢复。备份业务及恢复现场，确认终态清理完成再合并升级。

相同参数重试先恢复；参数不同、摘要冲突、人工修改、未知内容或损坏日志会停下并保留现场。
命令目录移动只接受受管入口文件；额外文件、既有目标目录和不明确目录状态均拒绝，不覆盖或猜测。
不要删除锁或 journal 绕过保护。`.template-init.lock` 是常驻锁文件，是否占用取决于系统锁；
单文件同目录临时替换不等于跨文件/断电原子性。持久化提交完成或回滚完成终态后才逐个清备份，
最后删 journal；清理中断可继续，未知内容保留。临时路径由受管 journal 唯一推导，不能按前缀批量删文件。

## 历史布局迁移

历史版本曾移动入口、配置、测试及开发包。合并到当前模板时以这些最终位置为准：

| 职责                     | 当前位置                                                    |
| ------------------------ | ----------------------------------------------------------- |
| 程序入口、源码运行与安装 | `cmd/<name>/`                                               |
| 应用业务与公共机制       | `internal/app/`、`internal/base/`                           |
| 运行时外部进程           | `internal/command/`                                         |
| 开发工具与私有共享包     | `tools/<领域>/`、`tools/internal/`                          |
| 配置、集成测试与机器契约 | `configs/`、`test/integration/`、`test/testdata/contracts/` |

同步导入、静态入口识别、受管文件清单、GoReleaser main 和测试，不为迁移重建已移除的中间目录。
源码运行和安装使用 `cmd/<name>`；初始化器校验命令名和模块身份，不用于升级已有业务。
历史快照缺失可读取旧 Git 路径，损坏文件和 Git 错误不能当作“没有基线”。

## 迁移到集中消息目录和英文默认值

对仍采用分散 JSON 消息或默认 zh-CN 的派生项目：

1. 默认语言改为 en。需要保留中文时显式使用 `--lang zh-CN` 或 `<PREFIX>_LANG=zh-CN`，flag 优先于非空环境变量；zh 仍为别名，不读取系统区域。核对依赖帮助、结果正文和 schema `lang.default` 的脚本，机器字段和退出码保持。
2. 将模块双语 JSON 迁入 `internal/locales/active.en.toml` 和 `active.zh-CN.toml`，使用完整消息 ID、同时维护两种语言，删除旧 Catalog 接线。通过 `locales.LoadBundle()` 加载共享只读 Bundle，每次调用创建 Localizer；帮助和成功结果使用严格 Format，Text/Render 仅作诊断兜底。
3. 自行维护的应用配置使用受管 TOML 和统一加载入口；工具配置、JSON 数据及协议保留原格式。不增加运行时翻译覆盖或 `[i18n]` 配置。
4. 核对显式中文和默认英文，并按[验证矩阵](development.md#实现与检查)检查。参阅[兼容记录](compatibility/changes/2026-11-02-i18n-config.json)，同步调用方和部署环境。

## 迁移到 0.3.0

先备份业务与来源记录，用旧工具完成未终结事务，再选择已审查的模板提交人工合并：

1. 按上面的当前布局迁移代码与测试；不要只搬目录而遗漏调用校验、Session 生命周期或受管清单。
2. 将旧 `config.yaml` 的 `log_level` 改为唯一 `config.toml` 的 `[logging] level`，归档示例改为 `config.example.toml`。不再自动读取、合并或转换 YAML；加载拒绝未知键、弱转换和非法范围，显式 flag > 非空环境变量 > TOML > 默认值。旧 `<PREFIX>_LOG_LEVEL` 优先于新 `<PREFIX>_LOGGING_LEVEL`，flag 最优先。
3. 默认日志由 warn/stderr 改为 info/file。需旧出口时显式配置：

   ```toml
   [logging]
   level = "warn"
   output = "stderr"
   ```

   none 关闭日志，Trace 默认 none、file 仅本地诊断 JSON。合并 Session 的登记、等待和收尾顺序，借用 logger/IO 不 Sync/Close。

4. 核对 XDG、Application Support/Caches、APPDATA/LOCALAPPDATA 等私有路径。`MY_HOME` 必须为绝对根，改名 mycli 后使用 `MYCLI_HOME`；配置在根下，data/state/cache 分开。测试和示例同时隔离父子进程的此变量。
5. 早期 0.3.0 的默认 zh-CN 和模块 JSON 消息继续按上一节迁到当前实现。[CLI 基础记录](compatibility/changes/2026-10-30-cli-foundations.json)中的应用 v0.1.0→v0.2.0 是兼容决策，不代表已发布；模板来源和事务格式不因布局调整升级。

当前 API、配置和资源所有权见[设计指南](design-guidelines.md)，不运行 init 覆盖业务。

## 补全、帮助与 doctor 入口

合并四个 completion shell 子命令、独立 help/schema 和双语示例，保留 `completion <shell>`。
补全使用当前命令树，未完成的语言参数按有效参数、环境、en 兜底，普通调用仍严格校验；不读应用配置或创建诊断资源。
合并 app/doctor、Dependencies.Doctor 和结果 schema：空入口返回空数组并成功，真实检查由派生项目扩展，
已完成报告按 RequiredAvailable 决定 0/1，执行或渲染失败保持空 stdout。具体契约见[设计指南](design-guidelines.md#扩展-doctor)。

## 统一项目工具入口

将旧 `mise.agent.toml` 的固定版本 gopls 移入 `mise.toml`，删除旧配置和专用任务。
`setup:agent`、`doctor:agent` 改用 `mise run setup`、`mise run doctor`；gopls MCP 使用 `mise exec -- gopls mcp`，不再选择 agent 环境。
同步 Codex 项目配置、工具入口、CI 缓存键和受检版本声明；不改用户级工具、凭据或个人配置。
统一 setup 和 doctor 的当前范围见[开发说明](development.md#协作与环境)，产品 CLI 的 doctor 与机器契约不变。

## 按领域整理开发任务

旧任务不保留别名，调用脚本改用当前名称：

| 原任务           | 当前任务        |
| ---------------- | --------------- |
| `i18n:check`     | `lint:i18n`     |
| `config:check`   | `lint:config`   |
| `doc:check`      | `lint:doc`      |
| `versions:check` | `lint:versions` |
| `rules:check`    | `lint:test`     |
| `init`           | `template:init` |

移除 `tools/dev` 总入口和 `tools/init`，检查归入 `tools/lint`，初始化与副本验收归入 `tools/template`。
同步自定义 CI 的工具测试路径、受管清单和包文档；共享能力只放在 `tools/internal`。
只读格式和版本检查纳入 lint，规则自测、契约、构建与模板验收纳入 check；MCP、发布和安全检查独立。
最终按[验证矩阵](development.md#实现与检查)执行对应检查；涉及发布验收时在最终提交后生成 snapshot，再 release:verify。

## Serena 与按需文本工具

将 Codex 项目 MCP 从 gopls 改为统一 mcp:serve 的 Serena，保留固定 gopls 供 Go 后端及 CLI 使用。
合并六语言 `.serena/project.yml`，将项目名纳入初始化受管文件；按绝对 checkout 路径激活，避免 worktree 同名项目混淆。
Serena 使用 uvx 和独立兼容 Python，关闭 dashboard；后台依赖与真实验收见[开发说明](development.md#serena-查询与-go-cli-补充)。

从 mise 工具清单移除 npm/PyPI 文本包，将准确版本移到 `[vars]`；同步 npx/uvx task、Prettier 插件解析、doctor 和版本检查。
删除旧包安装布局探测及其测试，保留有效格式、语法、排除规则与只读回归。既有用户级安装无需卸载，不修改个人工具配置。
setup 继续显式准备 mise 工具与 Go 模块；npx/uvx 可在外部缓存按需下载，不再要求预热或禁止安装。
不在项目中生成 node_modules、.venv、package.json 或 npm 锁文件，检查不改写源码和配置。

## 项目级 Graphify 与 worktree 路径

合并 Graphify 的 mise 版本变量、CLI/建图/更新/服务 task 和项目 Codex 配置；使用官方 `graphifyy[mcp]` 包及独立兼容 Python。
同步版本检查、README 表、MCP 验收客户端和模板受管清单；已有 `mcp:check` 同时验收 Serena 与 Graphify，继续沿用三平台 MCP CI。
合并 `.graphifyignore` 和生成目录忽略规则，各 checkout 按需重建本地代码图，不复制旧索引或用户配置。
项目不安装用户 Skill，不迁移用户级 MCP；职责、单次会话关闭和故障处理见[开发说明](development.md#graphify-与工具分工)。

协作规则改为新建开发 worktree 固定放主工作区 `.worktrees/`；原生工具不能指定该位置时使用 Git，失败后不自动改放其他目录。
已有适合本任务且可写的 worktree 可继续复用，不批量搬迁或清理已有工作区。

## 迁移到 0.4.0：诊断契约

本节对应模板 0.4.0；准确来源提交以已审查 PR 或 Release 的完整提交 SHA 为准，不能仅凭版本号认定已同步。
先按本文完成旧初始化事务并备份业务，再人工合并；不对既有项目运行 template:init，不拿派生项目 HEAD 冒充模板来源。

1. 同步 fault 的 Diagnostic/Recovery、快照和敏感原因保护，再同步消息呈现、日志与 telemetry.RecordError 接线。原始原因仍能 errors.Is/As；对外文本会省略敏感解析值。字段与未知副作用语义见[错误规则](design-guidelines.md#错误保留结构文案在边界渲染)。
2. 同步 CLI JSON 模式预选、JSONL 诊断、Session RunID、收尾后最终错误及 Schema 发现。--json 的 stderr 从文本改为 JSONL，属于破坏变更：原有正则和整段 JSON 解析须改为逐行解析，按 type 选择 error/warning；普通日志仍用 event 等字段。成功 stdout、doctor 完成负向报告和退出码不变。
3. 同步领域阶段事件、错误诊断、双语消息、测试与契约工具。greet 阶段结束事件由 debug 改为 info，日志增加 error_details；使用日志过滤的消费者须核对流量和字段。保留自己的业务字段、配置及检查，不覆盖凭据来源或恢复逻辑。
4. 同步本次 AGENTS、设计与开发规范，参考脱敏 doctor 和部分完成夹具接入派生业务；空 doctor 默认仍不读配置、不联网、不创建诊断资源。
5. 更新契约快照和本项目兼容记录，隔离应用 HOME 后按[验证矩阵](development.md#实现与检查)运行对应检查。纯文档部分可单独同步，运行时错误类型、呈现、观测、双语资源和协议须作为一组验收。

my 的已提交兼容决策以 v0.3.1 为基线，本轮目标 v0.4.0 尚未发布；模板与应用版本独立，本次不创建标签或发布。原始模板来源为 88c55dc68019e220368e063dae487017ea8b939c，本次人工同步至 065fc74a424020155f9d849709b0baaa819ca685。
来源记录与初始化事务格式保持，不把用户配置或诊断数据复制到模板。

## 迁移到 0.4.1：开发约束自动检查

此版本保持产品 CLI、配置、诊断协议和退出码，增加开发门禁；既有项目可能因原有旁路调用或缺失消息而不再通过 lint。

1. 人工合并 .golangci.yml 的入口规则、精确豁免及说明，保留目标项目自己的边界；改名模块需同步既有 loggercheck/depguard 身份。
2. 同步 tools/lint 的引用检查、目录解析、入口和回归夹具，以及 mise.toml 的任务说明。x/tools 继续固定 v0.51.0，仅改为直接依赖；按模块变更流程 setup、doctor。
3. 业务改用注入的环境、IO、logger、tracer；直接 Span.RecordError 改为 telemetry.RecordError。基础设施确需新入口时审查精确豁免并增加正反例，不关闭整类检查。
4. 为 DIAG001 报告的常量消息 ID 同步补齐两种语言。动态 ID、诊断继承及无 Code 的归一化包装保持原行为，仍须提供行为测试。
5. 同步协作、设计和开发说明；规则范围集中在[静态规则](development.md#静态规则与开发探测)。运行完整 check 和宿主 race，核对改名副本同样执行规则和消息检查。

无需重新运行 template:init 或覆盖业务文件。同步时以 v0.4.1 标签的准确提交作为来源，人工记录本次合并范围；
不把派生项目 HEAD 写作模板来源，也不改写既有 .template-origin.json。

## my 的模板同步记录

2026-11-11 从模板来源 `065fc74a424020155f9d849709b0baaa819ca685` 人工同步至
`main@3dcd7c8d9e9632a7857b091afa107ae31a0b3c7b`，模板版本由 0.4.0 更新为 0.4.1。
`.template-origin.json` 保留最初初始化的版本、来源和目标身份，当前模板版本由 `.template-version` 声明。

本轮合入开发入口与诊断消息静态门禁、Graphify 本地代码图及 MCP 验收、worktree 协作规则，
以及 Go 1.27.4 安全补丁和相关工具声明。通过旧、新公共模板副本各自的初始化器转换相同 my 身份后进行三方合并，
保留 my 的业务、文档、配置和原有故障回归，不在业务 checkout 执行初始化。

产品入口继续使用 `machine`、`target`、`env` 与 `activate`；模板同步不改变这些命令的机器契约，
应用兼容决策仍见[兼容记录](compatibility.md)。新建开发 worktree 使用主工作区 `.worktrees/`，已有任务工作区继续复用。
