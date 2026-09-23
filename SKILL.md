---
name: handoff
description: 跨客户端会话交接——把当前任务进度写入本地 markdown 并同步到 git 远端，或在另一台机器/客户端恢复续做。当用户说"交接 / 收尾 / handoff / 续做 / 继续某任务"时触发。
---

# Handoff 交接技能（契约骨架）

> 本技能目前固化契约与依赖，完整流程细化见 P3。动作全部委托 `handoff` CLI（P2 已实现）。

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

- `handoff` CLI（P2 已实现）需安装并在 PATH；构建方式见 `README.md`。
- 本机 `~/.config/handoff/config.toml` 指向 `ai-handoff` 根（或设 `HANDOFF_ROOT` 环境变量）。
