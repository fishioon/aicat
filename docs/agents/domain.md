# Domain docs

本仓库采用 single-context 领域文档布局。

## 探索代码前读取

开始涉及领域行为、架构或业务边界的工作前，按需读取：

- 根目录 `CONTEXT.md`：aicat 的稳定领域语言；
- [一期设计](../phase-one-design.md) 与 [gateway/traffic 接口设计](../gateway-traffic-design.md)：当前架构及模块边界；
- `docs/adr/`：与当前工作相关、仍适用的架构决策。

当前为从零重写的 mygo Native UI 原型，历史 Claw、消息渠道和 Wails 桌面代码已移除。旧 ADR 保留历史语境，以当前设计文档与 `CONTEXT.md` 为准，不得据此恢复已移除的旧业务。唯一配置、进程边界、网关与 addon 职责、外部 runtime 和私有 fork 的限制均在当前设计中维护，实施时须遵守。

领域文档不存在时直接继续，不把缺失本身当作错误，也不预先创建空文件。在第一个术语或架构决策真正明确时，按需创建；有 `domain-modeling` skill 时使用它，不可用时按本文约定处理。

## 文件布局

```text
/
├── CONTEXT.md
└── docs/
    └── adr/
        ├── 0001-*.md
        └── 0002-*.md
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

如果新方案与当前仍适用的 ADR 冲突，应明确指出冲突并讨论是否重新打开该决策，不能静默覆盖。已被当前设计明确淘汰的历史决策不触发重新确认，不能仅凭旧文档中的 `accepted` 状态将它视为现行约束。当前设计与领域文档之间若存在无法解释的冲突，应先对齐再修改。

只有同时满足以下条件的决策才创建 ADR：

1. 很难撤销；
2. 缺少背景时会让未来维护者感到意外；
3. 确实经过了有意义的方案权衡。
