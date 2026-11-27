// Package store 负责 SQLite 持久化。它是唯一直接接触数据库的包。
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动，注册名 "sqlite"
)

var (
	// ErrNotFound 表示查询对象不存在。
	ErrNotFound = errors.New("not found")
	// ErrConflict 表示并发修改导致操作没有生效（例如已被别人 ack）。
	ErrConflict = errors.New("conflict")
)

// Project 是一个远程 git 仓库的本地记录。
type Project struct {
	ID          int64
	RemoteKey   string
	DisplayName string
	CreatedAt   time.Time
}

// Handoff 是一条不可变的交接快照。
type Handoff struct {
	// RepoKey 是归一化后的项目键，仅用于展示，不落库。
	RepoKey       string
	ProjectID     int64
	Workstream    string
	Revision      int
	Goal          string
	State         string
	NextSteps     []string
	Files         []string
	Decisions     []string
	OpenQuestions []string
	Client        string
	SessionID     string
	CreatedAt     time.Time
	AckedAt       *time.Time
	AckedBy       string
}

// Summary 是列表视图，不携带完整正文。
type Summary struct {
	// RepoKey 是归一化后的项目键，仅用于展示，不落库。
	RepoKey    string
	Workstream string
	Revision   int
	Goal       string
	State      string
	Client     string
	SessionID  string
	CreatedAt  time.Time
}

// Token 是远程访问凭证的元数据，不包含明文。
type Token struct {
	ID           string
	Name         string
	ScopeProject string // 空表示所有项目
	CreatedAt    time.Time
	LastUsedAt   *time.Time
	RevokedAt    *time.Time
}

// Store 持有数据库连接池。并发安全。
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS projects (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  remote_key   TEXT    NOT NULL UNIQUE,
  display_name TEXT    NOT NULL DEFAULT '',
  created_at   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS tokens (
  id            TEXT    PRIMARY KEY,
  name          TEXT    NOT NULL,
  hash          TEXT    NOT NULL UNIQUE,
  scope_project TEXT,
  created_at    INTEGER NOT NULL,
  last_used_at  INTEGER,
  revoked_at    INTEGER
);

CREATE TABLE IF NOT EXISTS handoffs (
  project_id     INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  workstream     TEXT    NOT NULL,
  revision       INTEGER NOT NULL,
  goal           TEXT    NOT NULL,
  state          TEXT    NOT NULL,
  next_steps     TEXT    NOT NULL DEFAULT '[]',
  files          TEXT    NOT NULL DEFAULT '[]',
  decisions      TEXT    NOT NULL DEFAULT '[]',
  open_questions TEXT    NOT NULL DEFAULT '[]',
  client         TEXT    NOT NULL DEFAULT '',
  session_id     TEXT    NOT NULL DEFAULT '',
  created_at     INTEGER NOT NULL,
  acked_at       INTEGER,
  acked_by       TEXT    NOT NULL DEFAULT '',
  PRIMARY KEY (project_id, workstream, revision)
);

CREATE INDEX IF NOT EXISTS idx_handoffs_pending
  ON handoffs (project_id, acked_at, created_at DESC);

PRAGMA user_version = 1;
`

// Open 打开（必要时创建）数据库，执行迁移并设置 PRAGMA。
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("store: 数据库路径为空")
	}
	if path != ":memory:" {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("store: 创建目录 %s: %w", dir, err)
		}
	}

	dsn := "file:" + filepath.ToSlash(path) +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(1)" +
		"&_txlock=immediate"

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: 打开数据库: %w", err)
	}
	// 写少读多；连接数保持很小，避免 SQLite 锁竞争。
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: 连接数据库: %w", err)
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: 迁移数据库: %w", err)
	}
	if path != ":memory:" {
		// 数据库里有个人工作现场，收紧权限。
		_ = os.Chmod(path, 0o600)
	}
	return &Store{db: db}, nil
}

// Close 关闭连接池。
func (s *Store) Close() error { return s.db.Close() }

// DB 暴露底层连接，仅供测试与排障使用。
func (s *Store) DB() *sql.DB { return s.db }

func millis(t time.Time) int64 { return t.UnixMilli() }

func fromMillis(v int64) time.Time { return time.UnixMilli(v).UTC() }

func fromMillisPtr(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := fromMillis(v.Int64)
	return &t
}
