// Package httpapi 提供远程形态：MCP Streamable HTTP + 给 CLI 用的 REST。
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/yujinping/handoff/internal/mcpserver"
	"github.com/yujinping/handoff/internal/repo"
	"github.com/yujinping/handoff/internal/service"
	"github.com/yujinping/handoff/internal/store"
	"github.com/yujinping/handoff/internal/wire"
)

type tokenCtxKey struct{}

// Server 组合 REST 与 MCP 路由，两者共享同一个 service。
type Server struct {
	svc *service.Service
	st  *store.Store
	mcp *mcp.Server
}

// New 构造 HTTP 层。
func New(svc *service.Service, st *store.Store) *Server {
	return &Server{svc: svc, st: st, mcp: mcpserver.New(svc)}
}

// Handler 返回完整路由。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// MCP：Streamable HTTP。传输层自己处理 GET/POST/DELETE。
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s.mcp }, nil)
	mux.Handle("/mcp", s.requireAuth(mcpHandler))

	mux.HandleFunc("POST /api/v1/handoffs", s.requireAuthFunc(s.save))
	mux.HandleFunc("GET /api/v1/handoffs", s.requireAuthFunc(s.list))
	mux.HandleFunc("GET /api/v1/handoffs/latest", s.requireAuthFunc(s.load))
	mux.HandleFunc("POST /api/v1/handoffs/ack", s.requireAuthFunc(s.ack))

	return mux
}

func (s *Server) save(w http.ResponseWriter, r *http.Request) {
	var in wire.SaveRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	tok := tokenFrom(r.Context())
	if !scopeAllows(tok, in.Repo, w) {
		return
	}
	h, err := s.svc.Save(r.Context(), in)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, wire.FromStore(h.RepoKey, h))
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	rawRepo := r.URL.Query().Get("repo")
	if rawRepo == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "repo 参数必填")
		return
	}
	if !scopeAllows(tokenFrom(r.Context()), rawRepo, w) {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.svc.List(r.Context(), rawRepo, limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]wire.Summary, 0, len(items))
	for _, it := range items {
		out = append(out, wire.SummaryFromStore(it.RepoKey, it))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) load(w http.ResponseWriter, r *http.Request) {
	rawRepo := r.URL.Query().Get("repo")
	if rawRepo == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "repo 参数必填")
		return
	}
	if !scopeAllows(tokenFrom(r.Context()), rawRepo, w) {
		return
	}
	h, err := s.svc.Load(r.Context(), rawRepo, r.URL.Query().Get("workstream"))
	if err != nil {
		writeErr(w, err)
		return
	}
	view := wire.FromStore(h.RepoKey, h)
	writeJSON(w, http.StatusOK, wire.LoadResponse{Found: true, Repo: h.RepoKey, Handoff: &view})
}

func (s *Server) ack(w http.ResponseWriter, r *http.Request) {
	var in wire.AckRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	if !scopeAllows(tokenFrom(r.Context()), in.Repo, w) {
		return
	}
	h, err := s.svc.Ack(r.Context(), in.Repo, in.Workstream, in.By)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, wire.FromStore(h.RepoKey, h))
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := s.authenticate(w, r)
		if !ok {
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tokenCtxKey{}, tok)))
	})
}

func (s *Server) requireAuthFunc(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, ok := s.authenticate(w, r)
		if !ok {
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), tokenCtxKey{}, tok)))
	}
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (*store.Token, bool) {
	header := r.Header.Get("Authorization")
	raw, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || strings.TrimSpace(raw) == "" {
		w.Header().Set("WWW-Authenticate", `Bearer realm="handoff"`)
		writeError(w, http.StatusUnauthorized, "unauthorized", "缺少 Bearer token")
		return nil, false
	}
	tok, err := s.st.Authenticate(r.Context(), strings.TrimSpace(raw), time.Now())
	if err != nil {
		if errors.Is(err, store.ErrInvalidToken) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="handoff", error="invalid_token"`)
			writeError(w, http.StatusUnauthorized, "unauthorized", "token 无效或已吊销")
			return nil, false
		}
		log.Printf("handoff: 校验 token: %v", err)
		writeError(w, http.StatusInternalServerError, "internal", "校验 token 失败")
		return nil, false
	}
	return tok, true
}

func tokenFrom(ctx context.Context) *store.Token {
	tok, _ := ctx.Value(tokenCtxKey{}).(*store.Token)
	return tok
}

// scopeAllows 检查 token 是否允许访问该项目。空 scope 表示所有项目。
// 注意：v0.1 只在 REST 上执行 scope 校验，MCP 传输层拿不到工具参数。
func scopeAllows(tok *store.Token, rawRepo string, w http.ResponseWriter) bool {
	if tok == nil || tok.ScopeProject == "" {
		return true
	}
	key, err := repo.Normalize(rawRepo)
	if err != nil || key != tok.ScopeProject {
		writeError(w, http.StatusForbidden, "forbidden", "token 无权访问该项目")
		return false
	}
	return true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 256<<10))
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求体不是合法 JSON: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, wire.ErrorBody{Error: wire.ErrorDetail{Code: code, Message: message}})
}

func writeErr(w http.ResponseWriter, err error) {
	var ve *service.ValidationError
	var nf *service.NotFoundError
	var ce *service.ConflictError
	switch {
	case errors.As(err, &ve):
		writeError(w, http.StatusBadRequest, "invalid_request", ve.Error())
	case errors.As(err, &nf):
		writeError(w, http.StatusNotFound, "not_found", nf.Error())
	case errors.As(err, &ce):
		writeError(w, http.StatusConflict, "conflict", ce.Error())
	default:
		log.Printf("handoff: 服务器错误: %v", err)
		writeError(w, http.StatusInternalServerError, "internal", "服务器内部错误")
	}
}
