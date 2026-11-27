package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yujinping/handoff/internal/service"
	"github.com/yujinping/handoff/internal/store"
	"github.com/yujinping/handoff/internal/wire"
)

// Client 是 CLI 远程模式使用的瘦客户端，接口与 service.Service 对齐。
type Client struct {
	base  string
	token string
	hc    *http.Client
}

// NewClient 构造远程客户端。base 例如 http://127.0.0.1:8787。
func NewClient(base, token string) *Client {
	return &Client{
		base:  strings.TrimRight(base, "/"),
		token: token,
		hc:    &http.Client{Timeout: 15 * time.Second},
	}
}

// Save 对应 POST /api/v1/handoffs。
func (c *Client) Save(ctx context.Context, in service.SaveInput) (*store.Handoff, error) {
	var out wire.Handoff
	if err := c.do(ctx, http.MethodPost, "/api/v1/handoffs", in, &out); err != nil {
		return nil, err
	}
	return out.ToStore(), nil
}

// List 对应 GET /api/v1/handoffs。
func (c *Client) List(ctx context.Context, rawRepo string, limit int) ([]store.Summary, error) {
	q := url.Values{"repo": {rawRepo}}
	if limit > 0 {
		q.Set("limit", fmt.Sprint(limit))
	}
	var out []wire.Summary
	if err := c.do(ctx, http.MethodGet, "/api/v1/handoffs?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	items := make([]store.Summary, 0, len(out))
	for _, it := range out {
		items = append(items, store.Summary{
			RepoKey:    it.Repo,
			Workstream: it.Workstream,
			Revision:   it.Revision,
			Goal:       it.Goal,
			State:      it.State,
			Client:     it.Client,
			SessionID:  it.SessionID,
			CreatedAt:  it.CreatedAt,
		})
	}
	return items, nil
}

// Load 对应 GET /api/v1/handoffs/latest。
func (c *Client) Load(ctx context.Context, rawRepo, workstream string) (*store.Handoff, error) {
	q := url.Values{"repo": {rawRepo}}
	if workstream != "" {
		q.Set("workstream", workstream)
	}
	var out wire.LoadResponse
	if err := c.do(ctx, http.MethodGet, "/api/v1/handoffs/latest?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	if !out.Found || out.Handoff == nil {
		return nil, &service.NotFoundError{Message: out.Message}
	}
	return out.Handoff.ToStore(), nil
}

// Ack 对应 POST /api/v1/handoffs/ack。
func (c *Client) Ack(ctx context.Context, rawRepo, workstream, by string) (*store.Handoff, error) {
	var out wire.Handoff
	body := wire.AckRequest{Repo: rawRepo, Workstream: workstream, By: by}
	if err := c.do(ctx, http.MethodPost, "/api/v1/handoffs/ack", body, &out); err != nil {
		return nil, err
	}
	return out.ToStore(), nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("连接 %s 失败: %w", c.base, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var eb wire.ErrorBody
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		_ = json.Unmarshal(raw, &eb)
		msg := eb.Error.Message
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		if msg == "" {
			msg = resp.Status
		}
		switch resp.StatusCode {
		case http.StatusBadRequest:
			return &service.ValidationError{Message: msg}
		case http.StatusNotFound:
			return &service.NotFoundError{Message: msg}
		case http.StatusConflict:
			return &service.ConflictError{Message: msg}
		case http.StatusUnauthorized, http.StatusForbidden:
			return fmt.Errorf("认证失败（%d）: %s", resp.StatusCode, msg)
		default:
			return fmt.Errorf("服务端错误（%d）: %s", resp.StatusCode, msg)
		}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
