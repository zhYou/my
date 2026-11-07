# CLI 设计指南

本指南供新增命令、模块和维护工具时使用。先找到使用场景，再沿当前实现扩展。
以下约束描述当前实现；新增功能沿这些边界扩展。库行为以 `go.mod` 锁定版本源码和官方文档为准。

## 先确定边界，再增加目录

一个命令需要 CLI、业务函数和运行资源时，项目选择由 `cmd/<name>` 处理信号与退出，
`internal/app/cli` 处理参数和呈现，应用模块定义自己的选项、结果和错误码，消息集中维护在
`internal/locales/`；`internal/app/session` 组合运行资源。这样业务可直接调用并校验输入，无需导入 Cobra。
公共机制在 `internal/base`，不得反向依赖 app、command、tools 或 Cobra；运行时外部进程在
`internal/command`。开发维护入口在根 `tools`，按环境、构建、lint、契约、模板、MCP 和发布划分领域；实际共享的实现放入
`tools/internal`。运行时代码及其测试不导入开发包，工具可以复用运行时包；Go 的 internal 导入规则约束工具共享包的使用范围。

例如问候逻辑位于 `internal/app/greeting`，结果 schema 的生成机制位于 `internal/base/schema`，
开发期快照比较位于 `tools/internal/contract`。不要把新业务错误码加入 base 的全局目录，
也不要为符合目录图而创建空包。构造函数应完成默认值、校验、依赖组合或资源建立；
简单选项值可以直接使用字面量。嵌套组合较复杂时逐字段分行，便于审查所有权。稳定协议、状态和错误码使用语义常量，同值不同义不强行合并。

