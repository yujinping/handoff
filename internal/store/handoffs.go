package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SaveHandoff 追加一条新 revision。revision 由数据库内当前最大值 +1 得到。
func (s *Store) SaveHandoff(ctx context.Context, h *Handoff) (*Handoff, error) {
	if h.CreatedAt.IsZero() {
		h.CreatedAt = time.Now().UTC()
	}
	next, _ := jsonArray(h.NextSteps)
	files, _ := jsonArray(h.Files)
	decisions, _ := jsonArray(h.Decisions)
	open, _ := jsonArray(h.OpenQuestions)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("store: 开始事务: %w", err)
	}
	defer tx.Rollback()

	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(revision), 0) + 1 FROM handoffs WHERE project_id = ? AND workstream = ?`,
		h.ProjectID, h.Workstream,
	).Scan(&h.Revision); err != nil {
		return nil, fmt.Errorf("store: 计算 revision: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO handoffs
			(project_id, workstream, revision, goal, state, next_steps, files, decisions,
			 open_questions, client, session_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		h.ProjectID, h.Workstream, h.Revision, h.Goal, h.State, next, files, decisions,
		open, h.Client, h.SessionID, millis(h.CreatedAt),
	); err != nil {
		return nil, fmt.Errorf("store: 写入交接: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("store: 提交交接: %w", err)
	}
	return h, nil
}

// ListPending 返回每个 workstream 最新且未确认的交接摘要。
func (s *Store) ListPending(ctx context.Context, projectID int64, limit int) ([]Summary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT h.workstream, h.revision, h.goal, h.state, h.client, h.session_id, h.created_at
		FROM handoffs h
		JOIN (
			SELECT workstream, MAX(revision) AS rev
			FROM handoffs
			WHERE project_id = ?
			GROUP BY workstream
		) latest ON latest.workstream = h.workstream AND latest.rev = h.revision
		WHERE h.project_id = ? AND h.acked_at IS NULL
		ORDER BY h.created_at DESC, h.workstream ASC
		LIMIT ?`, projectID, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: 列出待接手: %w", err)
	}
	defer rows.Close()

	var out []Summary
	for rows.Next() {
		var x Summary
		var created int64
		if err := rows.Scan(&x.Workstream, &x.Revision, &x.Goal, &x.State, &x.Client,
			&x.SessionID, &created); err != nil {
			return nil, fmt.Errorf("store: 读取待接手: %w", err)
		}
		x.CreatedAt = fromMillis(created)
		out = append(out, x)
	}
	return out, rows.Err()
}

// LatestHandoff 读取最新交接。
// workstream 非空时返回该工作线的最后一条（无论是否已确认）；
// 为空时返回整个项目最新的未确认交接。
func (s *Store) LatestHandoff(ctx context.Context, projectID int64, workstream string) (*Handoff, error) {
	var row *sql.Row
	if workstream != "" {
		row = s.db.QueryRowContext(ctx, selectHandoff+`
			WHERE project_id = ? AND workstream = ?
			ORDER BY revision DESC LIMIT 1`, projectID, workstream)
	} else {
		row = s.db.QueryRowContext(ctx, selectHandoff+`
			WHERE project_id = ? AND acked_at IS NULL
			ORDER BY created_at DESC, revision DESC LIMIT 1`, projectID)
	}
	h, err := scanHandoff(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return h, nil
}

// AckHandoff 原子确认最新交接。
// workstream 为空时选项目内最新未确认的一条。
// 返回 ErrConflict 表示该条已被其他接手方确认（或已被更新的 revision 取代）。
func (s *Store) AckHandoff(ctx context.Context, projectID int64, workstream, by string, now time.Time) (*Handoff, error) {
	var ws string
	var rev int
	var err error

	if workstream != "" {
		err = s.db.QueryRowContext(ctx, `
			SELECT workstream, MAX(revision)
			FROM handoffs
			WHERE project_id = ? AND workstream = ?
			GROUP BY workstream`, projectID, workstream).Scan(&ws, &rev)
	} else {
		err = s.db.QueryRowContext(ctx, `
			SELECT workstream, revision
			FROM handoffs
			WHERE project_id = ? AND acked_at IS NULL
			ORDER BY created_at DESC, revision DESC
			LIMIT 1`, projectID).Scan(&ws, &rev)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: 查找待确认交接: %w", err)
	}

	res, err := s.db.ExecContext(ctx, `
		UPDATE handoffs
		SET acked_at = ?, acked_by = ?
		WHERE project_id = ? AND workstream = ? AND revision = ? AND acked_at IS NULL`,
		millis(now), by, projectID, ws, rev)
	if err != nil {
		return nil, fmt.Errorf("store: 确认交接: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("store: 确认交接: %w", err)
	}
	if n == 0 {
		return nil, ErrConflict
	}

	h, err := scanHandoff(s.db.QueryRowContext(ctx, selectHandoff+`
		WHERE project_id = ? AND workstream = ? AND revision = ?`, projectID, ws, rev))
	if err != nil {
		return nil, fmt.Errorf("store: 读取已确认交接: %w", err)
	}
	return h, nil
}

const selectHandoff = `
	SELECT project_id, workstream, revision, goal, state, next_steps, files, decisions,
	       open_questions, client, session_id, created_at, acked_at, acked_by
	FROM handoffs`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanHandoff(row rowScanner) (*Handoff, error) {
	var h Handoff
	var next, files, decisions, open string
	var created int64
	var acked sql.NullInt64

	if err := row.Scan(&h.ProjectID, &h.Workstream, &h.Revision, &h.Goal, &h.State, &next,
		&files, &decisions, &open, &h.Client, &h.SessionID, &created, &acked, &h.AckedBy); err != nil {
		return nil, err
	}
	h.CreatedAt = fromMillis(created)
	h.AckedAt = fromMillisPtr(acked)
	h.NextSteps = parseArray(next)
	h.Files = parseArray(files)
	h.Decisions = parseArray(decisions)
	h.OpenQuestions = parseArray(open)
	return &h, nil
}

func parseArray(raw string) []string {
	out := []string{}
	if raw == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return []string{}
	}
	return out
}

func jsonArray(v []string) (string, error) {
	if v == nil {
		return "[]", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "[]", err
	}
	return string(b), nil
}
