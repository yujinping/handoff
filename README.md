# handoff（工具仓库）

Rust 单二进制 CLI，实现跨客户端会话交接的**传输层与标识层**。

- 语言：Rust（单二进制，无运行时依赖）— 计划 §0.2
- 形态：纯 CLI，**非服务** — 计划 §0.3
- 模块：`src/main·cli·id·store·repo·scan·skill·template·config` — 计划 §7

## 支持平台

| 层级 | 平台 | 说明 |
|---|---|---|
| 一等 | **Linux x86_64**、**macOS**（arm64 / x86_64）、**WSL2** | 必须始终可用；WSL2 与 Linux 同构，直接复用 Linux 二进制 |
| 尽力 | **Git Bash**（Git for Windows） | 可用，但需 `x86_64-pc-windows-gnu` 构建产物（**不能复用 Linux 二进制**）；`install-skill` 不可用，需手动复制技能目录 |
| 不支持 | 原生 Windows（PowerShell / cmd，无 Unix 环境） | 不承诺、不测试、不做适配 |

> 平台范围于 2026-09-23 锁定（计划 §0.4 / Q11）。同事接入**首选 WSL2**（零额外构建）；Git Bash 为兜底路径。
> `install-skill` 依赖符号链接，在非 Unix 平台会**明确报错**而非静默失败（`src/skill.rs` 的 `#[cfg]` 隔离）。

## 状态

- **P1** ✅ 仓库骨架已建。
- **P2** ✅ 已实现 `handoff` CLI 八个子命令 + project_id 派生 + 信任域校验（28 项单测通过，clippy 零告警）。
- **P3** ✅ 完整 `SKILL.md` + `install-skill` 子命令；已链接 WorkBuddy 与 Zed/Codex 两个技能根。
- **P4.5** ✅ 多用户与身份标识：`users/<user_id>/` 命名空间、user_id 四级解析、`owner`/`host`/`updated` 字段拆分、owner 归属校验、`whoami` 子命令；三仓库已迁移到 `users/` 布局并加 `.gitattributes`。
- **P4** 待办：各客户端常驻规则（触发与自动加载）。

## 多用户与身份标识（§4.5）

交接数据按 `users/<user_id>/` 分命名空间，同一任务在不同用户下互不覆盖：

```
<store>/users/<user_id>/
├─ tasks/<host_alias>/<ns>/<repo...>/<slug>.md
├─ sessions/<YYYY-MM-DD>-<host>.md
└─ archive/<host_alias>/<ns>/<repo...>/<slug>.md
```

`user_id` 四级解析（先命中先用）：`HANDOFF_USER` 环境变量 → `~/.config/handoff/config.toml` 顶层 `user` →
`git config --global user.name` → `whoami`（各级 sanitize 后无有效字符则降级；全失败兜底常量 `user`）。
用 `handoff whoami` 查看当前值与其命中级。

文档 front-matter 的 `owner` **必须等于所在 `users/<user_id>/` 目录名**，`save`/`scan` 会强制校验。

> **边界**：共享同一仓库 = **无读隔离**（有仓库读权限即可读全部命名空间）。命名空间防的是**误写与归档混乱**，
> 不是安全边界；真实保密必须靠仓库级隔离。

## 命令

```bash
handoff id [--path <dir>]      # 派生 project_id（<host_alias>:<path>）+ user_id + store
handoff whoami                 # 打印当前 user_id 与命中级（四级解析诊断）
handoff where <project_id>     # 显示归属 store 与磁盘路径
handoff new <target> [--scope <s>]   # 从模板创建任务文档
handoff show <target>          # 打印任务文档（注入上下文用）
handoff list [--open] [--store <s>]  # 列出任务
handoff save <target> -m "<msg>"     # 校验（信任域+owner+敏感信息）→ commit → push
handoff sync [--store <s>]     # pull --rebase && push
handoff scan [--store <s>]     # 敏感信息 + 双重归属扫描
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
> Git Bash 下 `install-skill` 不可用（Windows 构建走 `#[cfg(not(unix))]` 分支并明确报错）；该环境请手动把
> 本仓库目录复制/链接到上述技能根。


## 构建

```bash
cargo build --release
# 产物：target/release/handoff
```

## 仓库边界

本仓库只放工具源码与模板；**交接数据不在此处**，分别位于同级 `../work` 与 `../personal`。
