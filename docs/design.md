# handoff 设计文档

> 用远程 git 仓库地址当项目键，把 AI 会话现场存进一个 SQLite 文件。
> 本机用 stdio MCP 或 CLI，跨设备用 HTTP MCP；一份数据，三个入口，不做同步。

状态：v0.1，待实现
最后更新：2026-11-27

---

## 1. 问题

AI 会话的上下文随着会话结束而消失。换一个 AI 客户端、换一台设备、或者同一客户端开新会话，都要从头把项目讲一遍。现有 Memora 项目已经实现了 handoff，但它是 memory + handoff 的一体化服务：24k 行 Rust、axum + sea-orm + rmcp、带 HTTP 服务、嵌入、检索和一堆配置。handoff 本身只需要四件事：**存现场、列现场、读现场、确认已读**。

本项目的目标是把这四件事做到足够小、足够可靠，让「交接」成为一条命令，而不是一套基础设施。

主要场景按频率排序：

1. 同一台机器上，不同 AI 客户端之间交接（Claude Code → Codex → Zed），最高频。
2. 不同设备之间交接（公司 Mac → 家里 Mac → 服务器）。
3. 同一客户端里多个并行会话交接（一个改后端，一个改前端）。

## 2. 目标与非目标

### 目标

- 单用户、多项目，每个项目允许多条工作流。
- 一次 MCP 工具调用或一条 CLI 命令保存现场；接手的 AI 一次调用拿到最新未确认现场。
- 本机零配置：无守护进程、无 token、无网络，断网可用。
- 跨设备只需在持有数据库的机器上跑 `handoff serve`，其余设备连过来。
- 单文件 SQLite：备份就是复制文件，迁移就是拷贝文件。
- 数据可读：数组字段以 JSON 文本存储，`sqlite3` 直接可查。
- 跨平台单二进制：linux/darwin/windows × amd64/arm64，`CGO_ENABLED=0`。

### 非目标

- 多租户、团队共享、细粒度 ACL。（单用户；token 只解决远程入口认证）
- 记忆（memory）、向量检索、全文搜索。（后续可加 FTS，不影响现有表）
- 双向同步、冲突合并、离线队列。（远程只是同一文件的另一个入口）
- 自动从 git diff 推断状态。（由 skill 提示模型自己总结）
- Web UI、账号体系、配额、审计日志。

## 3. 三种形态，一套代码

| 形态 | 命令 | 传输 | 谁拉起 | 认证 | 数据 |
|---|---|---|---|---|---|
| 本地 MCP | `handoff mcp` | stdio | AI 客户端 | 无 | 直接打开 db 文件 |
| 本地 CLI | `handoff save/list/load/ack` | — | 人/脚本 | 无 | 直接打开 db 文件 |
| 远程 MCP + REST | `handoff serve` | Streamable HTTP | 用户 | Bearer token | 进程持有 db 文件 |

硬性约束：三种形态都必须调用同一个 `internal/service`。不允许出现第二套存储逻辑，也不允许出现同步逻辑。远程模式下的 CLI 是瘦客户端，走 REST，不直接碰远程文件。

```
                 ┌─────────────────────────────┐
 Claude Code ──stdio──▶ handoff mcp ──┐        │
 Codex       ──stdio──▶ handoff mcp ──┤        │
 人          ──cli────▶ handoff cli ──┼──▶ service ──▶ store ──▶ handoff.db
 远程设备    ──http───▶ handoff serve ┘        │
                 └─────────────────────────────┘
```

## 4. 概念模型

- **Project**：一个远程 git 仓库。键是归一化后的 remote URL，不是本地路径。
- **Workstream**：项目内的一条工作线，例如 `current@claude-code`、`review`、`hotfix`。交接挂在 workstream 上，不挂在分支上——同一分支也可能并行两条工作线。
- **Handoff**：一条不可变快照。`revision` 从 1 递增，再次保存产生新 revision，历史永远保留。
- **Ack**：接手方确认「我已读到」。只对最新 revision 有意义；ack 旧 revision 不改变 pending 状态。
- **Pending**：每个 `(project, workstream)` 的最新 revision，且 `acked_at IS NULL`。

默认 workstream 为 `current@<client>`，`client` 缺省为 `unknown`。这样两个 AI 客户端在同一仓库并行工作时不会互相覆盖；想接力时显式传同一个 workstream。

## 5. 数据模型

时间统一存 Unix 毫秒（INTEGER）。数组字段存 JSON 文本（TEXT），空数组写 `[]`。

