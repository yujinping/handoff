// Package wire 定义 MCP、HTTP、CLI 之间共享的 JSON 传输结构。
package wire

import (
	"time"

	"github.com/yujinping/handoff/internal/service"
	"github.com/yujinping/handoff/internal/store"
)

// SaveRequest 与 MCP 工具 handoff_save 的入参一致。
type SaveRequest = service.SaveInput

// Handoff 是对外暴露的交接全文。
type Handoff struct {
	Repo          string     `json:"repo"`
	Workstream    string     `json:"workstream"`
	Revision      int        `json:"revision"`
	Goal          string     `json:"goal"`
	State         string     `json:"state"`
	NextSteps     []string   `json:"next_steps"`
	Files         []string   `json:"files"`
	Decisions     []string   `json:"decisions"`
	OpenQuestions []string   `json:"open_questions"`
	Client        string     `json:"client"`
	SessionID     string     `json:"session_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	AckedAt       *time.Time `json:"acked_at,omitempty"`
	AckedBy       string     `json:"acked_by,omitempty"`
}

// Summary 是列表项。
type Summary struct {
	Repo       string    `json:"repo"`
	Workstream string    `json:"workstream"`
	Revision   int       `json:"revision"`
	Goal       string    `json:"goal"`
	State      string    `json:"state"`
	Client     string    `json:"client"`
	SessionID  string    `json:"session_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// LoadResponse 是读取结果；没有可接手的交接时 Found=false。
type LoadResponse struct {
	Found   bool     `json:"found"`
	Repo    string   `json:"repo"`
	Message string   `json:"message,omitempty"`
	Handoff *Handoff `json:"handoff,omitempty"`
}

// AckRequest 是确认请求。
type AckRequest struct {
	Repo       string `json:"repo"`
	Workstream string `json:"workstream,omitempty"`
	By         string `json:"by,omitempty"`
}

// ErrorBody 是 REST 错误响应。
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail 描述一个错误。
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// FromStore 把存储层对象转成传输对象。
func FromStore(repoKey string, h *store.Handoff) Handoff {
	if h == nil {
		return Handoff{}
	}
	return Handoff{
		Repo:          repoKey,
		Workstream:    h.Workstream,
		Revision:      h.Revision,
		Goal:          h.Goal,
		State:         h.State,
		NextSteps:     nonNil(h.NextSteps),
		Files:         nonNil(h.Files),
		Decisions:     nonNil(h.Decisions),
		OpenQuestions: nonNil(h.OpenQuestions),
		Client:        h.Client,
		SessionID:     h.SessionID,
		CreatedAt:     h.CreatedAt,
		AckedAt:       h.AckedAt,
		AckedBy:       h.AckedBy,
	}
}

// ToStore 把传输对象转回存储层对象，供 CLI 远程模式复用输出逻辑。
func (h Handoff) ToStore() *store.Handoff {
	a := h.AckedAt
	return &store.Handoff{
		Workstream:    h.Workstream,
		Revision:      h.Revision,
		Goal:          h.Goal,
		State:         h.State,
		NextSteps:     nonNil(h.NextSteps),
		Files:         nonNil(h.Files),
		Decisions:     nonNil(h.Decisions),
		OpenQuestions: nonNil(h.OpenQuestions),
		Client:        h.Client,
		SessionID:     h.SessionID,
		CreatedAt:     h.CreatedAt,
		AckedAt:       a,
		AckedBy:       h.AckedBy,
	}
}

// SummaryFromStore 转换列表项。
func SummaryFromStore(repoKey string, s store.Summary) Summary {
	return Summary{
		Repo:       repoKey,
		Workstream: s.Workstream,
		Revision:   s.Revision,
		Goal:       s.Goal,
		State:      s.State,
		Client:     s.Client,
		SessionID:  s.SessionID,
		CreatedAt:  s.CreatedAt,
	}
}

func nonNil(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
