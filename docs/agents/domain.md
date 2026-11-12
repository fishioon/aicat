# Domain docs

本仓库采用 single-context 领域文档布局。

## 探索代码前读取

开始涉及领域行为、架构或业务边界的工作前，按需读取：

- 根目录 `CONTEXT.md`：aicat 的稳定领域语言；
- [架构](../architecture.md)：模块边界、数据流与约束；
- `docs/adr/`（如存在）：与当前工作相关的架构决策。

唯一配置、进程边界、网关与 addon 职责、外部 runtime 和私有 fork 的限制均在架构文档中维护，实施时须遵守。文档只描述当前状态，不记录变更过程；过程留在 git 历史和 Issue 里。

领域文档不存在时直接继续，不把缺失本身当作错误，也不预先创建空文件。在第一个术语或架构决策真正明确时，按需创建；有 `domain-modeling` skill 时使用它，不可用时按本文约定处理。

## 文件布局

```text
/
├── CONTEXT.md
└── docs/
    ├── architecture.md
    └── adr/            # 按需创建
        └── 0001-*.md
```

内置网关是 aicat 自己的领域：它由仓库内的 `internal/gateway/` 与
`internal/gateway/addons/` 承载，当前业务概念（Gateway Addon、traffic 等）进入 aicat 的
`CONTEXT.md`。`go-mitmproxy` 只是被复用的库，不构成第二个领域上下文，也不把它自身的文档复制到根目录——只有与 aicat 的接缝有关的部分（入口形态、拦截白名单、CA）才记在 aicat 的文档里。

## 使用 glossary 的词汇

Issue 标题、设计方案、测试名称和代码评审在引用领域概念时，应使用 `CONTEXT.md` 定义的 canonical term。

如果 glossary 明确列出了应避免的同义词，不应在后续产物中继续使用这些同义词。

如果需要的概念尚未出现在 glossary 中，应判断：

- 这只是普通工程术语，不需要进入领域模型；或者
- 这是 aicat 的真实领域概念，需要与用户确认后补充；有 `domain-modeling` skill 时使用它。

## ADR 冲突

如果新方案与某条 ADR 冲突，应明确指出冲突并讨论是否重新打开该决策，不能静默覆盖。决策被取代时直接更新或删除对应 ADR，不保留已失效的条目。架构文档与领域文档之间若存在无法解释的冲突，应先对齐再修改。

只有同时满足以下条件的决策才创建 ADR：

1. 很难撤销；
2. 缺少背景时会让未来维护者感到意外；
3. 确实经过了有意义的方案权衡。
