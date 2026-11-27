package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/yujinping/handoff/internal/service"
	"github.com/yujinping/handoff/internal/store"
	"github.com/yujinping/handoff/internal/wire"
)

func newTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "handoff.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	plain, _, err := st.CreateToken(context.Background(), "test", "", time.Now())
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	ts := httptest.NewServer(New(service.New(st), st).Handler())
	t.Cleanup(ts.Close)
	return ts, plain
}

func do(t *testing.T, ts *httptest.Server, method, path, token string, body any) (*http.Response, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, ts.URL+path, r)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, data
}

func TestHealthzNoAuth(t *testing.T) {
	ts, _ := newTestServer(t)
	resp, _ := do(t, ts, http.MethodGet, "/healthz", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestUnauthorized(t *testing.T) {
	ts, _ := newTestServer(t)
	resp, _ := do(t, ts, http.MethodGet, "/api/v1/handoffs?repo=github.com/a/b", "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("无 token status = %d, want 401", resp.StatusCode)
	}
	resp, _ = do(t, ts, http.MethodGet, "/api/v1/handoffs?repo=github.com/a/b", "hdt_bad_bad", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("错 token status = %d, want 401", resp.StatusCode)
	}
}

func TestRESTFlow(t *testing.T) {
	ts, token := newTestServer(t)
	repo := "git@github.com:Acme/Widgets.git"

	resp, data := do(t, ts, http.MethodPost, "/api/v1/handoffs", token, map[string]any{
		"repo": repo, "client": "codex", "goal": "把构建搬到 Go", "state": "骨架完成",
		"next_steps": []string{"写测试"},
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("save status = %d body=%s", resp.StatusCode, data)
	}
	var saved wire.Handoff
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("decode save: %v", err)
	}
	if saved.Revision != 1 || saved.Repo != "github.com/acme/widgets" || saved.Workstream != "current@codex" {
		t.Fatalf("unexpected saved handoff: %+v", saved)
	}

	resp, data = do(t, ts, http.MethodGet, "/api/v1/handoffs?repo="+repo, token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d body=%s", resp.StatusCode, data)
	}
	var items []wire.Summary
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(items) != 1 || items[0].Goal != "把构建搬到 Go" {
		t.Fatalf("unexpected list: %+v", items)
	}

	resp, data = do(t, ts, http.MethodGet, "/api/v1/handoffs/latest?repo="+repo, token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("load status = %d body=%s", resp.StatusCode, data)
	}
	var got wire.LoadResponse
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode load: %v", err)
	}
	if !got.Found || got.Handoff == nil || got.Handoff.Revision != 1 {
		t.Fatalf("unexpected load: %+v", got)
	}

	resp, data = do(t, ts, http.MethodPost, "/api/v1/handoffs/ack", token, map[string]any{"repo": repo, "workstream": "current@codex", "by": "claude"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ack status = %d body=%s", resp.StatusCode, data)
	}
	var acked wire.Handoff
	if err := json.Unmarshal(data, &acked); err != nil {
		t.Fatalf("decode ack: %v", err)
	}
	if acked.AckedAt == nil || acked.AckedBy != "claude" {
		t.Fatalf("unexpected ack: %+v", acked)
	}

	resp, _ = do(t, ts, http.MethodPost, "/api/v1/handoffs/ack", token, map[string]any{"repo": repo, "workstream": "current@codex", "by": "claude"})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("二次 ack status = %d, want 409", resp.StatusCode)
	}

	resp, _ = do(t, ts, http.MethodGet, "/api/v1/handoffs/latest?repo="+repo, token, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("ack 后 load status = %d, want 404", resp.StatusCode)
	}

	resp, data = do(t, ts, http.MethodGet, "/api/v1/handoffs?repo="+repo, token, nil)
	if resp.StatusCode != http.StatusOK || string(bytes.TrimSpace(data)) != "[]" {
		t.Fatalf("ack 后 list = %d %s", resp.StatusCode, data)
	}
}

func TestTokenScope(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "handoff.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	plain, _, err := st.CreateToken(context.Background(), "scoped", "github.com/acme/widgets", time.Now())
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	ts := httptest.NewServer(New(service.New(st), st).Handler())
	t.Cleanup(ts.Close)

	resp, _ := do(t, ts, http.MethodPost, "/api/v1/handoffs", plain, map[string]any{
		"repo": "github.com/other/repo", "goal": "x", "state": "y",
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("越权 status = %d, want 403", resp.StatusCode)
	}

	resp, _ = do(t, ts, http.MethodPost, "/api/v1/handoffs", plain, map[string]any{
		"repo": "https://github.com/Acme/Widgets.git", "goal": "x", "state": "y",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("授权 status = %d, want 201", resp.StatusCode)
	}
}

func TestBadJSON(t *testing.T) {
	ts, token := newTestServer(t)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/handoffs", bytes.NewBufferString("{oops"))
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
