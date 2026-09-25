---
name: handoff
description: "跨客户端会话交接：把当前任务进度写成 markdown 交接文档并同步到 git 远端，或在另一台机器/另一个 AI 客户端恢复续做。当用户说\"交接 / 收尾 / 保存进度 / handoff / 续做 / 继续某任务 / 接着上次做\"时使用。"
description_zh: "跨客户端会话交接——写入/同步/恢复任务级 markdown 交接文档"
description_en: "Cross-client session handoff: save task progress to markdown + git, resume on another machine/agent"
version: 1.0.0
agent_created: true
allowed-tools: Bash,Read,Write,Edit,Grep,Glob,AskUserQuestion
---

# Handoff 交接（跨客户端会话交接）

把「一个任务」的进度写成**本地 markdown 文档**并同步到 git 远端，使同一任务可在**另一台机器、另一个 AI 客户端**上接着做。
真相源是 markdown 文件（可离线读、可 diff、可版本化），git 负责同步，本技能只描述流程——**所有动作委托 `handoff` CLI**。

## 何时使用

| 场景 | 流程 |
|---|---|
| 用户要收尾 / 交接 / 保存进度 / 换机器 | **A. save** |
| 用户说续做 / 继续某任务 / 接着上次做 | **B. resume** |
| 只在当前会话内继续，不涉及跨机 | 不需要本技能 |

## 前置检查（两个流程都要做）

1. **CLI 可用**：`handoff --version`。若命令不存在 → 告知用户需要先安装 `handoff`（见工具仓库 README），并终止流程。
2. **拿到 project_id**：在**目标任务所在的项目目录**下执行 `handoff id`，输出 `project_id` / `store` / `remote`。
   - 若输出 `local:<目录名>`（无 git remote），向用户确认是否照此使用；**不要凭空编造 project_id**。
3. **确认 store 归属**：`work` = 公司项目（codeup 等内网域），`personal` = 个人项目（gitee/github）。**绝不可混写**。

## 流程 A · save（保存交接）

**A1. 确定 slug**
- 与该任务既有文档保持一致：先 `handoff list --open` 查看是否已有同任务文档，**有则复用其 slug**（同一任务跨机必须映射到同一文件）。
- 命名规范：`<模块前缀>--<短横线短语>`，ASCII 小写，例 `v4-core--fix-bearer-401`。monorepo 子模块用前缀（`v4-widget--...`）。
- 与用户确认 slug（若用户已给出任务名，直接派生，不必追问）。

**A2. 创建或复用文档**
- 不存在 → `handoff new <project_id>/<slug> --scope <模块>`（`--scope` 仅 monorepo 子模块需要）。
- 已存在 → 直接编辑该文件，**不要**重复 `new`（会报已存在）。

**A3. 填充字段**（按模板逐项；缺失项写 `none` 而非删除该行）

- `status`：`in-progress` | `blocked` | `done`
- `owner`：**必须等于文档所在 `users/<user_id>/` 的目录名**（`handoff whoami` 查当前值）。这是归属校验字段，写错会被 `scan`/`save` 阻断
- `host`：最后写入的机器，用 `hostname -s` 取值
- `updated`：用命令取时间，**不要手写**：`date "+%Y-%m-%d %H:%M"`
- `goal`：一句话目标
- `current`：已完成到什么程度（叙述式，便于他人接手）
- `decisions`：已定决策 **+ 理由**（避免接手者重复讨论）
- `files`：涉及文件（推荐 `@filepath:line-range` 语法）
- `next`：**编号的可执行下一步**（最重要字段——决定接手者能否立刻动手）
- `open`：待确认问题
- `blockers`：阻塞项
- `env`：关键命令 / 环境变量 / 依赖前提
- `refs`：相关文档与链接

**A4. 自查后再提交**
1. `handoff scan` → 必须输出 `clean`。若有违规，**先修文档**，不要绕过。
2. `handoff save <project_id>/<slug> -m "<类型>(<范围>): <描述>"`（提交信息遵循用户既有约定式规范）。
3. 输出给用户：文档路径、project_id、以及在另一台机器上的恢复命令。

## 流程 B · resume（恢复续做）

**B1. 同步**
- `handoff sync`（先 `pull --rebase` 再 `push`）。
- 若 store 尚未克隆到本机 → 先 `git clone <remote> <store 目录>`，再继续。
  （store 根默认 `~/.handoff`，即 `~/.handoff/work` / `~/.handoff/personal`；可用 `HANDOFF_ROOT` 或
  `~/.config/handoff/config.toml` 的 `stores.root` 覆盖。）
