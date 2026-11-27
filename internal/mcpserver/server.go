// Package mcpserver 把 service 暴露成 MCP 工具。stdio 与 HTTP 共用同一个 Server。
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/yujinping/handoff/internal/buildinfo"
	"github.com/yujinping/handoff/internal/repo"
	"github.com/yujinping/handoff/internal/service"
	"github.com/yujinping/handoff/internal/wire"
)

// New 构造带四个 handoff 工具的 MCP server。
func New(svc *service.Service) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "handoff",
		Version: buildinfo.Version,
	}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name: "handoff_save",
		Description: "保存当前会话的交接现场，供另一个 AI 会话或另一台设备接手。" +
			"再次保存会追加新的 revision，不会覆盖历史。repo 传项目的 origin 远程地址。",
	}, handleSave(svc))

	mcp.AddTool(s, &mcp.Tool{
		Name: "handoff_list",
		Description: "列出某个项目下所有待接手的交接。每个工作流只显示最新一条，" +
			"已确认的不会出现。开始接手前先调用它。",
	}, handleList(svc))

	mcp.AddTool(s, &mcp.Tool{
		Name: "handoff_load",
		Description: "读取最新交接的完整内容。不传 workstream 时返回该项目最新未确认的一条；" +
			"没有可接手的交接时返回 found=false。",
	}, handleLoad(svc))

	mcp.AddTool(s, &mcp.Tool{
		Name: "handoff_ack",
		Description: "确认已接手某条交接，避免重复交接。不传 workstream 时确认最新未确认的一条。" +
			"如果已被其他会话确认，会返回冲突错误。",
	}, handleAck(svc))

	return s
}

type saveArgs struct {
	Repo          string   `json:"repo" jsonschema:"项目的 origin 远程地址，例如 git@github.com:owner/repo.git"`
	Goal          string   `json:"goal" jsonschema:"这次会话要达成的目标"`
	State         string   `json:"state" jsonschema:"当前进展、已完成与卡点"`
	NextSteps     []string `json:"next_steps,omitempty" jsonschema:"下一步要做什么，按顺序"`
	Files         []string `json:"files,omitempty" jsonschema:"相关文件路径，带简短说明更好"`
	Decisions     []string `json:"decisions,omitempty" jsonschema:"已做出的决定及原因"`
	OpenQuestions []string `json:"open_questions,omitempty" jsonschema:"还没解决的问题"`
	Workstream    string   `json:"workstream,omitempty" jsonschema:"工作流名称，默认 current@<client>"`
	Client        string   `json:"client,omitempty" jsonschema:"当前 AI 客户端名称，例如 claude-code"`
	SessionID     string   `json:"session_id,omitempty" jsonschema:"当前会话 id，可选"`
}

func handleSave(svc *service.Service) mcp.ToolHandlerFor[saveArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in saveArgs) (*mcp.CallToolResult, any, error) {
		h, err := svc.Save(ctx, wire.SaveRequest{
			Repo:          in.Repo,
			Workstream:    in.Workstream,
			Client:        in.Client,
			SessionID:     in.SessionID,
			Goal:          in.Goal,
			State:         in.State,
			NextSteps:     in.NextSteps,
			Files:         in.Files,
			Decisions:     in.Decisions,
			OpenQuestions: in.OpenQuestions,
		})
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(wire.FromStore(h.RepoKey, h))
	}
}

type listArgs struct {
	Repo  string `json:"repo" jsonschema:"项目的 origin 远程地址"`
	Limit int    `json:"limit,omitempty" jsonschema:"最多返回几条，默认 20，上限 100"`
}

func handleList(svc *service.Service) mcp.ToolHandlerFor[listArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in listArgs) (*mcp.CallToolResult, any, error) {
		items, err := svc.List(ctx, in.Repo, in.Limit)
		if err != nil {
			return nil, nil, err
		}
		out := make([]wire.Summary, 0, len(items))
		for _, it := range items {
			out = append(out, wire.SummaryFromStore(it.RepoKey, it))
		}
		return jsonResult(out)
	}
}

type loadArgs struct {
	Repo       string `json:"repo" jsonschema:"项目的 origin 远程地址"`
	Workstream string `json:"workstream,omitempty" jsonschema:"工作流名称，不传则取最新未确认的一条"`
}

func handleLoad(svc *service.Service) mcp.ToolHandlerFor[loadArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in loadArgs) (*mcp.CallToolResult, any, error) {
		h, err := svc.Load(ctx, in.Repo, in.Workstream)
		if err != nil {
			var nf *service.NotFoundError
			if errors.As(err, &nf) {
				key, _ := repo.Normalize(in.Repo)
				return jsonResult(wire.LoadResponse{Found: false, Repo: key, Message: nf.Error()})
			}
			return nil, nil, err
		}
		view := wire.FromStore(h.RepoKey, h)
		return jsonResult(wire.LoadResponse{Found: true, Repo: h.RepoKey, Handoff: &view})
	}
}

type ackArgs struct {
	Repo       string `json:"repo" jsonschema:"项目的 origin 远程地址"`
	Workstream string `json:"workstream,omitempty" jsonschema:"工作流名称，不传则确认最新未确认的一条"`
	By         string `json:"by,omitempty" jsonschema:"接手方名称，便于追溯"`
}

func handleAck(svc *service.Service) mcp.ToolHandlerFor[ackArgs, any] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in ackArgs) (*mcp.CallToolResult, any, error) {
		h, err := svc.Ack(ctx, in.Repo, in.Workstream, in.By)
		if err != nil {
			return nil, nil, err
		}
		return jsonResult(wire.FromStore(h.RepoKey, h))
	}
}

func jsonResult(v any) (*mcp.CallToolResult, any, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(b)}},
	}, nil, nil
}
