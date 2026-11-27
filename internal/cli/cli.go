// Package cli 实现 handoff 的命令行入口。
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/yujinping/handoff/internal/buildinfo"
	"github.com/yujinping/handoff/internal/config"
	"github.com/yujinping/handoff/internal/httpapi"
	"github.com/yujinping/handoff/internal/mcpserver"
	gitrepo "github.com/yujinping/handoff/internal/repo"
	"github.com/yujinping/handoff/internal/service"
	"github.com/yujinping/handoff/internal/store"
	"github.com/yujinping/handoff/internal/wire"
)

const usage = `handoff —— 按远程 git 仓库交接 AI 会话现场

用法:
  handoff init                         创建数据库
  handoff mcp                          本地 stdio MCP 服务（给 AI 客户端）
  handoff serve [--addr 127.0.0.1:8787] 远程 HTTP 服务（MCP + REST）
  handoff save --repo R [选项] < handoff.json   保存交接
  handoff list --repo R [--limit N] [--json]    列出待接手
  handoff load --repo R [--workstream W]        读取最新交接
  handoff ack  --repo R [--workstream W]        确认已接手
  handoff token add --name N [--scope R]        创建远程 token
  handoff token list / revoke ID                管理 token
  handoff version

全局选项:
  --db PATH        数据库路径（默认 ~/.handoff/handoff.db，环境变量 HANDOFF_DB）
  --server URL     远程服务地址（环境变量 HANDOFF_SERVER）；设置后 save/list/load/ack 走 REST
  --token TOKEN    远程 token（环境变量 HANDOFF_TOKEN）

退出码: 0 成功，1 业务错误，2 用法错误
`

// Main 是命令入口，返回进程退出码。
func Main(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return 2
	}
	switch args[0] {
	case "init":
		return cmdInit(args[1:])
	case "mcp":
		return cmdMCP(args[1:])
	case "serve":
		return cmdServe(args[1:])
	case "save":
		return cmdSave(args[1:])
	case "list":
		return cmdList(args[1:])
	case "load":
		return cmdLoad(args[1:])
	case "ack":
		return cmdAck(args[1:])
	case "token":
		return cmdToken(args[1:])
	case "version", "--version", "-v":
		fmt.Printf("handoff %s (commit %s, built %s)\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(os.Stdout, usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "handoff: 未知命令 %q\n\n%s", args[0], usage)
		return 2
	}
}