```sql
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
PRAGMA synchronous = NORMAL;

CREATE TABLE IF NOT EXISTS projects (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  remote_key   TEXT    NOT NULL UNIQUE,
  display_name TEXT    NOT NULL DEFAULT '',
  created_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS tokens (
  id            TEXT    PRIMARY KEY,          -- hdt_ 后的 8 位 hex
  name          TEXT    NOT NULL,
  hash          TEXT    NOT NULL UNIQUE,      -- sha256(secret) 的 hex
  scope_project TEXT,                         -- NULL = 所有项目；否则为 remote_key
  created_at    INTEGER NOT NULL,
  last_used_at  INTEGER,
  revoked_at    INTEGER
);

CREATE TABLE IF NOT EXISTS handoffs (
  project_id     INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  workstream     TEXT    NOT NULL,
  revision       INTEGER NOT NULL,
  goal           TEXT    NOT NULL,
  state          TEXT    NOT NULL,
  next_steps     TEXT    NOT NULL DEFAULT '[]',
  files          TEXT    NOT NULL DEFAULT '[]',
  decisions      TEXT    NOT NULL DEFAULT '[]',
  open_questions TEXT    NOT NULL DEFAULT '[]',
  client         TEXT    NOT NULL DEFAULT '',
  session_id     TEXT    NOT NULL DEFAULT '',
  created_at     INTEGER NOT NULL,
  acked_at       INTEGER,
  acked_by       TEXT    NOT NULL DEFAULT '',
  PRIMARY KEY (project_id, workstream, revision)
);

CREATE INDEX IF NOT EXISTS idx_handoffs_pending
  ON handoffs (project_id, acked_at, created_at DESC);
```

设计说明：

- `projects` 只增不改（`display_name` 允许更新）。仓库改名/迁移会产生新 project，这是有意的：键就是身份。
- `tokens.scope_project` 用 remote_key 而不是 project_id，避免建 token 时必须先有项目行。
- `handoffs` 不存 `repo` 字符串，靠 `project_id` 关联，避免同一仓库多种 URL 写法写入不同项目。
- 不建 revisions 表：revision 就是 `MAX(revision)+1`，单写者场景足够。

## 6. 项目键归一化

输入是 `git remote get-url origin` 的输出，或用户手填的 URL。规则：

1. 去首尾空白；拒绝空串和含控制字符的输入。
2. 支持 `git@host:path`、`ssh://[user@]host[:port]/path`、`https://[user[:pass]@]host[:port]/path`、`http://...`、`git://...`，以及无 scheme 的 `host/path`。
3. 去掉 scheme、userinfo、端口。
4. host 转小写。
5. path 去掉首尾 `/`，折叠重复 `/`，去掉末尾 `.git`。
6. 已知大小写不敏感的托管平台，path 也转小写：`github.com`、`gitlab.com`、`bitbucket.org`、`gitee.com`、`codeup.aliyun.com`。
7. 输出 `host/path`，例如 `github.com/yujinping/handoff`。

拒绝的情况，返回可读错误而不是猜测：

- 本地路径（`/Users/...`、`./repo`、`../repo`）、`file://`。
- 没有 path（`https://github.com`）。
- path 含 `..` 段。
- 无法识别的 scheme（`ftp://` 等）。

示例：

| 输入 | 归一化结果 |
|---|---|
| `git@github.com:Owner/Repo.git` | `github.com/owner/repo` |
| `https://github.com/owner/repo.git` | `github.com/owner/repo` |
| `ssh://git@gitlab.com:2222/group/sub/repo.git` | `gitlab.com/group/sub/repo` |
| `https://user:pass@example.com/A/B` | `example.com/A/B` |
| `HTTPS://GitHub.com/O/R/` | `github.com/o/r` |
| `git@example.com:a/b.git` | `example.com/a/b` |

后续可加 `project_aliases` 表处理仓库改名，v0.1 不做。

## 7. 接口

### 7.1 MCP 工具

stdio 与 HTTP 暴露完全相同的四个工具。参数与返回均为 JSON object，字段用 `jsonschema` tag 描述。

