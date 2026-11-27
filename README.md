# handoff

按**远程 git 仓库**交接 AI 会话现场：在一个客户端里把目标、进展、下一步存下来，在另一个客户端、另一台机器上原样接上。

只做这一件事。单用户、多项目、一条命令、一个二进制。

## 为什么是 Go

- **一个静态二进制**：本地模式每个 AI 客户端都拉起自己的 `handoff mcp`，所以每台设备、每个平台都得有可执行文件。`CGO_ENABLED=0` 一条命令出 linux/amd64、linux/arm64、darwin/amd64、darwin/arm64、windows/amd64。
- **纯 Go SQLite**：`modernc.org/sqlite`，不需要目标平台的 C 工具链，交叉编译不会卡在 libsqlite3。
- **启动快**：stdio MCP 每次会话都要冷启动，Go 进程启动是毫秒级。

## 三种形态，一份数据

| 形态 | 谁在用 | 怎么连数据库 |
| --- | --- | --- |
| 本地 stdio MCP | AI 客户端（Claude Code、Codex、Cursor…） | 直接打开本机 SQLite |
| 本地 CLI | 人、脚本、没有 MCP 的客户端 | 直接打开同一个文件 |
| 远程 HTTP MCP + REST | 其他设备 | `handoff serve` 持有文件，Bearer token |

没有同步、没有第二份存储。远程只是同一文件的另一个入口，所以「哪份是最新的」永远不需要回答。

## 安装

一条命令，自动识别系统和架构，下载最新 Release 并校验 SHA256：

```bash
curl -fsSL https://raw.githubusercontent.com/yujinping/handoff/main/scripts/install.sh | sh
```

默认装到 `~/.local/bin`。指定版本或目录：

```bash
curl -fsSL https://raw.githubusercontent.com/yujinping/handoff/main/scripts/install.sh \
  | sh -s -- --version v0.2.0 --dir /usr/local/bin
```

支持 Linux/macOS 的 amd64、arm64。Windows 用 PowerShell，自动识别架构、校验 SHA256 并加入用户 PATH：

```powershell
irm https://raw.githubusercontent.com/yujinping/handoff/main/scripts/install.ps1 | iex
```

默认装到 `%LOCALAPPDATA%\Programs\handoff`。指定版本或目录时，管道方式用环境变量：

```powershell
$env:HANDOFF_VERSION='v0.2.0'; $env:HANDOFF_INSTALL_DIR='C:\tools'
irm https://raw.githubusercontent.com/yujinping/handoff/main/scripts/install.ps1 | iex
```

仅支持 Windows x64；Windows on ARM 不提供构建，脚本会直接报错。下载脚本后也可直接传参：

```powershell
.\install.ps1 -Version v0.2.0 -Dir C:\tools -NoPath
```

其他方式：

```bash
# 从源码
go install github.com/yujinping/handoff/cmd/handoff@latest
```

GitHub 下载慢时，可用 `HANDOFF_BASE_URL` 指向镜像前缀（形如 `https://镜像/https://github.com/yujinping/handoff/releases/download`）。

## 本机使用

```bash
handoff init                 # 创建 ~/.handoff/handoff.db
cd ~/code/my-project         # 必须在有 origin 的 git 仓库里
handoff save --client codex \
  --goal "把构建脚本从 Make 迁到 Go" \
  --state "已迁完 build 和 test" \
  --next "迁 release" --file scripts/build.sh
handoff list                 # 看当前仓库待接手的交接
handoff load                 # 读取最新一条
handoff ack --by claude      # 接手方确认
```

`--repo` 不传时自动用当前目录的 `git remote get-url origin`，并归一化成 `host/owner/repo`。本地路径和 `file://` 不被接受——交接必须挂在所有设备都认得的远程仓库上。

save 也接受 stdin JSON，适合 agent 一次写全：

```bash
handoff save < handoff.json
```

```json
{
  "client": "codex",
  "session_id": "abc123",
  "goal": "把构建脚本从 Make 迁到 Go",
  "state": "build/test 已迁完，release 还没动",
  "next_steps": ["迁 release", "删掉旧 Makefile"],
  "files": ["scripts/build.sh", "Makefile"],
  "decisions": ["保留 make test 作为入口，内部调 go test"],
  "open_questions": ["Windows 上要不要保留 bat 包装"]
}
```

## 接入 AI 客户端（MCP）

Claude Code：

```bash
claude mcp add handoff -- handoff mcp
```

Codex（`~/.codex/config.toml`）：

```toml
[mcp_servers.handoff]
command = "handoff"
args = ["mcp"]
```

Cursor / 其他 JSON 配置：

```json
{
  "mcpServers": {
    "handoff": {
      "command": "handoff",
      "args": ["mcp"]
    }
  }
}
```

四个工具：`handoff_save`、`handoff_list`、`handoff_load`、`handoff_ack`。参数与 CLI 字段一一对应。

## 技能（/handoff）

`skills/handoff/SKILL.md` 是技能的**唯一源**，要改行为就改它，不要改已安装的副本。

发布到本机所有 AI 客户端（`~/.agents/skills` 是 → 本机 agents-skills 仓库 `skills/` 的软链）：

```bash
cp skills/handoff/SKILL.md ~/.agents/skills/handoff/SKILL.md
```

然后到 agents-skills 仓库提交 `skills/handoff/SKILL.md` 与 README 的表格，副本才会同步到其他机器。
技能本身优先调 MCP 工具，其次 CLI，两者都没有才降级成仓库内 `.local/handoff/` 文件。

## 跨设备

在存有数据库的机器上：

```bash
handoff token add --name macbook     # 明文只打印一次
handoff serve --addr 127.0.0.1:8787  # 前面挂 Tailscale / 反代
```

另一台设备：

```bash
export HANDOFF_SERVER=https://handoff.example.com
export HANDOFF_TOKEN=hd_...
handoff list                         # 自动走 REST
```

远程 MCP 客户端填 `https://handoff.example.com/mcp`，带 `Authorization: Bearer <token>`。

宿主机离线时远程不可用——这是「一份数据」的代价，也是它简单的原因。

## 语义与限额

- **项目键**：`git remote get-url origin` 归一化后的 `host/owner/repo`。`git@github.com:Acme/Widgets.git`、`https://github.com/acme/widgets`、`ssh://git@github.com/acme/widgets.git` 是同一个项目。
- **workstream**：交接挂在工作流上，不挂在分支上。默认 `current@<client>`，同一仓库并行干活时用 `--workstream` 分开，避免互相覆盖。
- **只追加**：每次 save 产生新 revision，旧 revision 不删不改。
- **ack 是 CAS**：`UPDATE ... WHERE acked_at IS NULL`，并发接手只有一个成功，另一个拿到 409。
- **限额**：单字段 16 KiB，数组 64 项 × 4 KiB，整条 64 KiB。超过就拆成多次交接，别把日志塞进来。
- **数据库**：SQLite WAL，`busy_timeout=5000`，不要放在 NFS 上。

## 开发

```bash
make test    # go test ./...
make build   # bin/handoff
make release # dist/ 五个平台 + checksums.txt
```

目录：

```
cmd/handoff          入口
internal/repo        远程 URL 归一化 + 从 git 探测
internal/store       SQLite（projects / handoffs / tokens）
internal/service     校验、默认值、业务语义
internal/mcpserver   四个 MCP 工具
internal/httpapi     远程 MCP + REST，以及 CLI 的 HTTP 客户端
internal/cli         命令行
skills/handoff       交接/接手 skill
docs/design.md       设计文档
```