type common struct {
	db     string
	server string
	token  string
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

func (c *common) register(fs *flag.FlagSet) {
	fs.StringVar(&c.db, "db", config.DefaultDB(), "SQLite 数据库路径")
	fs.StringVar(&c.server, "server", config.DefaultServer(), "远程服务地址")
	fs.StringVar(&c.token, "token", config.DefaultToken(), "远程访问 token")
}

type backend interface {
	Save(context.Context, service.SaveInput) (*store.Handoff, error)
	List(context.Context, string, int) ([]store.Summary, error)
	Load(context.Context, string, string) (*store.Handoff, error)
	Ack(context.Context, string, string, string) (*store.Handoff, error)
}

// openBackend 根据是否设置 --server 选择本地直连或远程 REST。
func openBackend(c common) (backend, func(), error) {
	if c.server != "" {
		if c.token == "" {
			return nil, nil, errors.New("远程模式需要 token：用 --token 或 HANDOFF_TOKEN 指定")
		}
		return httpapi.NewClient(c.server, c.token), func() {}, nil
	}
	st, err := store.Open(c.db)
	if err != nil {
		return nil, nil, err
	}
	return service.New(st), func() { _ = st.Close() }, nil
}

func cmdInit(args []string) int {
	var c common
	fs := newFlagSet("init")
	c.register(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	st, err := store.Open(c.db)
	if err != nil {
		return fail("%v", err)
	}
	defer st.Close()
	fmt.Printf("数据库: %s\n", c.db)
	return 0
}

func cmdMCP(args []string) int {
	var c common
	fs := newFlagSet("mcp")
	c.register(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	st, err := store.Open(c.db)
	if err != nil {
		return fail("%v", err)
	}
	defer st.Close()

	srv := mcpserver.New(service.New(st))
	if err := srv.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		return fail("MCP 服务退出: %v", err)
	}
	return 0
}

func cmdServe(args []string) int {
	var c common
	var addr string
	fs := newFlagSet("serve")
	c.register(fs)
	fs.StringVar(&addr, "addr", config.DefaultAddr(), "监听地址")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	st, err := store.Open(c.db)
	if err != nil {
		return fail("%v", err)
	}
	defer st.Close()

	n, err := st.ActiveTokenCount(context.Background())
	if err != nil {
		return fail("%v", err)
	}
	if n == 0 {
		return fail("没有可用的 token。先运行：handoff token add --name <设备名>")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{
		Addr:              addr,
		Handler:           httpapi.New(service.New(st), st).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		log.Printf("handoff 监听 http://%s（MCP: /mcp，REST: /api/v1）", addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fail("监听失败: %v", err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}
	return 0
}

func cmdSave(args []string) int {
	var c common
	var repo, workstream, client, session, goal, state string
	var next, files, decisions, open stringList
	fs := newFlagSet("save")
	c.register(fs)
	fs.StringVar(&repo, "repo", "", "项目的 origin 远程地址")
	fs.StringVar(&workstream, "workstream", "", "工作流名称，默认 current@<client>")
	fs.StringVar(&client, "client", "", "AI 客户端名称")
	fs.StringVar(&session, "session", "", "会话 id")
	fs.StringVar(&goal, "goal", "", "目标")
	fs.StringVar(&state, "state", "", "当前进展")
	fs.Var(&next, "next", "下一步（可重复）")
	fs.Var(&files, "file", "相关文件（可重复）")
	fs.Var(&decisions, "decision", "已做决定（可重复）")
	fs.Var(&open, "question", "未决问题（可重复）")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	in := wire.SaveRequest{}
	if data := readStdin(); len(data) > 0 {
		if err := json.Unmarshal(data, &in); err != nil {
			return fail("stdin 不是合法 JSON: %v", err)
		}
	}
	if repo != "" {
		in.Repo = repo
	}
	if workstream != "" {
		in.Workstream = workstream
	}
	if client != "" {
		in.Client = client
	}
	if session != "" {
		in.SessionID = session
	}
	if goal != "" {
		in.Goal = goal
	}
	if state != "" {
		in.State = state
	}
	if len(next) > 0 {
		in.NextSteps = next
	}
	if len(files) > 0 {
		in.Files = files
	}
	if len(decisions) > 0 {
		in.Decisions = decisions
	}
	if len(open) > 0 {
		in.OpenQuestions = open
	}
	if in.Repo == "" {
		key, err := gitrepo.Detect("")
		if err != nil {
			return fail("未指定 --repo，且无法从当前目录推断项目：%v", err)
		}
		in.Repo = key
	}

	b, closeFn, err := openBackend(c)
	if err != nil {
		return fail("%v", err)
	}
	defer closeFn()

	h, err := b.Save(context.Background(), in)
	if err != nil {
		return fail("%v", err)
	}
	return printJSON(wire.FromStore(h.RepoKey, h))
}

func cmdList(args []string) int {
	var c common
	var repo string
	var limit int
	var asJSON bool
	fs := newFlagSet("list")
	c.register(fs)
	fs.StringVar(&repo, "repo", "", "项目的 origin 远程地址")
	fs.IntVar(&limit, "limit", 20, "最多返回几条")
	fs.BoolVar(&asJSON, "json", false, "输出 JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if repo == "" {
		key, err := gitrepo.Detect("")
		if err != nil {
			return fail("未指定 --repo，且无法从当前目录推断项目：%v", err)
		}
		repo = key
	}

	b, closeFn, err := openBackend(c)
	if err != nil {
		return fail("%v", err)
	}
	defer closeFn()

	items, err := b.List(context.Background(), repo, limit)
	if err != nil {
		return fail("%v", err)
	}
	if asJSON {
		out := make([]wire.Summary, 0, len(items))
		for _, it := range items {
			out = append(out, wire.SummaryFromStore(it.RepoKey, it))
		}
		return printJSON(out)
	}
	if len(items) == 0 {
		fmt.Println("没有待接手的交接。")
		return 0
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "WORKSTREAM\tREV\tCLIENT\tUPDATED\tGOAL")
	for _, it := range items {
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\n",
			it.Workstream, it.Revision, dash(it.Client),
			it.CreatedAt.Local().Format("2006-01-02 15:04"), truncate(it.Goal, 60))
	}
	_ = tw.Flush()
	return 0
}

func cmdLoad(args []string) int {
	var c common
	var repo, workstream string
	fs := newFlagSet("load")
	c.register(fs)
	fs.StringVar(&repo, "repo", "", "项目的 origin 远程地址")
	fs.StringVar(&workstream, "workstream", "", "工作流名称，不传取最新未确认的一条")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if repo == "" {
		key, err := gitrepo.Detect("")
		if err != nil {
			return fail("未指定 --repo，且无法从当前目录推断项目：%v", err)
		}
		repo = key
	}

	b, closeFn, err := openBackend(c)
	if err != nil {
		return fail("%v", err)
	}
	defer closeFn()

	h, err := b.Load(context.Background(), repo, workstream)
	if err != nil {
		return fail("%v", err)
	}
	return printJSON(wire.FromStore(h.RepoKey, h))
}

func cmdAck(args []string) int {
	var c common
	var repo, workstream, by string
	fs := newFlagSet("ack")
	c.register(fs)
	fs.StringVar(&repo, "repo", "", "项目的 origin 远程地址")
	fs.StringVar(&workstream, "workstream", "", "工作流名称，不传确认最新未确认的一条")
	fs.StringVar(&by, "by", "", "接手方名称")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if repo == "" {
		key, err := gitrepo.Detect("")
		if err != nil {
			return fail("未指定 --repo，且无法从当前目录推断项目：%v", err)
		}
		repo = key
	}

	b, closeFn, err := openBackend(c)
	if err != nil {
		return fail("%v", err)
	}
	defer closeFn()

	h, err := b.Ack(context.Background(), repo, workstream, by)
	if err != nil {
		return fail("%v", err)
	}
	return printJSON(wire.FromStore(h.RepoKey, h))
}

func cmdToken(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "用法: handoff token add|list|revoke")
		return 2
	}
	switch args[0] {
	case "add":
		return cmdTokenAdd(args[1:])
	case "list":
		return cmdTokenList(args[1:])
	case "revoke":
		return cmdTokenRevoke(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "handoff: 未知 token 子命令 %q\n", args[0])
		return 2
	}
}

func cmdTokenAdd(args []string) int {
	var c common
	var name, scope string
	fs := newFlagSet("token add")
	c.register(fs)
	fs.StringVar(&name, "name", "", "token 名称，例如 macbook")
	fs.StringVar(&scope, "scope", "", "限制到某个项目（归一化后的键，可选）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if name == "" {
		return fail("--name 必填")
	}
	st, err := store.Open(c.db)
	if err != nil {
		return fail("%v", err)
	}
	defer st.Close()

	plain, tok, err := st.CreateToken(context.Background(), name, scope, time.Now().UTC())
	if err != nil {
		return fail("%v", err)
	}
	fmt.Println(plain)
	fmt.Fprintf(os.Stderr, "已创建 token %s（%s）。明文只显示这一次，请立即保存。\n", tok.ID, tok.Name)
	return 0
}

func cmdTokenList(args []string) int {
	var c common
	fs := newFlagSet("token list")
	c.register(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	st, err := store.Open(c.db)
	if err != nil {
		return fail("%v", err)
	}
	defer st.Close()

	tokens, err := st.ListTokens(context.Background())
	if err != nil {
		return fail("%v", err)
	}
	if len(tokens) == 0 {
		fmt.Println("还没有 token。")
		return 0
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tSCOPE\tCREATED\tLAST USED\tSTATUS")
	for _, t := range tokens {
		status := "active"
		if t.RevokedAt != nil {
			status = "revoked"
		}
		last := "-"
		if t.LastUsedAt != nil {
			last = t.LastUsedAt.Local().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			t.ID, t.Name, dash(t.ScopeProject), t.CreatedAt.Local().Format("2006-01-02"), last, status)
	}
	_ = tw.Flush()
	return 0
}

func cmdTokenRevoke(args []string) int {
	var c common
	var id string
	fs := newFlagSet("token revoke")
	c.register(fs)
	fs.StringVar(&id, "id", "", "token id")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if id == "" && fs.NArg() > 0 {
		id = fs.Arg(0)
	}
	if id == "" {
		return fail("用法: handoff token revoke <id>")
	}
	st, err := store.Open(c.db)
	if err != nil {
		return fail("%v", err)
	}
	defer st.Close()

	if err := st.RevokeToken(context.Background(), id, time.Now().UTC()); err != nil {
		return fail("%v", err)
	}
	fmt.Printf("已吊销 token %s\n", id)
	return 0
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ", ") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

func readStdin() []byte {
	stat, err := os.Stdin.Stat()
	if err != nil || stat.Mode()&os.ModeCharDevice != 0 {
		return nil
	}
	data, _ := io.ReadAll(io.LimitReader(os.Stdin, 256<<10))
	return bytes.TrimSpace(data)
}

func printJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fail("输出失败: %v", err)
	}
	return 0
}

func fail(format string, a ...any) int {
	fmt.Fprintf(os.Stderr, "handoff: "+format+"\n", a...)
	return 1
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