| 工具 | 入参 | 返回 | 语义 |
|---|---|---|---|
| `handoff_save` | `repo`(必填), `goal`, `state`, `next_steps[]`, `files[]`, `decisions[]`, `open_questions[]`, `workstream?`, `client?`, `session_id?` | 新 handoff 全文 | 追加一条 revision |
| `handoff_list` | `repo`(必填), `limit?`（默认 20，上限 100） | pending 摘要数组 | 列出待接手的现场 |
| `handoff_load` | `repo`(必填), `workstream?` | `{found, handoff}` | 有 workstream 取该线最新；否则取最新 pending |
| `handoff_ack` | `repo`(必填), `workstream?`, `by?` | 被确认的 handoff | 原子确认最新 pending；已被确认返回冲突 |

省略 `workstream` 时：save 用 `current@<client>`；list 列所有线；load/ack 选全局最新 pending。

校验上限（防止把整段对话塞进库里）：

- `goal`、`state`：各 16 KiB。
- `next_steps`、`files`、`decisions`、`open_questions`：各 64 项，每项 4 KiB。
- 整条 handoff：64 KiB。
- `workstream`、`client`：128 字符，限 `[a-zA-Z0-9._@-]`，统一转小写。
- `session_id`：128 字符。

错误通过工具结果的 `isError` 返回，消息面向模型，例如：`repo is required: pass the project's origin remote URL`、`no handoff pending for github.com/a/b`。

### 7.2 REST

给 CLI 远程模式和脚本使用，JSON 请求/响应，Bearer 认证。

```
POST /api/v1/handoffs            保存
GET  /api/v1/handoffs?repo=&limit=   列表
GET  /api/v1/handoffs/latest?repo=&workstream=   读取
POST /api/v1/handoffs/ack        确认
GET  /healthz                    无认证
```

错误格式：`{"error":{"code":"not_found","message":"..."}}`，HTTP 状态码 400/401/403/404/409/500。

### 7.3 CLI

```
handoff init                         创建数据库与目录
handoff mcp                          本地 stdio MCP 服务
handoff serve [--addr]               远程 HTTP 服务
handoff save --repo R [--workstream W] [--client C] < handoff.json
handoff list --repo R [--limit N]
handoff load --repo R [--workstream W]
handoff ack  --repo R [--workstream W] [--by NAME]
handoff token add --name N [--scope REPO]
handoff token list
handoff token revoke ID
handoff version
```

- `save` 从 stdin 读 JSON（字段同 MCP 入参），便于 skill 与脚本调用；输出保存后的 handoff JSON。
- `list` 输出对齐的表格；`--json` 输出机器可读格式。
- 远程模式：`--server URL` 或 `HANDOFF_SERVER`，`--token` 或 `HANDOFF_TOKEN`。设置后 save/list/load/ack 全部走 REST。
- 退出码：0 成功，1 业务错误（无 pending、校验失败、冲突），2 用法错误，3 网络/服务不可用。

## 8. 认证

- token 格式：`hdt_<id>_<secret>`。`id` 为 8 位 hex，用于定位行；`secret` 为 32 字节随机数的 base64url。
- 数据库只存 `sha256(secret)`；明文 token 只在 `token add` 时打印一次。
- 校验：解析 id → 查行 → 若 `revoked_at` 非空拒绝 → 比较 hash（`subtle.ConstantTimeCompare`）→ 若 `scope_project` 非空则要求项目匹配。
- `last_used_at` 最多每 60 秒更新一次，避免每次请求都写库。
- stdio 与本地 CLI 不需要 token：能读这个文件的人本来就有全部权限。
- `serve` 启动时若没有活跃 token，拒绝启动并提示先运行 `handoff token add --name <设备名>`。

## 9. 并发与一致性

- 打开数据库时设置 WAL、`busy_timeout=5000`、`synchronous=NORMAL`、`foreign_keys=ON`。
- `save`：`BEGIN IMMEDIATE` → 取 `MAX(revision)` → 插入 `+1` → 提交。SQLite 的写锁保证同一 workstream 的 revision 连续。
- `ack`：单条 `UPDATE ... WHERE project_id=? AND workstream=? AND revision=? AND acked_at IS NULL`，检查 `RowsAffected`；为 0 表示已被确认或不存在，返回冲突。不要先查后写。
- 不做分布式锁。一个 db 文件只应被本机进程与一个 `serve` 使用；**禁止放在 NFS/SMB/iCloud 同步目录**，文件锁在这些文件系统上不可靠。
- 多设备同时写同一个 db 不在支持范围内；跨设备请只通过 `serve` 访问。

## 10. 配置

优先级：命令行 flag > 环境变量 > 默认值。

