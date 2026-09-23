# handoff（工具仓库）

Rust 单二进制 CLI，实现跨客户端会话交接的**传输层与标识层**。

- 语言：Rust（单二进制，无运行时依赖）— 计划 §0.2
- 形态：纯 CLI，**非服务** — 计划 §0.3
- 模块：`src/main·cli·id·store·repo·scan·template·config` — 计划 §7

## 状态

- **P1**：仓库骨架已建，`src/main.rs` 占位（打印用法）。
- **P2**：实现 `handoff` CLI（`id` / `where` / `new` / `show` / `list` / `save` / `sync` / `scan`）+ project_id 派生 + 信任域校验。

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
