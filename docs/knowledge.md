# 私云 Skill 集成

私云业务技能统一由独立仓库 [netcat-ai/skills](https://github.com/netcat-ai/skills) 维护：

- [netcat](https://github.com/netcat-ai/skills/blob/main/skills/netcat/SKILL.md)：会员资料、用量查询与个人企微授权。
- [wecom-knowledge](https://github.com/netcat-ai/skills/blob/main/skills/wecom-knowledge/SKILL.md)：链接和文本整理、个人企微知识库、视频号下载与微盘保存。

业务流程、依赖、节点限制和业务验证见 [私云知识库说明](https://github.com/netcat-ai/skills/blob/main/docs/knowledge.md)。aicat 继续负责消息读取、身份准入、任务调度、Skill 发现和回复发送。

## 安装与更新

在独立 Skills 仓库执行：

```sh
mkdir -p ~/.aicat/skills
cp -R skills/netcat skills/wecom-knowledge ~/.aicat/skills/
```

两个 Skill 并排安装，保留相互引用和完整配套脚本。更新时在 Skills 仓库拉取所需版本，再复制到同一安装目录；先比对并保留部署节点的本机改动。

所有 Claw Agent 在处理 Room 消息时，通过符号链接共享 `~/.aicat/skills/`，再经 Room 的 `.agents/skills` 发现。Agent 同名专属技能优先，Room 已有内容保留。这不安装到用户全局 Codex skills；共享技能不共享聊天身份或授权。

从旧的 `agent/work/skills/wecom-knowledge` 迁移时，先保留部署节点的本机改动，再将完整技能移到全局目录，移除遮蔽全局版本的旧专属副本。复制技能不会更新已运行的 aicat 二进制或已有模型上下文。

安装本身不会开启其他渠道的 AI 准入，也不执行企微写入。当前身份规则见 [聊天身份识别](identity-binding.md)，官方 CLI 集成见 [企微 CLI](wecom-cli.md)。

## 运行体验证

```sh
make verify
python3 scripts/wecom-remark-e2e.py --binary bin/aicat --output build/identity-check
python3 scripts/wecom-sphfeed-e2e.py --binary bin/aicat --output build/sphfeed-check
```

上述验证使用真实 aicat、本地加密数据库和合成 Skill/Codex/UI 夹具，不依赖私云 Skill 源码，也不操作真实账号、系统代理或 Keychain。产出可复核的 `report.json`、RPC 观察和消息记录。
