package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/yujinping/handoff/internal/service"
	"github.com/yujinping/handoff/internal/store"
	"github.com/yujinping/handoff/internal/wire"
)

func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res.IsError {
		if len(res.Content) > 0 {
			if tc, ok := res.Content[0].(*mcp.TextContent); ok {
				t.Fatalf("tool error: %s", tc.Text)
			}
		}
		t.Fatal("tool error")
	}
	if len(res.Content) == 0 {
		t.Fatal("empty tool result")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content type = %T", res.Content[0])
	}
	return tc.Text
}

func TestMCPToolFlow(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "handoff.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	srv := New(service.New(st))
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	go func() { _ = srv.Run(ctx, serverTransport) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	repo := "git@github.com:Acme/Widgets.git"
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "handoff_save", Arguments: map[string]any{
		"repo": repo, "client": "codex", "goal": "重写 handoff", "state": "骨架完成",
		"next_steps": []string{"写测试"},
	}})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	var saved wire.Handoff
	if err := json.Unmarshal([]byte(textOf(t, res)), &saved); err != nil {
		t.Fatalf("decode save: %v", err)
	}
	if saved.Revision != 1 || saved.Repo != "github.com/acme/widgets" || saved.Workstream != "current@codex" {
		t.Fatalf("unexpected save: %+v", saved)
	}

	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "handoff_list", Arguments: map[string]any{"repo": repo}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var items []wire.Summary
	if err := json.Unmarshal([]byte(textOf(t, res)), &items); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(items) != 1 || items[0].Workstream != "current@codex" {
		t.Fatalf("unexpected list: %+v", items)
	}

	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "handoff_load", Arguments: map[string]any{"repo": repo, "workstream": "current@codex"}})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	var loaded wire.LoadResponse
	if err := json.Unmarshal([]byte(textOf(t, res)), &loaded); err != nil {
		t.Fatalf("decode load: %v", err)
	}
	if !loaded.Found || loaded.Handoff == nil || loaded.Handoff.State != "骨架完成" {
		t.Fatalf("unexpected load: %+v", loaded)
	}

	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "handoff_load", Arguments: map[string]any{"repo": "github.com/nobody/nothing"}})
	if err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if err := json.Unmarshal([]byte(textOf(t, res)), &loaded); err != nil {
		t.Fatalf("decode missing: %v", err)
	}
	if loaded.Found {
		t.Fatalf("missing repo should not be found: %+v", loaded)
	}

	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "handoff_ack", Arguments: map[string]any{
		"repo": repo, "workstream": "current@codex", "by": "claude",
	}})
	if err != nil {
		t.Fatalf("ack: %v", err)
	}
	var acked wire.Handoff
	if err := json.Unmarshal([]byte(textOf(t, res)), &acked); err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if acked.AckedAt == nil || acked.AckedBy != "claude" {
		t.Fatalf("unexpected ack: %+v", acked)
	}

	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "handoff_ack", Arguments: map[string]any{
		"repo": repo, "workstream": "current@codex", "by": "claude",
	}})
	if err != nil {
		t.Fatalf("second ack: %v", err)
	}
	if !res.IsError {
		t.Fatal("二次 ack 应当报冲突")
	}

	// 校验错误也应以 tool error 返回，而不是 panic。
	res, err = session.CallTool(ctx, &mcp.CallToolParams{Name: "handoff_save", Arguments: map[string]any{
		"repo": repo, "goal": "", "state": "",
	}})
	if err != nil {
		t.Fatalf("invalid save: %v", err)
	}
	if !res.IsError {
		t.Fatal("空 goal/state 应当报校验错误")
	}
}
