# 开发说明

环境版本与首次使用见 [README](../README.md#本地环境)，协作、工作区与交付见 [AGENTS.md](../AGENTS.md)。
本文件集中开发命令、验证、包文档、测试组织和工具维护；运行时契约见[设计指南](design-guidelines.md)。

## 协作与环境

项目命令在仓库根目录执行；优先使用 task，直接调用项目 `[tools]` 纳管工具时用 `mise exec --`，其他命令直接执行。
首次准备环境、工具版本或 Go 模块变更后显式 setup，再 doctor；声明未变且环境已准备时不重复安装。
mise 普通任务和 exec 关闭运行时自动安装，`GOTOOLCHAIN=local` 禁止隐式下载 Go 工具链。
npx、uvx 可按固定版本下载并在外部缓存准备工具环境；不生成项目内依赖目录、npm 工程或锁文件。
检查不改写源码和配置，下载失败保留非零退出；缓存命中不跳过检查。

`mise.toml` 的 `[tools]` 固定运行时、Go 和原生工具，`[vars]` 固定按需执行工具及插件版本；禁止 latest 和版本范围。
setup 只安装 mise 工具并下载 Go 模块，不强制预热 npx/uvx 或语言服务。mise、Git、gh、Codex、RTK 和 superpowers 由用户维护。
doctor 要求 Git 可用、mise 达到最低版本、mise 工具及运行环境版本准确，并确认 npx、uvx 入口存在。
它不扫描 npm/Python 包缓存，不通过逐包版本探测触发安装；工具功能由实际 task 验收。gh、Codex、RTK 仅报告状态，
缺失、执行失败、超时或截断不影响结果，不检查认证或固定版本。superpowers 以会话技能列表为准。
基础检查无需 Codex、RTK 或 superpowers；Serena MCP 的真实验收独立运行。

开发 doctor 最多四项并行，每项三秒、每流 2 KiB；govulncheck 核对本地构建来源和版本并执行 `-h`，不访问漏洞库。
真正联网扫描使用 `mise exec -- govulncheck ./...`，数据库访问失败同样失败。
actionlint 通过 `-version` 离线核对准确版本，属于必需工具。
项目关闭 mise 锁文件，准确版本取自 TOML，后端解析并校验下载来源，模块由 `go.sum` 校验。
产品 CLI 的 doctor 是数据仓与机器绑定的只读检查入口，见[设计指南](design-guidelines.md#扩展-doctor)。

仓库文本使用 LF，由 `.editorconfig` 和 `.gitattributes` 约束。调整换行规则时确认只有本次修改，
再执行 `git add --renormalize .` 并审查差异。个人模型、凭据、权限、插件与 IDE 设置不写入模板。

## 开发命令

mise 聚合流程，领域 Go 入口只处理各自能力；参数不足、未知或多余时立即失败。

| 命令                                                         | 用途                                                                           |
| ------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `mise run setup`                                             | 安装 mise 声明的运行时与工具，下载 Go 模块                                     |
| `mise run doctor`                                            | 检查项目环境，报告用户工具状态                                                 |
| `mise run fmt`                                               | 显式格式化 Go 和五类文本；分项可指定路径                                       |
| `mise run fmt:go -- <路径...>` / `fmt:text -- <路径...>`     | 仅格式化指定 Go / 文本文件或目录；省略路径处理全部受检文件                     |
| `mise run lint:text`                                         | 只读检查 JSON、XML、TOML、Markdown、YAML 的格式、语法及规则                    |
| `mise run lint:workflow`                                     | 检查 GitHub Actions 工作流的语法、表达式、上下文与任务依赖；纳入 lint 和 check |
| `mise run lint:syntax`                                       | 用现成 CLI 检查 JSON、XML、TOML 语法和重复键                                   |
| `mise run lint`                                              | 只读检查格式、双语消息、应用配置、包文档、源码风格、版本声明和 34 项静态规则   |
| `mise run lint:i18n` / `lint:config`                         | 校验双语消息目录 / 受管应用配置；均纳入 lint 和 check                          |
| `mise run lint:style`                                        | 检查具名和匿名结构体字段逐项声明；纳入 lint 和 check                           |
| `mise run lint:doc`                                          | 检查正式 Go 包的 doc.go 与包说明位置；纳入 lint 和 check                       |
| `mise run lint:test`                                         | 用真实 fixture 测试静态规则自身；仅纳入 check                                  |
| `mise run build`                                             | 构建到 bin，注入版本信息                                                       |
| `mise run check`                                             | 完整源码检查，含测试、构建、契约和模板验收                                     |
| `mise run template:check`                                    | 在临时副本中验证改名、lint、测试、构建、安装和发布配置                         |
| `mise run template:init --module <模块路径> --name <命令名>` | 初始化模板身份                                                                 |
| `mise run contract:check` / `contract:update`                | 检查 / 显式更新机器契约                                                        |
| `mise run lint:versions`                                     | 检查工具、模块、CI 和文档版本一致性                                            |
| `mise run mcp:check`                                         | 验收 Serena 六后端、真实调用和同连接 worktree 切换                             |
| `mise run release:verify`                                    | 验证发布归档，并隔离安装宿主程序                                               |

直接调用纳管工具无需 shell 激活；以下直接工具命令也可用于针对性验证：

| 命令                                                                 | 用途                                      |
| -------------------------------------------------------------------- | ----------------------------------------- |
| `mise exec -- golangci-lint fmt --color=never`                       | 使用 gofumpt、goimports 和 golines 格式化 |
| `mise exec -- golangci-lint fmt --diff --color=never`                | 只读检查格式；有差异时失败                |
| `mise exec -- go test ./...`                                         | 单元与命令测试                            |
| `mise exec -- go test -race ./...`                                   | 竞态检测，需要 C 编译器                   |
| `mise exec -- go vet ./...`                                          | Go vet 检查                               |
| `mise exec -- go mod tidy -diff`                                     | 只读检查模块元数据                        |
| `mise exec -- govulncheck ./...`                                     | 联网扫描 Go 漏洞                          |
| `mise exec -- goreleaser check`                                      | 验证发布配置，需要 Git remote             |
| `mise exec -- goreleaser release --snapshot --clean --parallelism 2` | 本地构建全部发布压缩包，不发布            |

Linux CI 另执行五秒 argv fuzz 和没有性能门槛的 benchmark：

```sh
mise exec -- go test ./internal/app/cli -run=^$ -fuzz=FuzzArgv -fuzztime=5s
mise exec -- go test ./internal/app/cli -run=^$ -bench=. -benchmem
```

`lint` 聚合五类文本、工作流语义、消息、配置、包文档、源码风格、版本、格式及静态规则；`lint:test` 验证规则自身，仅纳入 check。
`check` 进一步完成测试、vet、模块元数据、构建、机器契约和模板副本验收；它不改源码，但会写缓存、临时文件和 bin。

## 文本格式与检查

Prettier 通过同一 npx 调用取得固定版本本体、XML 和 TOML 插件，统一格式化 JSON、XML、TOML、Markdown、YAML。
根目录 `.prettierrc.cjs` 从实际 Prettier CLI 入口解析同一缓存环境中的插件，不依赖 mise 安装布局或全局模块。
`.prettierignore` 排除生成快照、vendor 和故障夹具。markdownlint-cli2 使用 npx；yamllint 和 pre-commit-hooks 使用 uvx，
通过 `--python python` 选择 mise 提供的项目 Python。所有工具与插件版本由 `[vars]` 明确声明，允许冷缓存按需下载。
格式化与检查不需要本地 node_modules、.venv、package.json 或 npm 锁文件；配置文件仍集中在仓库根目录。

文本使用两空格、UTF-8、LF 和末尾换行，`.editorconfig` 与格式器一致。
Prettier 目标行宽为 120，保留 Markdown 人工换行和代码块内容，XML 使用严格空白模式。
README 的应用路径表随模板改名改变列宽，用局部 `prettier-ignore` 保留格式，初始化不额外改写文档。

`.markdownlint-cli2.yaml` 检查标题层级、同级重复标题、代码块语言、空链接和引用；不要求中文段落固定行长。
`.yamllint.yaml` 检查重复键、锚点和布尔值歧义，允许 Actions 的 `on` 键；排版由 Prettier 检查。
JSON、XML、TOML 的语法由 pre-commit-hooks 包的 `check-json`、`check-xml`、`check-toml` 检查，
只运行现成命令，不启用 Git hooks。没有业务 Schema、拼写或联网链接检查。

`lint:workflow` 使用固定版本 actionlint 检查 GitHub Actions 工作流，支持 YAML 锚点与别名。
显式关闭 ShellCheck 和 Pyflakes 集成，不受本机是否安装可选检查器影响。
远端 Action 输入检查受 actionlint 内置元数据及引用形式限制，不承诺解析所有固定 SHA 的输入；修改这些输入仍须核对对应 Action 定义。

文件选择覆盖隐藏配置，遵循仓库 Git 忽略规则；vendor 和 `tools/lint/testdata/` 明确排除。
`test/testdata/contracts/` 和 `.template-origin.json` 保留生成器格式，仍检查语法。
`lint:syntax` 使用 Git 的文件列表，因此新建项目须先 `git init`，并在检查前暂存删除操作。
任务不按成功缓存跳过检查；检查输出到终端，临时列表放在系统临时目录，不产生仓库内报告或依赖目录。
`.gitignore` 仅登记实际产物，本次工具接入无需新增忽略项。配置文件提交到仓库。

## 目录和依赖

| 位置                                                        | 职责                                                                                           |
| ----------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `cmd/<name>/`                                               | 程序入口、信号、退出及入口测试                                                                 |
| `internal/app/cli`                                          | 独立命令树、参数、输出和退出码映射                                                             |
| `internal/app/config`                                       | 模块配置组合与严格加载                                                                         |
| `internal/app/session`                                      | 自建日志、Trace、诊断及工作收尾                                                                |
| `internal/app/configuration`                                | 配置目标、三方比较、基线和冲突解决                                                             |
| `internal/app/shellenv`                                     | 数据仓权威环境变量、本机缓存和 shell activate                                                  |
| `internal/app/greeting`                                     | 可移除业务示例                                                                                 |
| `internal/base`                                             | fault、messages、paths、validation、agecrypt、IO、logging、telemetry、diagnostics、结果 schema |
| `internal/locales`                                          | 集中双语 TOML 消息目录与共享 Bundle                                                            |
| `internal/command`                                          | 独立 argv、进程所有权、捕获和有界批量执行                                                      |
| `tools/doctor/`、`tools/build/`                             | 环境探测、本地构建                                                                             |
| `tools/lint/`                                               | 消息、配置、包文档、版本声明检查和规则自测                                                     |
| `tools/contract/`、`tools/template/`                        | 契约维护、模板初始化与隔离副本验收                                                             |
| `tools/mcp/`、`tools/release/`                              | MCP 连接、发布归档与宿主安装验收                                                               |
| `tools/internal/`                                           | 工具私有共享实现：模板事务、契约比较、工具版本、二进制验收、进程调用与 fixture                 |
| `configs/`、`test/integration/`、`test/testdata/contracts/` | 配置示例、真实进程脚本、共享契约基线                                                           |

base 不依赖 app、command、tools 或 Cobra。领域码及结果由所属模块定义，文案集中维护；新目录按实际职责增加。
运行时代码与测试不依赖开发包；工具可以复用运行时包。`tools/internal` 的导入范围由 Go 限制在 tools 子树内。
开发工具用 `scaffold.Inspect` 静态识别唯一入口，不根据模块路径末段猜测命令名。

mise 聚合完整流程，领域 Go 入口只承接自己的能力：`tools/lint` 接收 i18n/config/doc/style/versions/test，
`tools/contract` 接收 check/update，`tools/template` 接收 init/check；其他领域入口不接受位置参数。
初始化的 `--module`、`--name` 和契约检查的 `--against` 保持原用途。参数不足、未知或多余时立即失败。

`tools/internal/toolchain` 共享准确版本读取与探测规格，供 doctor 和版本声明检查使用；
`tools/internal/acceptance` 共享真实二进制的身份、双语帮助/schema 与配置优先级验收，供模板和发布使用。
scaffold 保留事务初始化与安全路径检查，contract 保留被验收复用的契约归一化与差异比较；
toolrun 和 fixture 分别共享进程调用与临时文件/Git fixture 操作。新增工具先确定领域，单处使用的实现就近保留。

`template:check` 在临时副本内调用同一 `mise run lint`，不维护第二份静态检查清单，也不递归调用 check。
本次子进程通过 `MISE_TRUSTED_CONFIG_PATHS` 只信任副本目录，并使用生成应用前缀隔离 HOME；
不写入用户的持久信任配置。源项目和生成项目分别接受检查，验证对象不同。

## 代码风格

以 [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments) 的惯用写法为基础，
采用 [Google Go Style Guide](https://google.github.io/styleguide/go/guide) 的清晰、简单原则，
参考 [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md) 的函数组织建议。
项目的具体取舍以本节为准；外部规范不整体引入，也不因上游调整自动重写代码。

### 排版与展开

`golangci-lint fmt` 统一运行 gofumpt、goimports 和 golines。golines 以 120 列为换行目标，
tab 按 4 列计算，不截短注释、不重排 struct tag；长字符串和 URL 不设硬性行长上限。
gofumpt 的输出兼容 gofmt；工具版本随项目固定的 golangci-lint 提供，不另行安装。
保存后或提交前格式化本次修改范围，`mise run lint` 和 `check` 只读检查格式。

- 所有非空函数、方法和匿名函数体都展开，单句返回和转发方法也一样；空函数体可保留 `{}`。
- 普通语句各占一行，不用分号压缩多条语句；`if` 初始化和 `for` 子句保留语言要求的分号。
- 非空具名 struct 和 interface 的成员分行声明；空类型保留 `struct{}` 等紧凑形式。
- 所有 struct 字段逐项声明，匿名测试表格也一样；不使用 `A, B string`，拆分时保持字段顺序、类型和 tag。
- 简单字面量和匿名表格类型交给格式工具处理；复杂嵌套字面量逐字段展开，便于核对依赖和资源所有权。
- 空行分隔输入校验、依赖准备、主要操作和结果处理等不同阶段；紧密相关的赋值、错误检查和使用放在一起。
- 不设置硬性行长、函数长度或空行数量。长表达式先考虑命名中间值或按职责拆分，避免为凑行数增加抽象。

字段逐项声明由 `lint:style` 检查；funcorder 检查构造函数、公开方法和公开函数的位置，不要求字母排序。
函数展开和逻辑分段由代码审查确认，格式工具不会全部代劳。例如：

```go
return writeText(cmd.OutOrStdout(), func(w io.Writer) error {
    _, err := fmt.Fprintln(w, result.Message)
    return err
})
```

### 命名、组织与注释

变量名结合使用范围表达含义：短作用域可用 `ctx`、`err`、`i` 等惯用名称，跨较长流程的变量使用明确名称。
避免重复包名或上下文已经表达的概念，同一类型的方法保持接收者名称一致；需要区分原实例与工作副本时按角色命名。

相关类型、构造函数和方法集中组织，同一接收者的方法成组；同一职责内公开入口优先，私有辅助函数随后按大致调用顺序排列。
以职责和阅读顺序组织文件，不为满足排列规则拆包、创建接口或增加文件。移动声明时保留初始化、注册顺序和编译指令。
错误及特殊情况优先处理并返回，减少主流程嵌套；资源取得后及时登记收尾，不为调整排版移动执行时机。

注释用中文说明职责、原因、限制和所有权，避免复述显然的语句。导出 API 和包说明继续遵守下节的 GoDoc 要求。
生产代码、开发工具和测试使用同一阅读风格；测试名称、断言和已有辅助代码归属保持各自职责。
测试数据、内嵌源码和协议快照按其测试意图保留，不参与批量风格改写。

## 包文档

每个正式 Go 包使用一份 `doc.go` 说明职责、主要使用方式和关键约束，包括 `main` 入口、
开发工具和 integration 测试包。它首先是包文档，IDE 折叠是可选的附加用途，不创建空占位文件。
说明按包的实际复杂度编写，不要求固定章节或字数；小包可以简短，多文件包应说明必要的协作边界。

```go
// Package greeting 提供独立于 Cobra 的问候业务示例，可按需移除。
//
// 直接业务调用也会校验输入，使用注入的 Localizer 生成所选语言的消息。
package greeting
```

普通包的首句以 `Package <包名>` 开头；`main` 包说明对应命令的行为。
包说明紧邻 `package` 声明，其他实现文件不重复放包说明。
`doc.go` 只包含注释与 `package` 声明，不放导入、函数、类型、变量或常量；测试辅助代码保留在测试文件中。

`mise run lint:doc` 按目录解析 Go AST，不依赖当前主机的 GOOS/GOARCH，也检查平台专用文件。
它拒绝缺失或空白的包说明、只有包名前缀与标点的占位说明、包名不一致、包说明位置错误，
以及 `doc.go` 中的实现声明。测试夹具 `testdata`、vendor、隐藏或下划线开头目录、根目录
bin/dist、`docs/superpowers/` 工作记录、生成代码和外部 `*_test` 包不受约束；普通 `*_test.go` 不需各自编写包说明。
现有 godoclint 继续检查注释格式、重复包说明和导出 API 文档，描述是否准确、有用由代码审查确认。
该任务纳入 lint 和 check，不会安装工具或修改文件。

## 实现与检查

先阅读[设计指南](design-guidelines.md)对应场景，编辑期间运行针对性检查；交付前按下表选择最终验证。
分类按实际影响判断：纯文字是说明和规则表述，不包括工具配置、版本表、模板身份或可执行示例。
多类改动取并集；完整 check 已覆盖的任务无需再单独执行。此表只调整本地要求，CI 和发布门禁保持完整。

| 改动                                          | 必需验证                                                                                                                                          |
| --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| 纯文字、协作规则                              | 格式化本次修改的文本，运行 `mise run lint:text`、`git diff --check`，核对本地链接、标题锚点及内容与实现一致性；无需 Go 格式化、全量 check 或 race |
| 受检版本表、工具声明格式                      | `mise run lint:versions`；工具或模块实质变更还须运行完整 check，并按 Go/依赖、MCP、打包与发布的影响补充对应验证                                   |
| 文档中的模板身份或可执行示例                  | 身份和受管文件变更运行 `mise run template:check`；修改示例时验证对应入口、参数或 API，执行时隔离应用 HOME                                         |
| Go 代码、测试、依赖、应用配置                 | 格式化受影响的 Go 文件，运行针对性测试、`mise run check`；支持的宿主运行 `mise exec -- go test -race ./...`                                       |
| MCP 配置、验收工具或影响 MCP 的 Go/gopls 升级 | 增加 `mise run mcp:check`；Codex 客户端配置变更在受信任会话核对 `codex mcp list` 包含 Serena，并验证实际调用                                      |
| 打包与发布流程                                | 按[发布验收](maintenance.md#升级和发布)完成 snapshot、release:verify 及发布前检查                                                                 |

消息和配置编辑期间分别可用 `lint:i18n`、`lint:config` 快速反馈；它们及 `lint:doc`、`lint:style`、`lint:versions`、
`lint:test`、`template:check`、`contract:check` 已由完整 check 覆盖。MCP、漏洞扫描、发布验收及其他 OS 原生运行独立执行。
只格式化授权的 Go 文件或目录，例如 `mise exec -- golangci-lint fmt --color=never internal/app/cli/greet.go`。
无法执行的检查说明原因，不把配置存在、启动成功或 mock 当作真实验收。

### Serena 查询与 Go CLI 补充

项目通过 `mise run mcp:serve` 启动固定版本的 uvx Serena。它使用独立 Python 3.14.10、LSP 后端和独立工具接口，
关闭 web dashboard、自动打开浏览器和 GUI 日志窗口；首次使用可在外部缓存下载运行环境与后台依赖。
Codex 启动超时为 300 秒、工具调用为 180 秒；独立 mcp:check 总超时为 10 分钟。

项目工作区为根目录 `.`，配置启用 go、typescript、json、yaml、toml、markdown；JavaScript/CommonJS 由 typescript 服务处理。
后台依赖为 gopls 0.24.0、TypeScript 5.9.3、typescript-language-server 5.1.3、vscode-json-languageserver 1.3.4、
yaml-language-server 1.19.2、Taplo 0.10.0 和 Marksman 2024-12-18。gopls、Taplo 与 Node.js 由 mise 提供；
其余后台依赖沿用固定 Serena 版本的内置准确版本，由 Serena 在外部缓存按需准备，不新增禁止安装或强制预热层。
Taplo 纳入 mise 可避免 Serena 将 PATH 中未配置的旧 shim 误认为可用后台程序。

首次读取 `initial_instructions` 并取得 session 信息。创建、切换或恢复 worktree 后，
先用绝对路径调用 `activate_project`，核对返回的根目录，再查询或编辑；共享项目名不能证明 checkout 身份。
六种语言各按后端实际能力提供符号与结构查询；JSON 不承诺跨文件引用。
隐藏配置、文档、CI 和故障夹具均纳入项目；普通文本、go.mod、go.sum 和 txtar 可通过非代码文本检索与 Codex 文件操作处理。
遵循 Git 忽略规则，排除构建产物、缓存和嵌套 worktree；Serena 项目配置提交，memory、日志、缓存及本地覆盖不提交。

Go 专用能力和 Serena 未支持的操作使用当前 checkout 的 gopls CLI，例如 `mise exec -- gopls call_hierarchy <位置>`、
`mise exec -- gopls codeaction <位置>`、`mise exec -- gopls check <文件...>`。
包 API、测试、漏洞检查继续使用 Go、现有 task 和 govulncheck。Serena 不可用时可使用 CLI，并报告限制；
不得使用其他 checkout 的结果证明当前改动正确。同一 checkout 串行修改。
MCP 集成变更必须通过真实 mcp:check，验证六后端、引用/重命名/诊断和同连接新建 worktree；CLI 回退不能代替验收。

### 测试与示例隔离

所有测试及示例验证隔离父进程和 helper 子进程的应用 HOME、配置、状态及缓存：

```sh
example_home=$(mktemp -d)
MY_HOME="$example_home" mise run check
MY_HOME="$example_home" mise exec -- go test -race ./...
```

改名为 mycli 后使用 `MYCLI_HOME`，由 `paths.EnvPrefix(name)+"_HOME"` 派生。
嵌入调用可注入 Directories、LookupEnv 与 IO；只设置 ConfigDir 不会隔离日志状态。
`IOStreams.WithDefaults` 返回副本，缺省输入 EOF、输出 io.Discard，不访问或关闭进程标准流。
真实退出码用构建后的程序验证，`go run` 会包装非零退出。输出测试检查单 JSON 与独立 stderr；
配置与认证测试使用合成 age 身份和临时数据仓替身，不读取用户 `auth.json`、真实 identity 或私有仓。
`activate` 测试同时检查离线、无 Session/日志、shell 语法和敏感值不进入诊断。
坏配置下帮助、version、completion、schema 不读配置、不落盘。
平台进程树、取消和句柄回收需对应 OS 原生 helper；交叉编译不证明运行行为，报告以实际结果为准。

### 能力验证与交付证据

行为标准见[配置](design-guidelines.md#配置使用严格的模块结构)、[错误](design-guidelines.md#错误保留结构文案在边界渲染)、
[结构化诊断](design-guidelines.md#结构化诊断)和 [doctor](design-guidelines.md#扩展-doctor)，不在本节重复定义协议。
新增或变更能力按实际适用范围验证：

| 场景                   | 必须观察的结果                                                                         |
| ---------------------- | -------------------------------------------------------------------------------------- |
| 配置默认值、覆盖和错误 | 文件、环境与显式 flag 的实际优先级；未知键、类型、范围和组合错误在业务副作用前失败     |
| 取消、超时和部分完成   | 退出状态、错误链、已确认副作用与未知状态准确；恢复建议可操作，取消后仍完成自有资源收尾 |
| 诊断资源故障           | 创建、写入、导出、Sync、关闭失败有界告警；业务成功或失败状态保持，借用资源不被关闭     |
| 输出与敏感内容         | stdout 单结果、stderr JSONL 每行可解析、诊断符合 Schema；日志/Trace 不含虚构敏感标记   |
| doctor 扩展            | 默认不联网、只读、必需/可选失败与父取消区分；实际超时和请求取消可观察，不只断言 mock   |

配置泄漏测试须同时验证可显示文本、双语错误、日志和 Trace 的适用出口，以及原始原因仍可遍历。
部分完成使用临时目录或脱敏资源验证真实副作用，不能仅检查预设字段。第三方隐式输出使用独立子进程捕获真实标准流。
场景不适用时说明原因，不为没有副作用的业务预建恢复框架。

交付按[验证矩阵](#实现与检查)列出实际命令、提交、环境和结果；区分已验证、失败及未运行，保留失败原因。
CLI 协议变更提供真实退出码、样例、Schema 与相对基线的契约差异；模板同步提供准确来源提交、变更范围、兼容影响和人工步骤。
纯文字无需全量 check/race；可执行示例按实际入口验收，运行时变更执行完整检查。平台和发布证据仍见[维护清单](maintenance.md#ci-与验收证据)。

## 测试组织与可靠性

普通测试优先放入所属实现同名的 `*_test.go`；跨文件行为先判断实际归属，合适时归并。
外部测试包使用 `<实现名>_external_test.go`，保留 `package xxx_test`，不为折叠改成内部测试包。
有明确实现归属的集成测试可以使用 `<实现名>_integration_test.go`；普通跨文件测试不因命名改称集成测试。
无需为常量文件创建空测试，也不为文件名对应拆分生产源码。

平台后缀使用 `linux`、`darwin`、`windows`，紧邻 `_test.go`，例如 `foo_external_linux_test.go`；
`macos` 不是 Go 自动识别的 GOOS 后缀。合并前核对职责、包名与有效平台覆盖，可以统一到本项目
支持的三个系统，例如 `process_test.go` 使用 `//go:build linux || darwin || windows`；不机械合并导致覆盖遗漏。

多处使用的 fixture 放在所属包的 `helpers_test.go`，平台专用 helper 使用 `helpers_linux_test.go`、
`helpers_darwin_test.go`、`helpers_windows_test.go`。单处使用的测试类型就近保留；
真实配置加载与注入 stub 的 fixture 各自保持原有边界。
`fuzz_test.go` 维护包级 fuzz 入口和 seeds，`benchmark_test.go` 维护包级 benchmark，普通行为测试按实现归位。

通过真实子进程验证程序契约的集成测试集中在 `test/integration/integration_test.go`，
共享 helper 放在同包的 `helpers_test.go`。集成测试随 `mise exec -- go test ./...` 默认执行，也可用
`mise exec -- go test ./test/integration` 单独运行；目录和文件名是本项目约定，Go 没有专用的集成测试命名规则。

集中消息目录损坏场景由 `test/integration/integration_test.go` 使用 Go overlay 运行 greeting、Session 和
command 三个包的 `TestCorruptCatalogProcess`，worker 分别位于同名的 greeting、session 和 errors 测试文件。
完整测试会执行该集成入口；单独运行所属包时，
这些 worker 会明确跳过。可在隔离应用目录下单独验收：

```sh
test_home=$(mktemp -d)
MY_HOME="$test_home" mise exec -- go test ./test/integration -run '^TestCorruptCatalogEndToEnd$' -count=1
```

进程启动、取消和并发先后使用就绪、释放信号协调，并为等待设置截止时间。资源取得后立即登记清理；
断言中途失败也释放阻塞、取消工作，并等待子进程和 goroutine 收尾。直接子进程由唯一调用方完成 Wait。
心跳检查验证观察期内的写入行为，进程退出和句柄回收使用相应平台的原生测试证据。
时间本身属于被测行为的超时、排空宽限和观察窗口可以保留。

检查成功、失败和取消时同时核对状态、退出码及错误链；根进程成功退出而后代持有管道的场景，
明确验证 `exec.ErrWaitDelay` 的既有边界。事件顺序断言先确认事件存在，无状态场景同时核对预期退出码。

## IDE 与文件折叠

文件折叠只改变项目树的显示，不移动文件、不改变 Go 包边界或测试发现。
同名、平台、外部包及有明确实现归属的集成测试跟随实现；共享辅助文件、包级 fuzz、benchmark
和独立集成测试包的入口折叠在描述本包的 `doc.go` 下。设置保留在个人 IDE 配置中。

```text
cli/
  doc.go
    helpers_test.go
    fuzz_test.go
    benchmark_test.go
  cli.go
    cli_test.go
    cli_external_test.go
```

### VS Code

执行命令面板中的 **Preferences: Open User Settings (JSON)**，将下面的设置合并到已有配置，
保留原有嵌套规则。`expand: false` 让文件组默认折叠：

```json
{
  "explorer.fileNesting.enabled": true,
  "explorer.fileNesting.expand": false,
  "explorer.fileNesting.patterns": {
    "*.go": "${capture}_test.go, ${capture}_linux_test.go, ${capture}_darwin_test.go, ${capture}_windows_test.go, ${capture}_external_test.go, ${capture}_external_linux_test.go, ${capture}_external_darwin_test.go, ${capture}_external_windows_test.go, ${capture}_integration_test.go, ${capture}_integration_linux_test.go, ${capture}_integration_darwin_test.go, ${capture}_integration_windows_test.go",
    "doc.go": "helpers_test.go, helpers_linux_test.go, helpers_darwin_test.go, helpers_windows_test.go, fuzz_test.go, benchmark_test.go, integration_test.go"
  }
}
```

配置方法和变量含义见 [VS Code 文件嵌套说明](https://code.visualstudio.com/docs/editing/getting-started/userinterface#_file-nesting)。
VS Code 在文件组折叠时会将复制、移动或删除操作应用于整组；只操作单个文件前先展开分组。

### GoLand

1. 打开 **Project** 窗口的菜单，选择 **Appearance → File Nesting…**。
2. 勾选 **Show files with the same names as nested**。
3. 在父后缀 `.go` 的子后缀中追加下面的列表，保留已有条目：

```text
_test.go; _linux_test.go; _darwin_test.go; _windows_test.go; _external_test.go; _external_linux_test.go; _external_darwin_test.go; _external_windows_test.go; _integration_test.go; _integration_linux_test.go; _integration_darwin_test.go; _integration_windows_test.go
```

4. 新增一行，父后缀填写完整文件名 `doc.go`，子后缀填写下面的列表，不加 `*`：

```text
helpers_test.go; helpers_linux_test.go; helpers_darwin_test.go; helpers_windows_test.go; fuzz_test.go; benchmark_test.go; integration_test.go
```

GoLand 使用分号分隔子后缀，完整文件名也可以作为后缀匹配；配置后收起 `doc.go` 节点。
它的内置规则按后缀匹配，需要逐项登记，不支持用 `*_test.go` 通配全部测试。
配置入口见 [GoLand 文件嵌套说明](https://www.jetbrains.com/help/go/file-nesting-dialog.html)。

### 新增折叠规则

新增测试先按实际归属选择同名、外部包、集成、平台或共享辅助文件名，优先复用上述通用规则。
必须独立命名的行为测试才向两款 IDE 的清单追加文件名，避免同一测试同时挂在实现文件和 `doc.go` 下。
不为折叠统一添加 `zzz` 前缀、创建占位实现或额外生产文件，也不合并不同测试包。
平台构建约束不同的文件先核对有效覆盖，再决定是否合并。文件调整时核对本文两款 IDE 示例中的例外清单及模板受管文件清单。
这些是供维护者选择的个人设置示例，智能体不读写用户 IDE 配置；本机显示由维护者验收。

## 静态规则与开发探测

`mise run lint` 检查五类文本、双语消息、应用配置、包文档、源码风格和版本声明，再检查 Go 格式并启用以下 34 项，生产导出 API 需中文 GoDoc；测试仅豁免 GoDoc，其他边界仍生效：

```text
errcheck govet ineffassign staticcheck unused bodyclose errorlint gosec godoclint
contextcheck nilnesserr durationcheck nolintlint thelper usetesting goconst depguard forbidigo
asasalint bidichk gocheckcompilerdirectives makezero unconvert predeclared testifylint revive
funcorder gocritic fatcontext loggercheck recvcheck errname modernize containedctx
```

局部 nolint 指明检查器并用中文解释。稳定协议和状态使用语义常量，同值不同义可各自命名。
koanf 只可在精确的 `internal/app/config/load.go` 导入，且仅整体解码；禁用 typed/Must getter、
Get、Raw、All 等绕过结构体的入口。zap 只在 base/logging，OTel SDK/exporter 只在 base/telemetry。
base 不得导入 app、command、tools 或 Cobra；运行时代码及其测试不得导入 tools，开发包内部可复用。
`.golangci.yml` 纳入初始化的身份文件白名单；模块路径变更后继续匹配架构边界和 logging 包装器。

errcheck 同时检查显式丢弃的错误和未检查的类型断言。清理失败应返回、告警或在测试中断言；
已经由资源所有者报告的诊断错误可加精确中文说明，避免重复告警或改变业务退出码。
govet 在默认项外启用 nilness、deepequalerrors、reflectvaluecompare。gocritic 使用默认稳定检查。
loggercheck 检查 Infow、Errorw、Debugw、Warnw 和 With 的成对键值，键必须是字符串。
Context 放在首个参数，测试辅助函数也遵守；thelper 检查 t/b/tb 命名，不强制测试参数居首。
containedctx 约束结构体保存 Context；OTel 固定接口与 Session 生命周期之间的桥接有局部例外。
testifylint 使用默认规则但关闭 float-compare，JSON 整数码与计数的 float64 表示继续精确比较；
modernize 关闭 omitzero，保留 JSON 零值、nil 和空容器的省略行为。

revive 仅启用以下 15 项，indent-error-flow 保留原变量作用域：

```text
blank-imports context-as-argument context-keys-type dot-imports error-return error-strings
errorf import-shadowing increment-decrement indent-error-flow range receiver-naming unexported-return
unused-parameter var-naming
```

import-shadowing 检查局部变量、参数及 range 变量等声明对导入包名的遮蔽，避免路径和命令等名称混淆。

`mise run lint:style` 直接解析所有平台的 Go AST，检查具名、匿名、泛型结构体和外部测试包，
不限制函数参数或返回值的合并声明。扫描跳过 testdata、vendor、隐藏或下划线开头的目录与源码、
根 bin/dist、生成代码和 docs/superpowers 工作记录；拒绝符号链接 Go 源文件。
`mise run lint:test` 在临时模块运行真实类型分析，验证架构、koanf、日志键值、错误处理和 revive 规则的违规与合规 fixture；模板改名后的副本也运行该检查。它测试规则自身，只纳入 check，不作为普通 lint 的依赖。

`mise run lint:i18n` 检查 `internal/locales/active.en.toml` 与 `active.zh-CN.toml`：
目录非空，消息 ID 不重复，字段与消息内容有效，模板语法正确；两种语言的 ID 和模板参数集合一致，
复数形式的参数按并集比较。每条消息必须有 other，允许 description 和 zero、one、two、few、many。
检查器支持直接字段、根别名、括号、`if`/`with`/`range`、被调用的非递归命名模板，以及根对象上
字符串字面量或可静态确定的字面量变量 `index`；管道中的字面量键也可识别。`and`/`or` 只有在各个值
都可确定为根对象时才被视为根别名。根对象的动态 `index` 键、无法确定的字段/链/模板上下文，
控制结构或其分支内的变量赋值，以及嵌套管道中的变量声明或赋值会报错；请改为直接字段或在分支外
确定的变量。子对象上的动态键
不会引入额外的根参数。新增或修改消息时同时维护两种语言，稳定 ID 不随语言改变。

`mise run lint:config` 递归检查 `configs/` 应用配置、`internal/locales/` 语言资源，以及根目录
明确登记的 `config.toml`、`config.example.toml` 和同名 JSON/YAML 变体。`configs/config.example.toml`
必须存在；`configs/` 仅接受 TOML，`internal/locales/` 接受 TOML 和 Go 文件。两处直接子目录
`testdata/` 只放测试 fixture，检查会跳过；其他受管文件必须是普通文件，不接受符号链接。扩展名
不区分大小写。应用 TOML 禁用环境变量覆盖并经 `config.Load` 校验语法、未知键、类型和值域；
语言 TOML 检查语法，消息语义由 lint:i18n 检查。

新增应用配置放入 `configs/` 并通过 `internal/app/config/load.go` 统一加载；确需其他路径时先登记到
`tools/lint/config.go`。mise、`.codex`、CI、golangci 等工具配置沿用工具自身格式，其他位置的
业务 JSON、测试数据和协议/契约数据不纳入扫描。两项检查由 lint 调用并纳入 check；它们不会自动安装
工具或修复文件。

`mise exec -- golangci-lint fmt --color=never` 使用 gofumpt、goimports 和 golines；
加上 `--diff` 只展示差异并失败，不改源码。
check 不改写源码，但会使用 Go 缓存、临时文件和 bin 产物。
CI、缓存及平台验收见[维护清单](maintenance.md#ci-与验收证据)。

## 修改模板与升级工具

初始化只重写 `tools/internal/scaffold/scaffold.go` 的 managedFiles 白名单。
新增身份文件须明确登记；保留初始化实现的原模板身份和 fixture，禁止全仓替换。
初始化不修改 remote、不创建提交或发布，也不复制本地密钥、诊断数据和用户级配置。
移动或新增文件先暂存，再运行使用 Git 索引清单复制项目的 `template:check`。
它验证改名、幂等、测试、真实构建与 go install 后的绝对路径二进制、version/help/schema/TOML。
旧项目迁移与事务恢复见[模板升级](template-upgrades.md)，初始化不承担升级。

升级选择官方最新 LTS 补丁，无 LTS 则最新稳定版，排除预发布；先查 Go、gopls、golangci-lint 兼容性。
同步 mise.toml、Go 声明、doctor 预期、README 受检工具表及 CI 引导，再执行：

```sh
mise run setup
mise run doctor
mise run lint:versions
```

随后按[验证矩阵](#实现与检查)补充对应检查，提交统一准确版本声明；平台支持以原生 CI 和六目标发布构建为证。
gopls 等 Go 后端从固定模块版本在宿主平台编译；非升级任务继续使用已锁定版本。
README 的 tool-versions 标记与工具显示标签由 lint:versions 解析，同时检查 `[tools]` 和 `[vars]`，修改表格时保留它们。
Renovate 由管理员启用，覆盖 mise 工具及带 datasource/depName 注释的 npm/PyPI 版本变量；权限不足时人工更新版本声明并完成检查，不放宽检查。
用户工具通过各自渠道维护，项目 setup 不安装或更新它们。

## 安装发布程序

源码安装入口是 `go install <module>/cmd/<name>@<version>`，安装产物与命令目录同名。
直接解包前先核对同一 Release 的 checksums.txt、SPDX 和来源证明。发布程序本身无需 mise/Go。
应用 HOME 只存运行数据，独立于 go install、解包或 mise 的二进制安装目录。

若用 mise 管理，参照其 [HTTP 后端](https://mise.jdx.dev/dev-tools/backends/http.html)，
把下列占位符替换为选定版本、宿主平台归档固定 URL 和该归档的真实最终 SHA-256，再显式安装。
本地归档可使用绝对 `file:///...` URL；每个平台的摘要分别填写，不要复用：

```toml
[settings]
auto_install = false
exec_auto_install = false

[tools."http:mycli"]
version = "<version>"
url = "<fixed-release-archive-url>"
checksum = "sha256:<archive-sha256>"
```

```sh
mise install http:mycli@<version>
mise which mycli
MYCLI_HOME="$(mktemp -d)" mise exec -- mycli version --json
```

安装 URL 的 version 只是选择标签，不能替代实际二进制 version/commit 验证。
`release:verify` 先核对六归档和 SPDX 最终清单，再在两个独立 mise data/cache 中验证错 SHA 明确失败与
正确 SHA 安装成功，解析绝对安装路径并运行 version/commit/help/schema/TOML。归档必须属于最终清单、
版本一致且 metadata.commit 等于当前 HEAD；发布验收在最终提交后生成 snapshot，后续提交须重建，旧产物会被拒绝。
以上占位安装示例是人工部署流程；自动验收使用实际 URL 和摘要，不会修改全局 mise 配置。

## 发布与交付

交付、署名、PR 和工作区保留遵循[协作约定](../AGENTS.md#隔离开发与交付)，用户明确指定的交付方式优先。
机器接口变更按[兼容性规则](compatibility.md)维护快照和记录，不仅刷新基线。
发布验收、托管权限和证明重试统一见[维护清单](maintenance.md#升级和发布)；合并和发布由维护者决定。

### 数据仓与机器验收

常规测试使用隔离 HOME、伪 gh、内存状态和本地 HTTP 服务；不得读取用户凭据。配置测试覆盖 data 默认值、文件/非空环境优先级、
非法仓名/分支/超时和副作用前失败。生命周期覆盖幂等登记、补绑、退役、重新激活、平台更新、其他身份保护、状态锁和不安全文件。
HTTP 测试覆盖认证/权限、私有仓限制、固定快照、CAS 冲突、超时与取消、提交结果未知和已确认提交后本机失败。
日志、Trace 和 JSONL 同时核对敏感标记保护、run_id、版本事实；诊断创建/写入/导出故障遵循既有降级，不改变业务结果。

模板同步单独记录准确来源与验收提交 U；业务接入对 U 比较契约，整轮再对原始 my 基线比较，不能用模板版本替代应用版本。
真实验收显式选择正式私有数据仓和专用分支，只写合成记录并隔离 MY_HOME；验证实际 CLI、doctor、日志/Trace，
并区分真实 API 行为与注入的 IO/响应丢失故障。成功后删除验收分支，保留正式仓与初始化主分支；证据仅保存脱敏命令、结果、提交和 run_id。
