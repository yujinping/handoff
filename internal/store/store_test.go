package store

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "handoff.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigrateIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "handoff.db")
	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("Open #%d: %v", i, err)
		}
		s.Close()
	}
}

func TestEnsureProjectIsIdempotent(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	now := time.Now()

	p1, err := s.EnsureProject(ctx, "github.com/a/b", "b", now)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := s.EnsureProject(ctx, "github.com/a/b", "b", now)
	if err != nil {
		t.Fatal(err)
	}
	if p1.ID != p2.ID {
		t.Fatalf("expected same project id, got %d and %d", p1.ID, p2.ID)
	}
	if _, err := s.ProjectByKey(ctx, "github.com/x/y"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSaveHandoffIncrementsRevision(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	now := time.Now()
	p, _ := s.EnsureProject(ctx, "github.com/a/b", "b", now)

	h1, err := s.SaveHandoff(ctx, &Handoff{
		ProjectID: p.ID, Workstream: "current@codex", Goal: "g1", State: "s1",
		NextSteps: []string{"n1"}, CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	h2, err := s.SaveHandoff(ctx, &Handoff{
		ProjectID: p.ID, Workstream: "current@codex", Goal: "g2", State: "s2",
		OpenQuestions: []string{"q1"}, CreatedAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if h1.Revision != 1 || h2.Revision != 2 {
		t.Fatalf("revisions = %d, %d; want 1, 2", h1.Revision, h2.Revision)
	}

	got, err := s.LatestHandoff(ctx, p.ID, "current@codex")
	if err != nil {
		t.Fatal(err)
	}
	if got.Goal != "g2" || len(got.OpenQuestions) != 1 || got.OpenQuestions[0] != "q1" {
		t.Fatalf("unexpected handoff: %+v", got)
	}
}

func TestConcurrentSaveProducesContiguousRevisions(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	now := time.Now()
	p, _ := s.EnsureProject(ctx, "github.com/a/b", "b", now)

	const n = 12
	var wg sync.WaitGroup
	revs := make(chan int, n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			h, err := s.SaveHandoff(ctx, &Handoff{
				ProjectID: p.ID, Workstream: "current@codex",
				Goal: "g", State: "s", CreatedAt: now.Add(time.Duration(i) * time.Millisecond),
			})
			if err != nil {
				errs <- err
				return
			}
			revs <- h.Revision
		}(i)
	}
	wg.Wait()
	close(revs)
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent save: %v", err)
	}
	seen := map[int]bool{}
	for r := range revs {
		if seen[r] {
			t.Fatalf("duplicate revision %d", r)
		}
		seen[r] = true
	}
	for i := 1; i <= n; i++ {
		if !seen[i] {
			t.Fatalf("missing revision %d", i)
		}
	}
}

func TestListPendingOnlyLatestUnacked(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	now := time.Now()
	p, _ := s.EnsureProject(ctx, "github.com/a/b", "b", now)

	for i := 0; i < 3; i++ {
		if _, err := s.SaveHandoff(ctx, &Handoff{
			ProjectID: p.ID, Workstream: "current@claude", Goal: "g", State: "s",
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.SaveHandoff(ctx, &Handoff{
		ProjectID: p.ID, Workstream: "review", Goal: "r", State: "s", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListPending(ctx, p.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("pending = %d, want 2: %+v", len(list), list)
	}
	for _, x := range list {
		if x.Workstream == "current@claude" && x.Revision != 3 {
			t.Fatalf("expected latest revision 3, got %d", x.Revision)
		}
	}
}

func TestAckIsCompareAndSwap(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	now := time.Now()
	p, _ := s.EnsureProject(ctx, "github.com/a/b", "b", now)
	if _, err := s.SaveHandoff(ctx, &Handoff{
		ProjectID: p.ID, Workstream: "current@claude", Goal: "g", State: "s", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	acked, err := s.AckHandoff(ctx, p.ID, "current@claude", "codex", now)
	if err != nil {
		t.Fatal(err)
	}
	if acked.AckedAt == nil || acked.AckedBy != "codex" {
		t.Fatalf("unexpected ack: %+v", acked)
	}
	if _, err := s.AckHandoff(ctx, p.ID, "current@claude", "zed", now); !errors.Is(err, ErrConflict) {
		t.Fatalf("second ack: want ErrConflict, got %v", err)
	}
	if _, err := s.LatestHandoff(ctx, p.ID, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("pending after ack: want ErrNotFound, got %v", err)
	}
}

func TestAckWithoutWorkstreamPicksLatestPending(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	now := time.Now()
	p, _ := s.EnsureProject(ctx, "github.com/a/b", "b", now)

	for i, ws := range []string{"a", "b", "c"} {
		if _, err := s.SaveHandoff(ctx, &Handoff{
			ProjectID: p.ID, Workstream: ws, Goal: ws, State: "s",
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}
	acked, err := s.AckHandoff(ctx, p.ID, "", "me", now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if acked.Workstream != "c" {
		t.Fatalf("acked workstream %q, want c", acked.Workstream)
	}
}

func TestTokenLifecycle(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	now := time.Now()

	plain, tok, err := s.CreateToken(ctx, "macbook", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if plain == "" || tok.ID == "" {
		t.Fatal("empty token")
	}
	got, err := s.Authenticate(ctx, plain, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != tok.ID || got.Name != "macbook" {
		t.Fatalf("authenticate = %+v", got)
	}
	if _, err := s.Authenticate(ctx, "hdt_deadbeef_wrong", now); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("wrong token: want ErrInvalidToken, got %v", err)
	}
	if err := s.RevokeToken(ctx, tok.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, plain, now); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("revoked token: want ErrInvalidToken, got %v", err)
	}
}

func TestTokenScopeIsReturned(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	now := time.Now()
	plain, _, err := s.CreateToken(ctx, "scoped", "github.com/a/b", now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Authenticate(ctx, plain, now)
	if err != nil {
		t.Fatal(err)
	}
	if got.ScopeProject != "github.com/a/b" {
		t.Fatalf("scope = %q", got.ScopeProject)
	}
}
