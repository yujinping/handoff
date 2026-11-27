// Package service 承载 handoff 的业务规则：归一化项目键、校验字段、编排存储。
// MCP、HTTP、CLI 三种入口都必须经过这一层。
package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/yujinping/handoff/internal/repo"
	"github.com/yujinping/handoff/internal/store"
)

const (
	// MaxFieldBytes 限制单个正文字段（goal/state）。
	MaxFieldBytes = 16 * 1024
	// MaxItems 限制每个数组字段的条目数。
	MaxItems = 64
	// MaxItemBytes 限制数组单项长度。
	MaxItemBytes = 4 * 1024
	// MaxTotalBytes 限制整条交接的大小。
	MaxTotalBytes = 64 * 1024
	// MaxNameLen 限制 workstream/client/session_id 的长度。
	MaxNameLen = 128
)

var namePattern = regexp.MustCompile(`^[a-z0-9._@-]+$`)

// Service 是 handoff 的唯一业务入口。
type Service struct {
	st  *store.Store
	now func() time.Time
}

// New 构造 Service。
func New(st *store.Store) *Service {
	return &Service{st: st, now: func() time.Time { return time.Now().UTC() }}
}

// SaveInput 是保存一次交接所需的全部字段。
type SaveInput struct {
	Repo          string   `json:"repo"`
	Workstream    string   `json:"workstream,omitempty"`
	Client        string   `json:"client,omitempty"`
	SessionID     string   `json:"session_id,omitempty"`
	Goal          string   `json:"goal"`
	State         string   `json:"state"`
	NextSteps     []string `json:"next_steps,omitempty"`
	Files         []string `json:"files,omitempty"`
	Decisions     []string `json:"decisions,omitempty"`
	OpenQuestions []string `json:"open_questions,omitempty"`
}

// ValidationError 表示入参不合法，调用方应原样展示给用户或模型。
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return e.Field + ": " + e.Message
}

// NotFoundError 表示没有可接手的交接。
type NotFoundError struct{ Message string }

func (e *NotFoundError) Error() string { return e.Message }
func (e *NotFoundError) Unwrap() error { return store.ErrNotFound }

// ConflictError 表示交接已被其他人确认。
type ConflictError struct{ Message string }

func (e *ConflictError) Error() string { return e.Message }
func (e *ConflictError) Unwrap() error { return store.ErrConflict }

// Save 追加一条交接快照。
func (s *Service) Save(ctx context.Context, in SaveInput) (*store.Handoff, error) {
	key, err := normalizeRepo(in.Repo)
	if err != nil {
		return nil, err
	}
	if err := validateText("goal", in.Goal, MaxFieldBytes, true); err != nil {
		return nil, err
	}
	if err := validateText("state", in.State, MaxFieldBytes, true); err != nil {
		return nil, err
	}
	for _, arr := range []struct {
		name  string
		items []string
	}{
		{"next_steps", in.NextSteps},
		{"files", in.Files},
		{"decisions", in.Decisions},
		{"open_questions", in.OpenQuestions},
	} {
		if err := validateItems(arr.name, arr.items); err != nil {
			return nil, err
		}
	}

	client, err := normalizeName("client", in.Client)
	if err != nil {
		return nil, err
	}
	if client == "" {
		client = "unknown"
	}
	workstream := in.Workstream
	if strings.TrimSpace(workstream) == "" {
		workstream = "current@" + client
	} else {
		workstream, err = normalizeName("workstream", workstream)
		if err != nil {
			return nil, err
		}
	}
	sessionID := strings.TrimSpace(in.SessionID)
	if len(sessionID) > MaxNameLen {
		return nil, &ValidationError{Field: "session_id", Message: fmt.Sprintf("不能超过 %d 字符", MaxNameLen)}
	}

	total := len(in.Goal) + len(in.State) + len(sessionID) + len(workstream) + len(client)
	for _, items := range [][]string{in.NextSteps, in.Files, in.Decisions, in.OpenQuestions} {
		for _, item := range items {
			total += len(item)
		}
	}
	if total > MaxTotalBytes {
		return nil, &ValidationError{Field: "handoff", Message: fmt.Sprintf("整条交接不能超过 %d 字节", MaxTotalBytes)}
	}

	now := s.now()
	project, err := s.st.EnsureProject(ctx, key, repo.DisplayName(key), now)
	if err != nil {
		return nil, err
	}
	h, err := s.st.SaveHandoff(ctx, &store.Handoff{
		ProjectID:     project.ID,
		Workstream:    workstream,
		Goal:          strings.TrimSpace(in.Goal),
		State:         strings.TrimSpace(in.State),
		NextSteps:     nonNil(in.NextSteps),
		Files:         nonNil(in.Files),
		Decisions:     nonNil(in.Decisions),
		OpenQuestions: nonNil(in.OpenQuestions),
		Client:        client,
		SessionID:     sessionID,
		CreatedAt:     now,
	})
	if err != nil {
		return nil, err
	}
	h.RepoKey = project.RemoteKey
	return h, nil
}

