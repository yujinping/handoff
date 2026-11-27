package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yujinping/handoff/internal/store"
)

func newTest(t *testing.T) *Service {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "handoff.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st)
}

func TestSaveDefaultsWorkstreamToCurrentClient(t *testing.T) {
	svc := newTest(t)
	ctx := context.Background()

	h, err := svc.Save(ctx, SaveInput{
		Repo:  "git@github.com:Owner/Repo.git",
		Goal:  "把 handoff 做出来",
		State: "仓库已初始化",
	})
	if err != nil {
		t.Fatal(err)
	}
	if h.Workstream != "current@unknown" {
		t.Fatalf("workstream = %q", h.Workstream)
	}
	if h.Revision != 1 {
		t.Fatalf("revision = %d", h.Revision)
	}

	h2, err := svc.Save(ctx, SaveInput{
		Repo:   "https://github.com/owner/repo",
		Client: "Claude-Code",
		Goal:   "继续",
		State:  "ok",
	})
	if err != nil {
		t.Fatal(err)
	}
	if h2.Workstream != "current@claude-code" {
		t.Fatalf("workstream = %q", h2.Workstream)
	}
}

func TestSaveThenLoadAcrossURLEorms(t *testing.T) {
	svc := newTest(t)
	ctx := context.Background()

	if _, err := svc.Save(ctx, SaveInput{
		Repo: "git@github.com:owner/repo.git", Client: "codex",
		Goal: "g", State: "s", NextSteps: nil,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := svc.Load(ctx, "https://github.com/OWNER/repo", "current@codex")
	if err != nil {
		t.Fatal(err)
	}
	if got.Goal != "g" || got.NextSteps == nil || len(got.NextSteps) != 0 {
		t.Fatalf("unexpected handoff: %+v", got)
	}
}

func TestLoadWithoutPending(t *testing.T) {
	svc := newTest(t)
	ctx := context.Background()
	if _, err := svc.Save(ctx, SaveInput{
		Repo: "github.com/a/b", Client: "codex", Goal: "g", State: "s",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Ack(ctx, "github.com/a/b", "", "zed"); err != nil {
		t.Fatal(err)
	}
	var nf *NotFoundError
	if _, err := svc.Load(ctx, "github.com/a/b", ""); !errors.As(err, &nf) {
		t.Fatalf("want NotFoundError, got %v", err)
	}
}

func TestAckConflict(t *testing.T) {
	svc := newTest(t)
	ctx := context.Background()
	if _, err := svc.Save(ctx, SaveInput{
		Repo: "github.com/a/b", Client: "codex", Goal: "g", State: "s",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Ack(ctx, "github.com/a/b", "", "claude"); err != nil {
		t.Fatal(err)
	}
	var ce *ConflictError
	if _, err := svc.Ack(ctx, "github.com/a/b", "current@codex", "zed"); !errors.As(err, &ce) {
		t.Fatalf("want ConflictError, got %v", err)
	}
}

func TestValidation(t *testing.T) {
	svc := newTest(t)
	ctx := context.Background()
	big := strings.Repeat("x", MaxFieldBytes+1)

	cases := []struct {
		name string
		in   SaveInput
	}{
		{"missing repo", SaveInput{Goal: "g", State: "s"}},
		{"local path repo", SaveInput{Repo: "/Users/me/repo", Goal: "g", State: "s"}},
		{"missing goal", SaveInput{Repo: "github.com/a/b", State: "s"}},
		{"missing state", SaveInput{Repo: "github.com/a/b", Goal: "g"}},
		{"goal too large", SaveInput{Repo: "github.com/a/b", Goal: big, State: "s"}},
		{"bad workstream", SaveInput{Repo: "github.com/a/b", Workstream: "a/b", Goal: "g", State: "s"}},
		{"bad client", SaveInput{Repo: "github.com/a/b", Client: "a b", Goal: "g", State: "s"}},
		{"too many items", SaveInput{Repo: "github.com/a/b", Goal: "g", State: "s", Files: make([]string, MaxItems+1)}},
		{"empty item", SaveInput{Repo: "github.com/a/b", Goal: "g", State: "s", Files: []string{"ok", ""}}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var ve *ValidationError
			if _, err := svc.Save(ctx, tt.in); !errors.As(err, &ve) {
				t.Fatalf("want ValidationError, got %v", err)
			}
		})
	}
}

func TestListReturnsPendingOnly(t *testing.T) {
	svc := newTest(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := svc.Save(ctx, SaveInput{
			Repo: "github.com/a/b", Client: "codex", Goal: "g", State: "s",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Ack(ctx, "github.com/a/b", "current@codex", "claude"); err != nil {
		t.Fatal(err)
	}
	list, err := svc.List(ctx, "github.com/a/b", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("pending = %+v, want empty", list)
	}
}

func TestValidationErrorDoesNotLeakInternalWrapping(t *testing.T) {
	svc := newTest(t)
	_, err := svc.Save(context.Background(), SaveInput{})
	if err == nil || !strings.Contains(err.Error(), "repo") {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = time.Now
}
