# 机器接口兼容性

`schema_version` 保持 1。产品 CLI 的命令、选项、JSON 字段、枚举和退出码是机器接口；
所选语言的帮助与说明可调整，字段不翻译。显式 --json 的 stderr JSONL 诊断及其 diagnostic_schema 也属于机器接口。消费者应忽略新增字段，结果允许额外属性，配置未知键仍失败。
删除、改名、改变字段类型、增加必填要求或改变退出码视为潜在破坏变更。
废弃产品 CLI 命令时先保留旧入口，说明迁移和计划移除版本，再在适当版本移除。

## 变更流程

1. 修改接口后运行 `mise run contract:update`，审查 `test/testdata/contracts/v1.json` 差异。
2. 在 `docs/compatibility/changes/` 新增 JSON 记录，包含 `id`、`compatibility`、`summary`、`paths`、`previous_version`、`target_version`、`migration`。
3. `compatibility` 使用 `compatible`、`breaking` 或 `review-required`；复杂 Schema 变化须人工审查，破坏变化不能声明为兼容。0.x 破坏变更提升次版本，1.x 以后提升主版本；首次基线的 previous_version 为 null。
4. 运行 `mise run contract:check`。CI 另用 `--against` 比较 PR base 或 push before，只接受相对基准新增或修改的记录；刷新快照不会跳过该比较。

检查只给出结构差异，不能证明任意 JSON Schema 的语义兼容。默认配置、语言、日志和路径变化即使未进入快照，也须评估使用影响并提供迁移说明。

## 记录与迁移

| 记录                                                                            | 涉及内容                                            |
| ------------------------------------------------------------------------------- | --------------------------------------------------- |
| [CLI 基础](compatibility/changes/2026-10-30-cli-foundations.json)               | lang、info 默认值及应用 v0.1.0→v0.2.0 决策          |
| [消息与默认语言](compatibility/changes/2026-11-02-i18n-config.json)             | 默认语言由 zh-CN 改为 en                            |
| [发现能力与 doctor](compatibility/changes/2026-11-02-cli-discovery-doctor.json) | doctor 报告、completion shell 子命令、帮助与 schema |

[0.4.0 诊断契约记录](compatibility/changes/2026-11-07-my-diagnostic-contract.json)声明 stderr 文本转 JSONL 的破坏变更；schema_version 仍为 1。

迁移步骤和旧初始化事务恢复见[模板升级](template-upgrades.md)。模板版本独立于应用发布版本，兼容记录不代表已创建标签或发布；
派生项目的检查依赖已提交基线与记录，不推测不存在的版本或历史快照。

[HOME、机器和 doctor 记录](compatibility/changes/2026-11-07-my-home-machine-doctor.json)记录本轮业务的破坏影响：
默认根改为 `~/.my`（Windows 为 USERPROFILE 下 `.my`），`MY_HOME` 仍优先且须为绝对路径；`--config` 只改配置文件。
已有系统目录不会自动读取或迁移，可先指定明确的 MY_HOME，再迁移所属配置、data、state、cache；不复制无关个人诊断数据。
doctor 从空报告改为实际只读检查，未就绪返回完整报告与退出1；--remote 才联网，--timeout 默认10s每项。
模板升级和业务接入共用尚未发布的应用 v0.4.0 目标，不把两个阶段写成 v0.4.0→v0.4.0，也不据此创建发布标签。
