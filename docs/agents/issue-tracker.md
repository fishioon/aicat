# Issue tracker：GitHub

本仓库的 Issues、PRD 和 Wayfinder 地图存放在 GitHub Issues。执行 tracker 操作时，在仓库目录内使用 `gh` CLI，并通过当前仓库的 `origin` 自动确定目标仓库。

需求 triage 使用 [Triage labels](triage-labels.md) 定义的五类标签，不创建同义标签。外部 PR 不作为 triage 需求入口。

## 基本操作

- 创建 Issue：`gh issue create --title "..." --body "..."`
- 阅读 Issue：`gh issue view <number> --comments`
- 列出 Issues：使用 `gh issue list --state open --json ...`，并根据任务添加 label、state 等过滤条件
- 添加评论：`gh issue comment <number> --body "..."`
- 添加或移除标签：`gh issue edit <number> --add-label "..."` / `--remove-label "..."`
- 关闭 Issue：`gh issue close <number> --comment "..."`

多行正文应通过安全的临时文件或 heredoc 传递，避免 shell quoting 改变 Markdown 内容。

## Pull requests 不是 triage 需求入口

外部 PR 不进入与 Issues 相同的 triage 队列。PR 继续按普通代码评审流程处理，不应用需求 Issue 的状态机。

GitHub Issues 和 PR 共享编号空间。遇到只有编号、类型不明确的引用时，先判断它是 Issue 还是 PR，不要直接假设。

## 当 skill 要求“发布到 issue tracker”

创建一个 GitHub Issue。

## 当 skill 要求“读取相关 ticket”

使用 `gh issue view <number> --comments` 读取正文、标签和评论。

## Wayfinding operations

Wayfinder 使用一个 GitHub Issue 作为 map，并使用它的 child issues 作为调查 tickets。

### Map

- Map 是一个带有 `wayfinder:map` 标签的 Issue。
- Map 正文只保存 Destination、Notes、Decisions so far、Not yet specified 和 Out of scope。
- Map 是低分辨率索引，不复制各 ticket 的完整结论。

### Child ticket

- 每个 ticket 创建为独立 Issue，并作为 GitHub sub-issue 关联到 map。
- ticket 使用以下标签之一：
  - `wayfinder:research`
  - `wayfinder:prototype`
  - `wayfinder:grilling`
  - `wayfinder:task`
- 如果仓库未启用 sub-issues，则回退为：
  - 在 map 中维护 task list；
  - 在 ticket 正文顶部添加 `Part of #<map>`。
- ticket 被某个 session 领取时，首先将其分配给当前推进者；未分配的开放 ticket 才视为未领取。

### Blocking

优先使用 GitHub 原生 issue dependencies：

```sh
gh api \
  --method POST \
  repos/<owner>/<repo>/issues/<child>/dependencies/blocked_by \
  -F issue_id=<blocker-database-id>
```

这里的 `issue_id` 必须是 blocker 的 numeric database id，可通过以下命令获得：

```sh
gh api repos/<owner>/<repo>/issues/<number> --jq .id
```

它不是 Issue 编号，也不是 GraphQL `node_id`。

如果仓库不支持原生 dependencies，则在被阻塞 ticket 正文顶部使用：

```text
Blocked by: #<number>, #<number>
```

ticket 的所有 blocker 关闭后，它才算 unblocked。

### Frontier

Frontier 是 map 下同时满足以下条件的开放 tickets：

1. 所有 blocker 已关闭；
2. 当前没有 assignee；
3. 仍属于当前 map；
4. 没有被判定为 Out of scope。

当没有指定 ticket 时，Wayfinder 按 map 顺序领取 frontier 中第一个 ticket。

### Claim

开始处理 ticket 前的第一次写操作必须是：

```sh
gh issue edit <number> --add-assignee @me
```

assignee 就是并发领取锁。

### Resolve

解决一个 ticket 时依次执行：

1. 在 ticket 下发表 resolution comment；
2. 关闭 ticket；
3. 在 map 的 Decisions so far 中追加一条“ticket 名称链接 + 一句话结论”；
4. 根据新获得的信息创建后续 tickets、补充 blocking edges，或将已清晰的问题从 Not yet specified 提升为 ticket。

每个 Wayfinder session 最多解决一个 ticket。
