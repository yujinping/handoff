---
name: handoff
description: 跨客户端会话交接——把当前任务进度写入本地 markdown 并同步到 git 远端，或在另一台机器/客户端恢复续做。当用户说"交接 / 收尾 / handoff / 续做 / 继续某任务"时触发。
---

# Handoff 交接技能（骨架 · P1）

> 本技能为 P1 占位，完整流程见 P3。此处仅固化契约与依赖，动作全部委托 `handoff` CLI。

## 触发词

交接 / 收尾 / handoff / 续做 / 继续某任务

## 契约（流程）

- **save（保存交接）**：`handoff id` 取当前 project_id → 按模板生成/更新 `tasks/<host_alias>/<ns>/<repo>/<slug>.md` → `handoff scan` → `handoff save`（commit + push）。
- **resume（恢复续做）**：`handoff sync`（pull --rebase && push）→ `handoff list --open` → `handoff show <id>` → 注入上下文 → 执行用户指令。

## 原则

- 真相源是本地 markdown，git 负责同步；不引入任何专有存储。
- 可靠性依赖各客户端常驻规则（见计划 §9），Skill 仅被动加载。
- 信任域隔离：work（codeup）/ personal（gitee）物理分离，`scan` 阻断跨域写入。

## 依赖

需要 `handoff` CLI（P2 实现）已安装并在 PATH；本机 `~/.config/handoff/config.toml` 指向 `ai-handoff` 根。
