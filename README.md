# handoff（工具仓库）

Rust 单二进制 CLI，实现跨客户端会话交接的**传输层与标识层**。

- 语言：Rust（单二进制，无运行时依赖）— 计划 §0.2
- 形态：纯 CLI，**非服务** — 计划 §0.3
- 模块：`src/main·cli·id·store·repo·scan·skill·template·config` — 计划 §7

## 状态

- **P1** ✅ 仓库骨架已建。
- **P2** ✅ 已实现 `handoff` CLI 八个子命令 + project_id 派生 + 信任域校验（28 项单测通过，clippy 零告警）。
- **P3** ✅ 完整 `SKILL.md` + `install-skill` 子命令；已链接 WorkBuddy 与 Zed/Codex 两个技能根。
- **P4** 待办：各客户端常驻规则（触发与自动加载）。

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
handoff install-skill [--from <dir>] [--root <dir>]  # 链接 SKILL.md 到客户端技能根
```

`<target>` = `<project_id>/<slug>`（全量）或仅 `<slug>`（从当前目录派生 project_id）。

## 技能安装

SKILL.md 是开放标准（Agent Skills），本工具**不在各客户端保留副本**，而是把技能根下的 `handoff`
符号链接到本仓库根，实现「一处修改、处处生效」：

```bash
handoff install-skill
```

已核实的客户端技能根（2026-09）：

| 客户端 | 全局技能根 |
|---|---|
| WorkBuddy | `~/.workbuddy/skills/` |
| Zed | `~/.agents/skills/` |
| Codex CLI | `~/.agents/skills/`（与 Zed 共用） |

即默认只需维护 **两个** 根目录。命令幂等：已正确链接则跳过；遇到同名实体目录或错误指向会报冲突并**不覆盖**。

> 技能在**会话启动时**装配——安装后需**新开会话**才会被客户端加载。


## 构建

```bash
cargo build --release
# 产物：target/release/handoff
```

## 仓库边界

本仓库只放工具源码与模板；**交接数据不在此处**，分别位于同级 `../work` 与 `../personal`。
