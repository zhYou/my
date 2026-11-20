# Issue tracker：GitHub

本仓库的 issue、规格和门票保存在 GitHub Issues 中，统一使用 `gh` 操作。

## 约定

- **创建 issue**：`gh issue create --title "..." --body "..."`。多行正文使用 heredoc。
- **读取 issue**：`gh issue view <number> --json number,title,body,labels,comments`。
- **列出 issue**：使用 `gh issue list --state open`，按需添加 `--label`、`--state` 和 `--json` 过滤。
- **建立子 issue**：使用 `gh issue create --parent <parent> ...`，或执行
  `gh issue edit <parent> --add-sub-issue <child>`。旧版 `gh` 可调用 GitHub sub-issues API；
  不支持子 issue 时，在正文首行写 `Part of #<parent>`。
- **声明阻塞关系**：优先使用 GitHub 原生 issue dependencies：
  `gh issue create --blocked-by <n>,<n>` 或 `gh issue edit <child> --add-blocked-by <blocker>`。
  旧版 `gh` 可调用 blocked-by API；平台不支持 dependencies 时，在子 issue 正文首行写
  `Blocked by: #<n>, #<n>`。
- **检查 open blocker 数量**：
  `gh api repos/<owner>/<repo>/issues/<n> --jq .issue_dependencies_summary.blocked_by`。
  返回 `0` 表示可以开始。`gh issue view <n> --json blockedBy` 包含已关闭的 blocker，须筛选
  `state == "OPEN"`。
- **评论**：`gh issue comment <number> --body "..."`。
- **添加或移除标签**：`gh issue edit <number> --add-label "..."` 或 `--remove-label "..."`。
- **关闭**：先评论说明结果，再执行 `gh issue close <number>`。对已关闭 issue，`gh issue close --comment`
  会丢弃评论但仍返回成功，因此必须拆成两次操作。

在克隆内运行 `gh` 时会自动推断仓库。创建 issue 前必须执行
`gh repo view --json nameWithOwner`，确认目标属于当前用户范围；本仓库为
`zhYou/my`。探索想法按 `docs/backlog.md` 维护，只有维护者明确要求记录时才新增待办条目。
不向 `mattpocock/skills` 提交 issue，除非用户明确指定且拥有对应仓库。

## PR 作为 triage 入口

**外部 PR 作为请求入口：否。**

本仓库不把外部 PR 当作功能请求进入 triage 队列。需要改变时应直接打开 issue。

## 技能操作映射

- “发布到 issue tracker”：创建 GitHub issue。
- “获取相关 ticket”：按上述“读取 issue”方式读取完整正文和评论。
- 不修改或关闭原始父 issue，除非用户明确要求。

## Wayfinder 操作

Wayfinder 的 map 是一个带 `wayfinder:map` 标签的父 issue。

- **创建 map**：`gh issue create --label wayfinder:map`。
- **创建子 ticket**：关联为 map 的子 issue；标签使用
  `wayfinder:research`、`wayfinder:prototype`、`wayfinder:grilling` 或 `wayfinder:task`。
  不支持子 issue 时，将子 ticket 加入 map 正文的任务列表，并在正文首行写 `Part of #<map>`。
- **阻塞关系**：使用前述原生 issue dependencies。
- **选择 frontier**：列出 map 的 open 子 issue，排除仍有 open blocker 或已有 assignee 的项，
  按 map 顺序选择第一项。
- **认领**：`gh issue edit <n> --add-assignee @me`，这是该会话的第一次写操作。
- **完成**：评论答案、关闭 ticket，再把结论摘要和链接追加到 map 的 Decisions-so-far。
