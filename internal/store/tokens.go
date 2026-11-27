package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalidToken 表示 token 不存在、格式错误、已吊销或 hash 不匹配。
var ErrInvalidToken = errors.New("invalid token")

const tokenPrefix = "hd_"

// CreateToken 生成并保存一个新 token，返回明文与元数据。
// 明文只在此刻可见，数据库只存 secret 的 sha256。
func (s *Store) CreateToken(ctx context.Context, name, scopeProject string, now time.Time) (string, *Token, error) {
	if name == "" {
		return "", nil, errors.New("store: token 名称不能为空")
	}
	idBytes := make([]byte, 4)
	if _, err := rand.Read(idBytes); err != nil {
		return "", nil, fmt.Errorf("store: 生成 token id: %w", err)
	}
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return "", nil, fmt.Errorf("store: 生成 token secret: %w", err)
	}
	id := hex.EncodeToString(idBytes)
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	plain := tokenPrefix + id + "_" + secret

	sum := sha256.Sum256([]byte(secret))
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO tokens (id, name, hash, scope_project, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		id, name, hex.EncodeToString(sum[:]), nullString(scopeProject), millis(now),
	); err != nil {
		return "", nil, fmt.Errorf("store: 写入 token: %w", err)
	}
	return plain, &Token{
		ID:           id,
		Name:         name,
		ScopeProject: scopeProject,
		CreatedAt:    now,
	}, nil
}

// ListTokens 按创建时间倒序列出 token，不含明文与 hash。
func (s *Store) ListTokens(ctx context.Context) ([]Token, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, COALESCE(scope_project, ''), created_at, last_used_at, revoked_at
		FROM tokens ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("store: 列出 token: %w", err)
	}
	defer rows.Close()

	var out []Token
	for rows.Next() {
		var t Token
		var created int64
		var lastUsed, revoked sql.NullInt64
		if err := rows.Scan(&t.ID, &t.Name, &t.ScopeProject, &created, &lastUsed, &revoked); err != nil {
			return nil, fmt.Errorf("store: 读取 token: %w", err)
		}
		t.CreatedAt = fromMillis(created)
		t.LastUsedAt = fromMillisPtr(lastUsed)
		t.RevokedAt = fromMillisPtr(revoked)
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokeToken 吊销指定 token。已吊销或不存在返回 ErrNotFound。
func (s *Store) RevokeToken(ctx context.Context, id string, now time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		millis(now), id)
	if err != nil {
		return fmt.Errorf("store: 吊销 token: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: 吊销 token: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ActiveTokenCount 返回未吊销 token 数量。serve 启动时用它判断是否需要引导用户建 token。
func (s *Store) ActiveTokenCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM tokens WHERE revoked_at IS NULL`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("store: 统计 token: %w", err)
	}
	return n, nil
}

// Authenticate 校验明文 token，成功时返回元数据并顺带更新 last_used_at。
// last_used_at 最多每 60 秒写一次，避免每个请求都产生写事务。
func (s *Store) Authenticate(ctx context.Context, plain string, now time.Time) (*Token, error) {
	rest, ok := strings.CutPrefix(plain, tokenPrefix)
	if !ok {
		return nil, ErrInvalidToken
	}
	id, secret, ok := strings.Cut(rest, "_")
	if !ok || len(id) != 8 || secret == "" {
		return nil, ErrInvalidToken
	}

	var t Token
	var storedHash string
	var created int64
	var lastUsed, revoked sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, hash, COALESCE(scope_project, ''), created_at, last_used_at, revoked_at
		FROM tokens WHERE id = ?`, id,
	).Scan(&t.ID, &t.Name, &storedHash, &t.ScopeProject, &created, &lastUsed, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, fmt.Errorf("store: 查询 token: %w", err)
	}
	if revoked.Valid {
		return nil, ErrInvalidToken
	}

	sum := sha256.Sum256([]byte(secret))
	if subtle.ConstantTimeCompare([]byte(storedHash), []byte(hex.EncodeToString(sum[:]))) != 1 {
		return nil, ErrInvalidToken
	}

	t.CreatedAt = fromMillis(created)
	t.LastUsedAt = fromMillisPtr(lastUsed)
	t.RevokedAt = fromMillisPtr(revoked)

	const refresh = 60 * time.Second
	if !lastUsed.Valid || now.Sub(fromMillis(lastUsed.Int64)) > refresh {
		_, _ = s.db.ExecContext(ctx,
			`UPDATE tokens SET last_used_at = ? WHERE id = ?`, millis(now), id)
	}
	return &t, nil
}

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
