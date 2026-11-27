// Package config 解析路径与远程地址，优先级：flag > 环境变量 > 默认值。
package config

import (
	"os"
	"path/filepath"
	"strings"
)

// DefaultDB 返回数据库默认路径。
func DefaultDB() string {
	if v := strings.TrimSpace(os.Getenv("HANDOFF_DB")); v != "" {
		return expand(v)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "handoff.db"
	}
	return filepath.Join(home, ".handoff", "handoff.db")
}

// DefaultServer 返回远程服务地址，空表示本地模式。
func DefaultServer() string {
	return strings.TrimSpace(os.Getenv("HANDOFF_SERVER"))
}

// DefaultToken 返回远程访问 token。
func DefaultToken() string {
	return strings.TrimSpace(os.Getenv("HANDOFF_TOKEN"))
}

// DefaultAddr 返回 serve 的监听地址。
func DefaultAddr() string {
	if v := strings.TrimSpace(os.Getenv("HANDOFF_ADDR")); v != "" {
		return v
	}
	return "127.0.0.1:8787"
}

func expand(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}