// List 列出待接手的交接。
func (s *Service) List(ctx context.Context, rawRepo string, limit int) ([]store.Summary, error) {
	project, err := s.project(ctx, rawRepo)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	out, err := s.st.ListPending(ctx, project.ID, limit)
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []store.Summary{}
	}
	for i := range out {
		out[i].RepoKey = project.RemoteKey
	}
	return out, nil
}

// Load 读取最新交接。workstream 为空时返回项目内最新未确认的一条。
func (s *Service) Load(ctx context.Context, rawRepo, workstream string) (*store.Handoff, error) {
	project, err := s.project(ctx, rawRepo)
	if err != nil {
		return nil, err
	}
	ws, err := normalizeName("workstream", workstream)
	if err != nil {
		return nil, err
	}
	h, err := s.st.LatestHandoff(ctx, project.ID, ws)
	if errors.Is(err, store.ErrNotFound) {
		if ws == "" {
			return nil, &NotFoundError{Message: fmt.Sprintf("no handoff pending for %s", project.RemoteKey)}
		}
		return nil, &NotFoundError{Message: fmt.Sprintf("no handoff for %s on workstream %q", project.RemoteKey, ws)}
	}
	if err != nil {
		return nil, err
	}
	h.RepoKey = project.RemoteKey
	return h, nil
}

// Ack 确认已接手。
func (s *Service) Ack(ctx context.Context, rawRepo, workstream, by string) (*store.Handoff, error) {
	project, err := s.project(ctx, rawRepo)
	if err != nil {
		return nil, err
	}
	ws, err := normalizeName("workstream", workstream)
	if err != nil {
		return nil, err
	}
	by = strings.TrimSpace(by)
	if by == "" {
		by = "unknown"
	}
	if len(by) > MaxNameLen {
		return nil, &ValidationError{Field: "by", Message: fmt.Sprintf("不能超过 %d 字符", MaxNameLen)}
	}
	h, err := s.st.AckHandoff(ctx, project.ID, ws, by, s.now())
	if errors.Is(err, store.ErrNotFound) {
		return nil, &NotFoundError{Message: fmt.Sprintf("no handoff pending for %s", project.RemoteKey)}
	}
	if errors.Is(err, store.ErrConflict) {
		return nil, &ConflictError{Message: "handoff was already acknowledged"}
	}
	if err != nil {
		return nil, err
	}
	h.RepoKey = project.RemoteKey
	return h, nil
}

// Project 返回归一化后的项目键与记录，供调用方复用。
func (s *Service) Project(ctx context.Context, rawRepo string) (*store.Project, error) {
	return s.project(ctx, rawRepo)
}

func (s *Service) project(ctx context.Context, rawRepo string) (*store.Project, error) {
	key, err := normalizeRepo(rawRepo)
	if err != nil {
		return nil, err
	}
	p, err := s.st.ProjectByKey(ctx, key)
	if errors.Is(err, store.ErrNotFound) {
		return nil, &NotFoundError{Message: fmt.Sprintf("no handoffs recorded for %s", key)}
	}
	return p, err
}

func normalizeRepo(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", &ValidationError{Field: "repo", Message: "必填；请传项目的 origin 远程地址，例如 git@github.com:owner/repo.git"}
	}
	key, err := repo.Normalize(raw)
	if err != nil {
		return "", &ValidationError{Field: "repo", Message: err.Error()}
	}
	return key, nil
}

func normalizeName(field, v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return "", nil
	}
	if len(v) > MaxNameLen {
		return "", &ValidationError{Field: field, Message: fmt.Sprintf("不能超过 %d 字符", MaxNameLen)}
	}
	if !namePattern.MatchString(v) {
		return "", &ValidationError{Field: field, Message: "只允许字母、数字和 . _ @ -"}
	}
	return v, nil
}

func validateText(field, v string, max int, required bool) error {
	if strings.TrimSpace(v) == "" {
		if required {
			return &ValidationError{Field: field, Message: "不能为空"}
		}
		return nil
	}
	if len(v) > max {
		return &ValidationError{Field: field, Message: fmt.Sprintf("不能超过 %d 字节", max)}
	}
	return nil
}

func validateItems(field string, items []string) error {
	if len(items) > MaxItems {
		return &ValidationError{Field: field, Message: fmt.Sprintf("最多 %d 项", MaxItems)}
	}
	for i, item := range items {
		if strings.TrimSpace(item) == "" {
			return &ValidationError{Field: fmt.Sprintf("%s[%d]", field, i), Message: "不能是空字符串"}
		}
		if len(item) > MaxItemBytes {
			return &ValidationError{Field: fmt.Sprintf("%s[%d]", field, i), Message: fmt.Sprintf("不能超过 %d 字节", MaxItemBytes)}
		}
	}
	return nil
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
