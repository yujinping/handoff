# 自托管部署（示例：handoff.example.com）

一套跑通过的部署：单台 Linux 主机，binary + systemd + Caddy。下文用
`handoff.example.com` 代指你的域名、`root@example.com` 代指你的主机，替换后即可照抄。

## 拓扑

```
客户端 --https--> Caddy (handoff.example.com, 443)
                    └─ /mcp* /api/* /healthz --> handoff serve (127.0.0.1:8787)
                                                   └─ /var/lib/handoff/handoff.db
```

- 只有 Caddy 暴露 443；`handoff serve` 只听回环，不对公网开口。
- 数据库只有一份，在服务器上；客户端不保存副本，全部走 REST / MCP。
- 路径白名单之外的请求由 Caddy 直接 404，探测不到 `/`。

## 主机

| 项 | 值 |
| --- | --- |
| 主机 | `example.com`（root，Ubuntu 24.04，x86_64） |
| 二进制 | `/usr/local/bin/handoff` |
| 数据库 | `/var/lib/handoff/handoff.db` |
| 单元 | `/etc/systemd/system/handoff.service` |
| 反代 | `/etc/caddy/Caddyfile` 中 `handoff.example.com` 站点 |
| 日志 | `journalctl -u handoff`、`/var/log/caddy/handoff.log` |

## 构建与发布

在开发机（仓库根）交叉编译，不依赖目标机工具链：

```bash
VERSION=$(git describe --tags --always | sed 's/^v//')
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w \
    -X github.com/yujinping/handoff/internal/buildinfo.Version=$VERSION \
    -X github.com/yujinping/handoff/internal/buildinfo.Commit=$(git rev-parse --short HEAD) \
    -X github.com/yujinping/handoff/internal/buildinfo.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o dist/handoff_${VERSION}_linux_amd64 ./cmd/handoff

scp dist/handoff_${VERSION}_linux_amd64 root@example.com:/tmp/handoff-new
ssh root@example.com 'install -m 0755 /tmp/handoff-new /usr/local/bin/handoff \
  && rm -f /tmp/handoff-new && systemctl restart handoff.service'
```

## systemd 单元

```ini
[Unit]
Description=handoff 远程交接服务（MCP + REST）
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=HANDOFF_DB=/var/lib/handoff/handoff.db
WorkingDirectory=/var/lib/handoff
ExecStart=/usr/local/bin/handoff serve --addr 127.0.0.1:8787
Restart=always
RestartSec=3
KillSignal=SIGTERM
TimeoutStopSec=20
StandardOutput=journal
StandardError=journal
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

数据库目录与库文件先建好（`init` 只建表，不重复执行也无害）：

```bash
install -d -m 0700 /var/lib/handoff
HANDOFF_DB=/var/lib/handoff/handoff.db handoff init
```

`HANDOFF_DB` 必须显式写：systemd 的系统级服务没有 `HOME`，`serve` 会退回到
`./handoff.db`，在 `WorkingDirectory` 是 `/` 时于根目录建一个空库，然后报
「没有可用的 token」。`WorkingDirectory` 一并固定，避免相对路径乱落。

**默认库是 `~/.handoff/handoff.db`，`HANDOFF_DB`（或 `--db`）只是覆盖它**，两者不需要
一致，但必须「处处一致」：`serve` 读哪个库，后面 `token add` / `token list` 就得指向
同一个库。否则 token 写进了默认库、服务却盯着 `/var/lib/handoff`，表现为新 token 立刻
401 或启动报「没有可用的 token」。

## Caddy 站点

```caddyfile
handoff.example.com {
	handle /mcp* {
		reverse_proxy 127.0.0.1:8787 {
			transport http {
				read_timeout 0
				write_timeout 0
			}
		}
	}
	handle /api/* {
		reverse_proxy 127.0.0.1:8787
	}
	handle /healthz {
		reverse_proxy 127.0.0.1:8787
	}
	handle {
		respond 404
	}
	log {
		output file /var/log/caddy/handoff.log
	}
}
```

- MCP 的 Streamable HTTP 会走 SSE 长连接，`read_timeout 0 / write_timeout 0` 不能省。
- 改完先 `caddy validate --config /etc/caddy/Caddyfile`，再 `systemctl reload caddy`。
- **validate 以 root 运行，会顺手创建 `handoff.log`（root:root 600），reload 时 Caddy
  以 `caddy` 用户打不开它、整份新配置被拒**。改完执行一次
  `chown caddy:caddy /var/log/caddy/handoff.log`。reload 失败时旧配置仍在跑，
  站点表现为 TLS 告警 `internal error`（还没拿到该域名的证书），不要误判成域名解析问题。

## token

```bash
# 与单元里的 HANDOFF_DB 指向同一个库
ssh root@example.com 'HANDOFF_DB=/var/lib/handoff/handoff.db handoff token add --name <设备名>'   # 明文只打印一次
ssh root@example.com 'HANDOFF_DB=/var/lib/handoff/handoff.db handoff token list'
ssh root@example.com 'HANDOFF_DB=/var/lib/handoff/handoff.db handoff token revoke <8位id>'
```

前缀 `hd_`。改前缀或吊销后，客户端立刻 401，需要重新下发。

客户端：

```bash
export HANDOFF_SERVER=https://handoff.example.com
export HANDOFF_TOKEN=hd_...
handoff list
```

远程 MCP 客户端填 `https://handoff.example.com/mcp`，带 `Authorization: Bearer hd_...`。

## 验收

```bash
curl -sS https://handoff.example.com/healthz            # {"status":"ok"}
curl -sS -o /dev/null -w '%{http_code}\n' https://handoff.example.com/            # 404
curl -sS -o /dev/null -w '%{http_code}\n' https://handoff.example.com/api/v1/handoffs?repo=x  # 401

curl -sS -H "Authorization: Bearer $HANDOFF_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -X POST https://handoff.example.com/mcp \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}'
```

最后一条必须返回带 `serverInfo` 的 SSE 事件。若返回 `403 invalid Host header`，
说明二进制是旧版：MCP SDK 默认把「回环连接 + 公网 Host」当成 DNS rebinding 攻击，
`internal/httpapi` 里已用 `DisableLocalhostProtection` 关掉，鉴权由 Bearer 主要负责。