[Go 官方布局指南](https://go.dev/doc/modules/layout)说明 internal 等布局方法；项目分层和所有权要求由本指南定义。

## 新命令先校验，再产生副作用

自动化调用缺少参数时应立即失败。项目选择保留非交互运行：参数不足不询问、不自动分页，
不添加进度动画或默认 TUI。这是对 [CLI Guidelines](https://clig.dev/) 的输出、错误、参数和信号建议
结合智能体场景所作的取舍；该指南包含交互建议，并不要求所有 CLI 都使用本项目策略。

新增命令参照 `internal/app/cli/greet.go`：每次调用构造独立命令树，显式注入依赖，使用局部 flag；Args 校验位置参数及
有类型业务选项，必填 flag 同时 `MarkFlagRequired`，RunE 调用业务并在成功后写结果。
根命令额外在配置入口前检查必填和组合 flag。Cobra 自带检查执行顺序不等于这里的副作用约束，
不能仅依赖它在 PreRun 之后的检查。

当前调用的核心形状如下，`ctx`、`localizer` 和校验器由调用入口提供：

```go
opts := greeting.Options{Name: "Codex", Localizer: localizer}
if err := opts.Validate(ctx, validator); err != nil {
    return err
}
result, err := greeting.New(validator).Run(ctx, opts)
if err != nil {
    return err
}
// result.Message 用于文本输出；result 的 JSON 字段为 message。
```

在 CLI 包内，greet 还通过以下真实调用登记必填参数与结果：

```go
greet.Flags().StringVar(&name, "name", "", "问候对象的姓名")
if err := greet.MarkFlagRequired("name"); err != nil {
    panic(err) // 固定命令定义错误，在构造时暴露。
}
if err := registerResult[greeting.Result](&call.results, greet, nil); err != nil {
    panic(err) // 结果结构无法登记，同样属于定义错误。
}
```

业务入口仍调用相同 Validate，避免直接调用绕过 CLI。greet 和 `internal/app/greeting` 可移除，替换时同步注册与测试。
新命令需补双语帮助、示例、参数错误测试、
成功结果和真实退出码测试，并用 `schema <command>` 核对必填标记。结果经 `registerResult[T]`
登记到本次树，由 jsonschema-go 生成 Draft 2020-12；特殊 JSON 编码需显式类型覆盖并验证真实 JSON。
format 不能替代业务语义校验。不要把输出写到一半后再执行可能失败的业务。

库依据：[Cobra v1.10.2](https://pkg.go.dev/github.com/spf13/cobra@v1.10.2)、
[pflag Changed v1.0.9](https://pkg.go.dev/github.com/spf13/pflag@v1.0.9#FlagSet.Changed)、
[jsonschema-go v0.4.3](https://pkg.go.dev/github.com/google/jsonschema-go@v0.4.3/jsonschema)。
Cobra 提供命令与元数据；前置顺序、schema 输出及退出码是项目选择。

## 配置使用严格的模块结构

要增加运行参数时，先在所属模块定义有类型 Config，组合到 `internal/app/config.Config`，
再扩展 defaults、环境映射、TOML 示例和校验测试。不要只加一个字符串 getter 或隐含环境开关。
通用执行器的 Dir、Env、捕获及并行选项保持有类型 API；产品 doctor 的检查预算通过显式 --timeout 传入。

每个实际新增配置项须说明类型、默认值、允许来源、覆盖关系、范围及组合约束；默认值由模块的 Defaults 提供，
经统一加载入口组合。先校验参数和完整配置，再创建资源或执行业务副作用；直接业务调用也校验输入。
数据仓配置为 [data] repository、branch、request_timeout；默认与环境变量见 README。
my 配置、Session 与业务状态默认共用 HOME 布局，MY_HOME 统一覆盖，--config 不改变状态根。

凭据由具体业务声明来源，优先通过调用级依赖、注入的 LookupEnv 或受控凭据提供者取得；不预建凭据配置框架。
需要文件、环境或 flag 输入时，明确读取时机和优先级，不用日志证明凭据存在，不把凭据写入配置示例。
配置及错误参数只保留审查过的字段名、类型、位置等安全信息；禁止整份配置、凭据值、认证头和带凭据 URL 进入诊断。
解析器的错误也可能包含输入值，不能因其来自第三方就直接显示；敏感原因保护见下一节的错误规则。

项目只读取一个 TOML，顺序为显式 flag > 非空环境变量 > 文件 > 默认值。`Changed` 表示用户
明确给过 flag，即使值恰好等于默认值也生效；未设置的 flag 不能覆盖文件。当前级别覆盖写法：

```go
var override *string
if cmd.Flags().Changed("log-level") {
    override = &logLevel
}
cfg, err := config.Load(cmd.Context(), config.Options{
    AppName: "my", Path: configPath,
    Explicit: cmd.Flags().Changed("config"), LogLevel: override,
    LookupEnv: lookupEnv, Validator: validator,
})
```

加载器先严格解码 TOML 原始嵌套 map，拒绝 `"logging.level"` 这种字面键伪装嵌套字段；
随后 koanf 合并并整体解码。合法 TOML 点号语法 `logging.level = "info"` 会被解析成嵌套结构。
mapstructure 使用 `ErrorUnused=true`、`WeaklyTypedInput=false`、精确名称及受控 duration hook，
最后 validator 校验范围。不要把 koanf 的 StrictMerge 当作未知键或值域校验，也不要把 TOML 的
数字字符串自动转成数字。koanf 只可在精确的 `internal/app/config/load.go` 导入，业务只接收结构体。

例如 `[telemetry] batch_size = 4096` 在默认 queue_size=2048 下应失败；
`MY_TELEMETRY_BATCH_INTERVAL=1s` 是显式的 duration 文本转换；
兼容 `MY_LOG_LEVEL` 在新 `MY_LOGGING_LEVEL` 之后应用，flag 最后应用。
空环境变量视为未设置。非法显式文件立即报错，默认文件缺失可继续；不自动迁移 YAML。
自行维护的应用配置使用 TOML，新增配置放在 `configs/` 并经统一入口加载。其他应用配置路径
须先登记到 `tools/lint/config.go`；工具配置以及 JSON 业务、测试和契约数据可保持原格式。
检查范围和例外见[开发说明](development.md#静态规则与开发探测)。

validator 实例可复用；项目在使用前完成自定义规则注册，之后只校验。
不要在并发调用期间变更规则。库依据：
[koanf v2.4.1](https://pkg.go.dev/github.com/knadh/koanf/v2@v2.4.1)、
[TOML v2.4.3](https://github.com/pelletier/go-toml/tree/v2.4.3)、
[mapstructure v2.4.0](https://pkg.go.dev/github.com/go-viper/mapstructure/v2@v2.4.0#DecoderConfig)、
[validator v10.30.5](https://pkg.go.dev/github.com/go-playground/validator/v10@v10.30.5)。

## 错误保留结构，文案在边界渲染

业务失败需要同时给自动化和用户诊断时，在所属模块定义稳定 fault.Code、Kind 及参数；
使用 Wrap 保留系统原因。项目不依赖错误字符串判断取消、文件缺失或子进程退出。
新增能力还须用 fault.WithDiagnostic 补充 Diagnostic：Phase 是所属模块的稳定阶段标识，
CompletedEffects 仅列已确认的脱敏副作用标识，Recovery 保存双语消息 ID 和安全参数。
nil 副作用列表表示未知，空列表表示确认没有副作用；非空列表不保证穷尽。
副作用未知时，应指导先核实业务状态，不能建议直接重试非幂等操作。
WithDiagnostic 快照切片和参数映射；嵌套参数须不可变。DiagnosticOf 读取最近的完整诊断，
聚合错误按 Details 的分支分别呈现，不合并或猜测副作用。边界使用 WithDetails 补齐逐分支呈现时仍保留原始错误链，不修改借用错误或用外层默认值覆盖已知事实。恢复说明按业务编写，不执行自动恢复。

含敏感输入的原始原因使用 fault.WithSensitiveCause；Error.Error、Localizer.Render、日志与 Trace
均隐藏该原因，errors.Is/As 仍可遍历。该选项不扫描或脱敏 Params、Message、Recovery 参数及任意业务日志，
这些内容须由产生方只放安全数据。配置 parse/decode/environment 错误已使用此机制，避免回显无效值。

以下片段展示实际 fault API 的用法，`readErr` 是调用者取得的错误：

```go
problem := fault.Definition{Code: "example.read", Kind: fault.Runtime}
err := problem.Wrap(readErr, fault.WithParams(fault.Params{"Path": file}))
// errors.Is(err, os.ErrNotExist) 与 errors.As(err, &exitError) 仍能遍历原始原因。
```

各模块的消息集中写入 `internal/locales/active.en.toml` 和 `active.zh-CN.toml`，每种语言包含
完整消息 ID；新增或修改时双语同次维护。使用带引号的完整 ID 作为 TOML 表名，other 必需，
可按语言添加 zero、one、two、few、many 复数形式及 description。例如：

```toml
# active.en.toml
["greeting.message"]
other = "Hello, {{.Name}}!"
```

入口通过 `locales.LoadBundle()` 取得缓存的只读 Bundle 或加载错误，每次调用建立独立 Localizer。
成功结果与帮助使用严格 Format，并检查错误；需要复数时在 `FormatOptions.PluralCount` 传入计数，
模板可用 `{{.PluralCount}}` 访问该值：

```go
bundle, err := locales.LoadBundle()
if err != nil {
    return err
}
localizer := bundle.Localizer("en")
text, err := localizer.Format("greeting.message", messages.FormatOptions{
    Params: map[string]any{"Name": "Codex"},
})
if err != nil {
    return err
}
```

加载失败、缺译、模板错误或缺少参数会返回错误；Format 失败时返回空字符串，不使用部分渲染文本。
Text/Render 仅用于安全诊断兜底，不能凭非空字符串判断渲染成功；诊断故障不吞掉原始业务错误。
`mise run lint:i18n` 检查双语目录、模板和参数，细则见[开发说明](development.md#静态规则与开发探测)。
项目暂不支持运行时翻译覆盖或 `[i18n]` 配置。CLI 按 `--lang` > `<PREFIX>_LANG` > en 选语言，
zh-CN 是中文、zh 是别名，不从 OS 区域推断。稳定命令、选项、JSON 字段、错误码和状态枚举不翻译；
第三方错误叶子在经过敏感边界审查后保留原文。Help、version、completion、schema 在坏配置下仍可运行，不创建诊断目录。

退出码为成功 0、运行/配置 1、调用无效 2、调用者取消 130。stdout 只提交成功结果；doctor 的已完成失败报告例外，见“扩展 doctor”。
`--json` 的成功 stdout 为一个文档，序列化失败保持空输出，底层写失败可能留下部分字节。消费方先检查退出码。
错误与告警协议见[结构化诊断](#结构化诊断)；doctor 的完成负向报告不额外产生错误记录。
默认文本；帮助为文本、补全为原生脚本、schema 始终为 JSON。错误和告警写 stderr，日志使用独立出口。
只有 `cmd/<name>` 处理进程退出，业务和日志返回错误。库依据：
[Go errors](https://pkg.go.dev/errors@go1.27.3)、
[go-i18n v2.7.0](https://pkg.go.dev/github.com/nicksnyder/go-i18n/v2@v2.7.0/i18n)。
错误分类、目录组织和语言优先级是项目契约；go-i18n 的 Bundle 在 Localizer 读取时不能继续修改。

## 结构化诊断

显式启用 --json 时，stderr 使用 JSONL：错误和告警各占一行，现有日志仍使用其 JSONL 结构。
诊断以 type=error 或 warning 区分，日志使用 event 等既有字段，不封装借用 logger。
每条诊断包含 schema_version=1 和非空 diagnostics 数组；仅错误包含 exit_code，存在 Session 时包含 run_id。
每项包含 code、kind、message、phase、completed_effects，以及可选 recovery（code 和 message）。
说明与恢复文案本地化，机器标识不翻译。不向 stderr 序列化原始 fault.Params 或错误链对象。

一次失败最多产生一条最终 error，先收尾 Session，再提交错误；warning 不改变业务结果。
参数或配置错误不为取得 run_id 创建诊断目录。未知领域码按调用边界提供 cli.usage/runtime/cancelled/deadline 等兜底码。
JSON 模式按命令树元数据预选，最后一个合法 --json 或 --json=bool 生效；跳过选项值、尊重 --，
非法布尔值仍由正式解析报错。预选不修改 Changed、不执行 hook 或读取配置。
消息目录初始化失败时仍可给出安全英文诊断。诊断渲染或写入失败不覆盖业务退出码，不递归告警。

schema [命令路径] 的 diagnostic_schema 给出错误与告警的 Draft 2020-12 结构；
它不把普通日志声明为诊断记录。消费方逐行解析 stderr，按 type 识别诊断，不能将整个 stderr 当作一个 JSON 文档。
文本模式在错误说明后补充阶段、已确认副作用和可用恢复建议。

## 日志、Trace 和异步工作的所有权

一次命令可能有业务 span、工具 span 和诊断文件。项目只在参数、配置校验后创建 Session，
以 run_id 串联命令生命周期。日志默认 info/file，写 `<state>/logs/<run_id>/events.jsonl`，
也可选 stderr/none；Trace 默认 none，file 独立写 `trace.jsonl`。file 是本地诊断 JSON 流，
不承诺 OTLP、采集服务接入或重放；使用私有 provider，不替换全局 provider，也不通过环境隐式启用 exporter。
SDK 环境仍有版本相关行为，不能将此概括为“忽略所有 OTEL_*”。

启用的诊断出口按日志级别记录 command_started、command_finished、实际命令、耗时、退出状态及 run_id；
file Trace 的 service.instance.id 对应相同 run_id。发现命令和校验前失败不强制创建 Session。
关键业务阶段明确稳定名称、结果和耗时；成功阶段用 info，失败用 error，不仅依赖 debug。
在已创建的调用中增加业务 span，参照 greet：

```go
started := time.Now()
ctx, span := call.session.Tracer.Start(cmd.Context(), "greeting.run")
defer span.End()
result, err := call.deps.Greet(ctx, greeting.Options{Name: name, Localizer: call.localizer})
if err != nil {
    telemetry.RecordError(span, err)
    span.SetStatus(codes.Error, call.localizer.Text("session.failed", nil))
    return err
}
call.logger.WithContext(ctx).Record(greetingFinished{
    message: call.localizer.Text("cli.greet_log", nil),
    duration: time.Since(started), failed: false,
})
```

`WithContext` 只绑定 span 关联字段，logger 仍显式传递。重要结束事件使用
`Logger.Record(logging.CommandResult{...})` 或 `logging.ToolCall`，普通临时诊断才用 Debugw/Infow。
工具事件只记录操作、程序名、结果、耗时与结构化错误，不记录 argv、环境和原始输出。
新领域事件由所属模块实现 `logging.Event` 的 Emit 方法，显式使用调用级 logger 和 tracer；
telemetry.RecordError 逐分支记录安全错误文本、error.code、error.phase、已知副作用和恢复标识。
命令结束日志保留既有错误字段，并增加 error_details 数组。日志门面没有 Fatal/Panic，业务不得绕过入口退出进程。

并发工作通过 `Session.Go(context.Context, func(context.Context)) bool` 登记，工作接收本次显式传入的 Context；Finish 开始后拒绝新任务。
工作须尊重 Context 并自行结束。Finish 幂等等待已登记工作，记录结束事件，结束根 span，
用 `context.WithoutCancel` 加独立 shutdown_timeout 排空 Trace，再 Sync 自有 logger、关闭自有文件。
不能在业务取消后直接复用已取消 ctx 进行导出，也不能丢下工作后提前关闭 writer。
借用 logger、reader、writer 不 Sync/Close；借用且共享的输出需由调用者提供并发安全包装。
CLI 自有 stderr 路径串行化错误、日志和告警，但不能替嵌入方接管独立的外部写入。
任意永久阻塞的自定义 IO 不保证硬截止，工作不结束也会阻止 Finish 返回。

第三方组件可能绕过调用级出口写进程标准流或全局 handler。接入时须验证默认、异常环境、取消和失败路径，
优先使用显式 writer 与私有实例，无法安全构造时降级到项目告警；不通过改进程环境或全局 handler 隔离并发调用。
现有 OTel 环境预检、资源字段过滤及 exporter 告警沿用，异常环境验证应捕获真实子进程 stdout/stderr。

资源创建、写入、导出或收尾故障按类别告警并继续业务，不覆盖业务结果；无可用出口时降级到空日志/Trace。
文件轮转软阈值默认 10 MiB，单条超大记录不拆分，默认 5 备份；按 14 天、200 MiB 软总量清理。后两项与备份数可设 0 关闭。
清理要求元数据、目录身份和普通文件清单有效，并成功取得运行系统锁；PID 或锁文件存在不证明活跃。
未知文件、符号链接、活跃运行保留，不递归删除目录；标记在数据删除后清除，中断后仍可识别。

库依据：[zap v1.28.0](https://pkg.go.dev/go.uber.org/zap@v1.28.0)、
[OTel SDK v1.47.0](https://pkg.go.dev/go.opentelemetry.io/otel/sdk@v1.47.0/trace)、
[stdouttrace WithWriter](https://pkg.go.dev/go.opentelemetry.io/otel/exporters/stdout/stdouttrace@v1.47.0#WithWriter)、
[context](https://pkg.go.dev/context@go1.27.3)、[flock v0.13.1](https://pkg.go.dev/github.com/gofrs/flock@v0.13.1)。
库提供记录、导出、锁与取消机制；上述生命周期、失败继续策略和删除白名单是项目选择。

## 调外部工具时保留 argv、错误和取消原因

需要 Git 等外部程序时，使用 command.Spec 构建独立 argv，禁止拼 shell 字符串。
通过 Execution 显式传递 Dir、Env、IO 和调用级 Localizer、Logger、Tracer。
工具层返回错误，不直接打印；入口统一本地化错误，并在成功时提交结果。
空参数、空格和 Unicode 必须保留。以下是可放入仓库内部包的实际 API 示例，省略 import：

```go
type revisionInput struct{ Revision string }
spec := command.Spec[revisionInput]{
    Operation: "git.revision", Program: "git",
    Args: []command.Arg[revisionInput]{
        command.Literal[revisionInput]("rev-parse", "--verify"),
        command.Value(func(p revisionInput) string { return p.Revision }),
    },
}
invocation, err := spec.Build(revisionInput{Revision: "HEAD"})
if err != nil {
    return err
}
execution := command.Execution{
    Dir: repoDir, Localizer: localizer, Logger: logger, Tracer: tracer,
}
result := command.Capture(ctx, invocation, execution, command.CaptureOptions{Limit: 2048})
if result.Err != nil {
    return result.Err
}
// 检查 result.StdoutTruncated，再使用原始 result.Stdout；这里不向 CLI stdout 写入。
```

Env=nil 继承父环境，空切片清空；WithEnv 复制并追加覆盖，不修改父选项。有效 PATH/PATHEXT 来自
本次环境，拒绝隐含当前目录命中（exec.ErrDot）。Run 流式输出借用 IO；Capture 只保留输入并独立捕获输出，
默认每流 1 MiB，达到上限继续排空，返回截断标记。错误保留 exec.ExitError、context 错误和 Cause。

批量使用 `command.RunBatch(ctx, calls, execution, command.BatchOptions{Concurrency: 4, ItemTimeout: 3*time.Second})`。
默认 concurrency=1、收集全部，按输入顺序返回；ItemTimeout 从取得名额后开始，零值不加超时。
FailFast=true 固定首先触发终止的原因并取消其他工作，收尾期间稍后的父取消不覆盖先前业务失败；
collect-all 遇到父取消仍中止批量。自身 deadline 记 Canceled+Err，尚未开始就因批量取消的项记 Skipped。
并发共用输入时调用方提供并发安全 reader，输出按项隔离。

Linux/macOS 的自有进程组清理普通后代并 Wait 直接子进程；未收养僵尸由 OS 回收。
setsid/detach 或交给外部服务的进程不在保证内。Windows 10/Server 2016+ 在创建时绑定私有 Job、
限制继承句柄，以挂起主线程准备监控后恢复；不支持嵌套 Job 时启动前失败。
Windows 终止失败先关闭 Job 再等待根进程，返回前验证活动进程清空，异常与超时保留错误。
WaitDelay 默认 250ms，控制取消终止及根进程退出后的管道排空宽限。Linux/macOS 在 SIGTERM 宽限耗尽后发送
SIGKILL，并额外保留一次 WaitDelay 观察进程组退出；确认组不存在时立即结束，未被收养的僵尸仍由 OS 回收。
永久阻塞的借用 IO 不保证被打断。

库依据：[os/exec](https://pkg.go.dev/os/exec@go1.27.3)、
[errgroup v0.24.0](https://pkg.go.dev/golang.org/x/sync@v0.24.0/errgroup)、
[Windows Job 创建属性](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-updateprocthreadattribute)。
CommandContext 默认取消单个进程，并不承诺清理整棵树；errgroup.WithContext 的首错取消也不是 collect-all。
不要以库名或交叉编译成功替代上述平台原生 helper 验收。

## 让证据匹配交付声明

测试观察真实结果：临时 Git 仓库、真实子进程、文件锁竞争、可控故障和取消。
配置存在、进程启动或 mock 成功不足以证明安装、清理或资源回收成功。
Serena 与 Go CLI 补充、应用 HOME 隔离和验证选择统一见[开发说明](development.md#实现与检查)；
平台、发布及托管证据见[维护清单](maintenance.md#ci-与验收证据)。

## 扩展 doctor

`internal/app/doctor.Run` 执行数据仓与本机绑定检查，CLI 通过 `Dependencies.Doctor` 保留受控注入边界。
添加检查时维护有类型的 Options 和 Check，校验在副作用前完成；不要把开发工具清单作为产品依赖。
doctor 自行严格加载配置，将错误归入报告；不创建 Session 诊断资源。

检查须只读：不安装依赖、不修复状态、不登录或刷新凭据、不修改业务配置和数据。
默认离线；联网由 --remote 显式选择，说明访问目标、请求内容、是否使用凭据及关闭方式，不能由隐式调试环境触发。
每项明确必需或可选、独立超时、输出捕获和并发上限；外部程序复用 command，网络调用传递检查 Context。
检查超时或依赖不可用形成 status=failed 的完成报告，继续收集其余检查；父调用取消或无法形成报告则返回 error。
不要把部分检查误报成完整成功，也不要以检查超时替代整个调用的取消语义。
Detail 只给安全解释和可执行建议，不输出响应体、原始 stderr 或凭据。
接入示例见 [doctor 脱敏夹具](../internal/app/cli/doctor_test.go)：使用 Dependencies.Doctor、只读临时文件和本地 HTTP 服务，
演示联网选择、必需/可选超时及取消。实际业务检查使用本地路径、gh、认证与固定 HEAD 快照，不增加通用调度器。

Result 的 checks 始终编码为数组。RequiredAvailable 表示所有必需检查均可用；可选失败不能将它改为 false。
检查项的 name、status、required 是机器接口，detail 提供当前语言说明。已完成的负向报告返回 Result、nil，
将 RequiredAvailable 置 false；CLI 完整渲染后输出并返回 1，不附加重复错误。
未能完成检查则返回 error，CLI 保持 stdout 为空并按已有规则报告；取消返回 130。

## 补全协议

帮助、示例和 shell 子命令沿用具体命令对象绑定集中目录。补全请求仅消费有效的完整语言参数，
未完成参数使用有效环境或 en 兜底；普通调用仍严格校验。补全不读取应用配置、不创建诊断资源。

补全在独立命令树预解析已完成参数，避免第三方解析错误绕过调用级 stderr；无法解析时返回
标准错误且禁止文件回退的 directive `:5`，退出 0，不输出诊断文本。预解析不改变实际候选树的值或 Changed 状态，
不替换全局 IO。取消仍返回 130。保留 `completion -- <shell>` 的历史位置参数调用。

## 数据仓业务接入

业务输入和完整配置先校验；凭据仅通过 command 层调用 gh auth token 捕获，不回显，不刷新。
go-gh 的 Host、APIHost、AuthToken、Transport、Timeout 显式设置，LogIgnoreEnv=true、EnableCache=false，禁止重定向携带凭据。
读取只消费格式标记与机器文件，严格 TOML、固定 HEAD、限长响应；不得把响应体或备注放入诊断字段。

业务 span 为 data.init、machine.register、machine.bind、machine.retire；子阶段为 data.read、data.commit、machine.binding.write/clear。
所属模块事件记录 phase、outcome、duration、error_code 和必要版本；data.commit 区分 base_commit 与确认成功的 commit。
Session 负责 run_id 与日志、Trace 出口和降级；业务复用 telemetry.RecordError，禁止直接记录原始 error 字符串。
stdout 输出成功结果；stderr 在 --json 下逐行区分 error/warning，普通日志保留 event。

失败通过 Diagnostic 表达 phase、completed_effects 与 Recovery。远端提交未确认时保留 nil，禁止推断无副作用或自动重发；
确认成功后本机失败列 data.commit，并给出提交版本与 bind 恢复步骤。并发 HEAD 变化先重新读取和确认资料，禁止覆盖提交。
配置读取拒绝FIFO、设备等非普通目标，保留指向普通文件的配置符号链接，不用阻塞后台读任务实现限时。
本机状态锁覆盖预检查到最后落盘；临界区重读绑定，仓名大小写不敏感，其余身份字段严格比较；不替换其他机器、仓数字 ID 或分支。状态目录0700、文件0600，拒绝符号链接。
父目录允许0755但拒绝组或其他用户写入。Windows 使用同样的类型校验，不将 Unix mode 位当作 ACL 证明。

产品 doctor 串行，每项默认10s：paths、config、gh、auth.local、binding.local 为本地必需项；
--remote 追加 data.repository、data.branch、data.format，已绑定时 binding.remote 必需，未绑定时为可选 skipped。
前置失败导致 required skipped，整体失败；每项超时形成 failed，父取消终止报告。只读检查不证明写权限。
