package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// EnsureProject 返回已有项目，不存在则创建。
func (s *Store) EnsureProject(ctx context.Context, remoteKey, displayName string, now time.Time) (*Project, error) {
	if displayName == "" {
		displayName = remoteKey
	}
	var p Project
	var created int64
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO projects (remote_key, display_name, created_at)
		VALUES (?, ?, ?)
		ON CONFLICT(remote_key) DO UPDATE SET remote_key = excluded.remote_key
		RETURNING id, remote_key, display_name, created_at`,
		remoteKey, displayName, millis(now),
	).Scan(&p.ID, &p.RemoteKey, &p.DisplayName, &created)
	if err != nil {
		return nil, fmt.Errorf("store: 写入项目: %w", err)
	}
	p.CreatedAt = fromMillis(created)
	return &p, nil
}

// ProjectByKey 按归一化后的项目键查询。
func (s *Store) ProjectByKey(ctx context.Context, remoteKey string) (*Project, error) {
	var p Project
	var created int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, remote_key, display_name, created_at FROM projects WHERE remote_key = ?`,
		remoteKey,
	).Scan(&p.ID, &p.RemoteKey, &p.DisplayName, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: 查询项目: %w", err)
	}
	p.CreatedAt = fromMillis(created)
	return &p, nil
}

// ListProjects 按创建时间倒序列出所有项目。
func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, remote_key, display_name, created_at FROM projects ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("store: 列出项目: %w", err)
	}
	defer rows.Close()

	var out []Project
	for rows.Next() {
		var p Project
		var created int64
		if err := rows.Scan(&p.ID, &p.RemoteKey, &p.DisplayName, &created); err != nil {
			return nil, fmt.Errorf("store: 读取项目: %w", err)
		}
		p.CreatedAt = fromMillis(created)
		out = append(out, p)
	}
	return out, rows.Err()
}
