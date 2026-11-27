---
name: handoff
description: 按远程 git 仓库交接 AI 会话现场——把当前会话的目标、进展、下一步、文件、决定和未决问题保存下来，或在同一仓库接手最新交接继续干。用户说「交接」「接手」「handoff」「换会话继续」「把现场交给下一个会话」时使用。
---

# handoff

交接的归属是**远程 git 仓库**，不是会话、不是本机路径、不是分支。同一个人在本机换个 AI、或在另一台设备上，只要在同一个仓库里，就能接上。

## 第一步：确认项目

在仓库根目录执行：

```bash
git remote get-url origin
```

- 有输出：继续。
- 没有 origin：停下来告诉用户「handoff 按远程仓库归属，请先给这个仓库加 origin」。不要用本地路径代替。

## 能力探测（按顺序，别跳）

1. 工具列表里有 `handoff_save` / `handoff_list` / `handoff_load` / `handoff_ack` → 用 MCP。
2. 否则 `handoff --version` 能跑 → 用 CLI。
3. 都没有 → 在仓库 `.local/handoff/` 写一份 Markdown，并在回复里说明「handoff 未安装，已降级为本地文件」。

MCP 和 CLI 的字段完全一致，不要发明新字段。

## 交接：保存现场

先把会话压缩成六段，**只写结论和可执行信息**，不要贴原始对话、大段日志、寒暄：

| 字段 | 写什么 |
| --- | --- |
| `goal` | 这次要达成什么，一句话 |
| `state` | 已经做到哪、改了什么、验证过什么 |
| `next_steps` | 接手的人下一步具体做什么，按顺序、可执行 |
| `files` | 涉及的文件路径（相对仓库根） |
| `decisions` | 已拍板且不该被推翻的决定 **和理由** |
| `open_questions` | 还没定、需要用户或下一会话决定的事 |

调用：

```
handoff_save {
  repo: "<git remote get-url origin 的输出>",
  client: "codex",                  // 你当前客户端
  workstream: "current@codex",      // 可省略，默认就是这个
  goal, state, next_steps, files, decisions, open_questions
}
```

CLI 等价写法：

```bash
handoff save --client codex --goal "..." --state "..." \
  --next "..." --file path/to/file --decision "..."
```

规则：

- 同一仓库有并行工作（多个分支/多个任务）时，显式传不同的 `workstream`，不要让它们互相覆盖。
- 每次 save 都是新 revision，旧的不删；不要试图「修改」上一条。
- 保存后向用户回报 `workstream` 和 `revision`，一句话即可。

## 接手

1. `handoff_list { repo }` 看待接手的条目。
2. 只有一条：直接 `handoff_load`。多条：把 `workstream / goal / 更新时间` 列给用户选，不要自己挑。
3. 读到后先向用户复述 `goal` 和 `next_steps`，确认这是要接的活。
4. `handoff_ack { repo, workstream, by: "<你的客户端名>" }` 确认接手。ack 是 CAS，冲突（409）说明别人先接了，回到第 1 步。
5. 按 `next_steps` 继续。`decisions` 里的结论不要重新讨论；`open_questions` 先问用户。

本地没有待接手时 `handoff_load` 会返回 `found: false` 或 404，不要把它当错误刷屏，直接告诉用户「这个仓库没有待接手的交接」。

## 硬规则

- 不写密钥、token、密码、个人隐私、客户数据。
- 单字段 ≤ 16 KiB；数组 ≤ 64 项、每项 ≤ 4 KiB；整条 ≤ 64 KiB。超了就拆成多条交接，或只留结论。
- 交接是给「下一个会话」看的，不是给用户看的汇报：不要礼貌用语，不要重复用户已经知道的事。
- 远程模式（`HANDOFF_SERVER` / `--server`）下所有命令都走 REST，不要直连别人机器上的数据库文件。