| 变量 | 默认 | 说明 |
|---|---|---|
| `HANDOFF_DB` | `~/.handoff/handoff.db` | 数据库路径 |
| `HANDOFF_SERVER` | 空 | 设置后 CLI 走远程 REST |
| `HANDOFF_TOKEN` | 空 | 远程 Bearer token |
| `HANDOFF_ADDR` | `127.0.0.1:8787` | serve 监听地址 |

创建数据库时目录 `MkdirAll 0700`，文件 `0600`。`init` 幂等。

## 11. 构建与分发

- 纯 Go：`modernc.org/sqlite`（无 CGO），`github.com/modelcontextprotocol/go-sdk`。
- `make dist` 交叉编译五个目标：`linux/amd64`、`linux/arm64`、`darwin/arm64`、`darwin/amd64`、`windows/amd64`。
- GitHub Actions 在 tag 上构建并上传 release 资产。
- 预期二进制 15–25 MB（modernc SQLite 占大头），可接受；换来的是每台设备一条 `curl` 就能装。

## 12. 测试策略

- `internal/repo`：表驱动，覆盖第 6 节全部示例与拒绝分支。
- `internal/store`：迁移幂等；并发 save 得到连续 revision；ack CAS；token 校验与吊销；scope 限制。
- `internal/service`：字段上限、默认 workstream、pending 语义、空数组序列化。
- `internal/mcpserver`：用 SDK 客户端拉起 stdio 子进程，跑 save → list → load → ack 冒烟。
- `internal/httpapi`：`httptest` 覆盖无 token/错 token/正确 token、REST 四个端点、404/409。
- `internal/cli`：本地模式端到端（临时目录 + 临时 db）。

## 13. 目录结构

```
handoff/
  cmd/handoff/main.go          入口，解析子命令
  internal/config/             配置与路径
  internal/repo/               远程地址归一化
  internal/store/              SQLite：schema、projects、tokens、handoffs
  internal/service/            业务校验与编排
  internal/mcpserver/          MCP 工具定义（stdio/HTTP 共用）
  internal/httpapi/            serve、Bearer 中间件、REST、客户端
  internal/cli/                子命令实现
  skills/handoff/SKILL.md      给 AI 客户端的交接/接手 skill
  docs/design.md               本文档
```

## 14. 里程碑

- **M1 核心**：repo + store + service，测试通过。
- **M2 CLI 本地**：init/save/list/load/ack/token，端到端可用。
- **M3 MCP stdio**：四个工具，用 SDK 客户端冒烟。
- **M4 远程**：serve + Bearer + REST + CLI 远程模式。
- **M5 分发**：Makefile、CI、skill、README。

## 15. 决策记录

- **为什么用 Go**：真正的理由是交叉编译成本。stdio 模式要求每台设备都有二进制；Go 用 `CGO_ENABLED=0` 一条命令出五个平台，Rust 的 `libsqlite3-sys` 需要目标平台 C 工具链，从 Linux 交叉到 macOS 基本不可行。「编译快」是附带好处。
- **为什么单用户**：多用户需要 ACL、配额、审计、管理台，会让 handoff 从「一个文件」变成「一个服务」。需要共享时，把 db 放在共享机器上跑 `serve` 即可。
- **为什么 workstream 不绑分支**：分支是代码维度，工作流是人的维度。一个分支可以同时有「实现」和「review」两条线；绑死分支会让并行工作互相覆盖。
- **为什么 ack 而不是删除**：历史快照是排查「上次交接说了什么」的唯一依据；删除会让接手方误以为没交接。ack 只标记已读，数据仍在。
- **为什么不做同步**：同步要处理冲突、时钟、离线队列，是另一个产品。远程模式已经是「同一份数据」，不需要同步。
- **为什么远程 CLI 走 REST 而不是 MCP**：CLI 不是 AI，不需要工具发现；REST 更简单，也方便 curl 排障。
- **为什么不用 UUID 当主键**：单机单文件，自增整数够用，且 revision 需要连续。

## 16. 风险与未决

- `modernc.org/sqlite` 在极端并发下比 CGO 版慢；本项目是低频写入，可接受。
- 用户把 db 放进 iCloud/Dropbox 会导致锁失效；文档与错误提示需要明确警告。
- token 泄露：只存 hash、可吊销、可限定项目；传输层需用户自行用 HTTPS/Tailscale。
- 仓库改名会产生新项目，旧 handoff 不会自动跟随；v0.1 接受，后续用 alias 表解决。
- `handoff_list` 只列 pending；如果用户想「看看历史」，后续加 `handoff log`。