- 若 `push` 失败但 `pull` 成功：**不阻塞**，继续 B2，并在结束时提示用户。

**B2. 定位任务**
- 用户已指明任务 → 直接 `handoff show <target>`。
- 未指明 → `handoff show latest`（最近更新的未完成任务）或 `handoff list --open`
  列出未完成任务（输出含完整可复制的 target），让用户选择。若列表为空，如实告知。

**B3. 读文档并校验**
- `handoff show <target>` 读取全文。
- 若 `status: done` → 提示用户该任务已归档，**询问**是否仍要继续，不要默默修改。

**B4. 复述与确认**
- 向用户复述三件事：**目标**（goal）、**当前进度**（current）、**建议的下一步**（next）。
- 与用户确认接下来做哪一项，然后执行用户指令。

**B5. 续做完成后**
- 更新同一 slug 文档（`updated` / `current` / `next` / `open`），再走 **A4** 提交。
- 任务真正完成时，将 `status` 置 `done`。

## 冲突与异常处理

| 情况 | 处理 |
|---|---|
| `sync` 出现 rebase 冲突 | **不要** `git push --force`。保留差异，将冲突点写入文档 `open:`（附两端内容摘要），提示用户人工确认后重试 |
| 同一 slug 被多机并行编辑 | 提示用户按主机拆分（slug 加 `-<host>` 后缀）或串行操作；不要静默覆盖 |
| `scan` 报跨信任域 | 文档写入的 store 与其 `project_id` 归属不一致 → 移动到正确 store，不要改 `project_id` 就范 |
| `scan`/`save` 报 owner 归属违规 | 文档 `owner` 与所在 `users/<user_id>/` 不符 → 二选一：把 `owner` 改成与目录一致，或把文件移到自己的命名空间。**不要**为了过校验随意改 `owner` |
| `save` 报「不在 `users/<user_id>/` 命名空间下」 | 文档仍在旧扁平布局 → 迁移到 `users/<user_id>/tasks/...` 后重试 |
| `scan` 报敏感信息 | 删除对应内容后重试；**不得** `--no-verify` 或绕过 |
| `handoff` 命令缺失 | 终止并告知安装方式，不要用裸 `git` 手工模拟流程 |

## 安全红线

1. **禁止**在交接文档写入 token / 密钥 / 口令 / 私钥 / 带凭据的连接串。`scan` 会阻断，但应先自查。
2. **信任域物理隔离**：公司项目（`work`）文档只能落在 work store；个人项目只能落在 personal store。跨域即信息外泄。
3. **用户命名空间**：只写自己 `users/<user_id>/` 下的文档，`owner` 必须与目录名一致。注意同一仓库内多用户**无读隔离**——命名空间防的是误写，不是保密手段。
4. **不强制推送**：任何情况下不用 `--force` / `--force-with-lease`。
5. 不确定归属时，先 `handoff where <project_id>` 查清落点再写。

## 命令速查

| 命令 | 用途 |
|---|---|
| `handoff id` | 派生当前目录的 project_id + user_id + store |
| `handoff whoami` | 查看当前 user_id 与其命中级 |
| `handoff where <id>` | 查 project 的归属 store 与磁盘路径 |
| `handoff new <id>/<slug>` | 从模板建任务文档 |
| `handoff show <id>/<slug>` | 打印文档（注入上下文） |
| `handoff show latest` | 打印最近更新的未完成任务（免记长 target） |
| `handoff list --open` | 列出未完成任务（输出含可直接复制的完整 target） |
| `handoff save <id>/<slug> -m "…"` | 校验 + 提交 + 推送 |
| `handoff sync` | pull --rebase && push；结束后提示最新未完成任务 |
| `handoff scan` | 敏感信息 + 信任域 + owner 扫描 |

## 说明

- 本技能**不会自动触发**（Skill 均为按需加载）。「任务收尾自动写交接 / 新会话自动先取档」由各客户端
  **常驻规则**承担，内容源为 `rules/agent-rules.md`，已注入 `~/.workbuddy/MEMORY.md`（WorkBuddy）、
  `~/.codex/AGENTS.md`（Codex）、`~/.config/zed/AGENTS.md`（Zed）。
- **主动触发的边界**：常驻规则仅在「`handoff id` 的 host_alias 属已登记域」或「该 project_id 已有任务文档」
  时生效；否则静默跳过。但若是**用户明确要求交接**（点名 / 说"交接""保存进度"），即使落在
  `local:<目录名>` 也照 §前置检查-2 向用户确认后执行。
- 本技能正文不依赖任何客户端专有界面；跨客户端（WorkBuddy / Codex / Zed）行为一致。
