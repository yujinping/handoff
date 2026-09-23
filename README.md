# handoff（工具仓库）

Rust 单二进制 CLI，实现跨客户端会话交接的**传输层与标识层**。

- 语言：Rust（单二进制，无运行时依赖）— 计划 §0.2
- 形态：纯 CLI，**非服务** — 计划 §0.3
- 模块：`src/main·cli·id·store·repo·scan·template·config` — 计划 §7

## 状态

- **P1** ✅ 仓库骨架已建。
- **P2** ✅ 已实现 `handoff` CLI 八个子命令 + project_id 派生 + 信任域校验（24 项单测通过，clippy 零告警）。
- **P3** 待办：可移植 Skill 流程落地（SKILL.md 目前仍是契约骨架）。

## 命令

```bash
handoff id [--path <dir>]      # 派生 project_id（<host_alias>:<path>）
handoff where <project_id>     # 显示归属 store 与磁盘路径
handoff new <target> [--scope <s>]   # 从模板创建任务文档
handoff show <target>          # 打印任务文档（注入上下文用）
handoff list [--open] [--store <s>]  # 列出任务
handoff save <target> -m "<msg>"     # 校验（信任域+敏感信息）→ commit → push
handoff sync [--store <s>]     # pull --rebase && push
handoff scan [--store <s>]     # 敏感信息 + 信任域归属扫描
```

`<target>` = `<project_id>/<slug>`（全量）或仅 `<slug>`（从当前目录派生 project_id）。

## 构建

```bash
cargo build --release
# 产物：target/release/handoff
```

## SKILL.md

跨客户端技能定义（开放标准 Agent Skills），与各客户端技能目录符号链接，实现「一处修改、处处生效」：

- WorkBuddy：`~/.workbuddy/skills/handoff/`
- Codex CLI：`~/.agents/skills/handoff/`
- Zed：`.zed/skills/handoff/`

## 仓库边界

本仓库只放工具源码与模板；**交接数据不在此处**，分别位于同级 `../work` 与 `../personal`。
