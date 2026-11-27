// Package clientsync 把本机 token 同步到各 MCP 客户端的配置文件。
//
// 远程客户端只接受字面量 Authorization，没有变量插值，所以同一枚 token
// 必然在每个配置文件里各存一份。这里只做一件事：给定明文 token，把文件里
// 已有的 handoff token 就地替换，其余内容和文件权限都不动。
package clientsync

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// TokenPattern 是 handoff 明文 token 的正则形状，供匹配与校验共用。
const TokenPattern = `hd_[A-Za-z0-9_-]+`

var tokenRe = regexp.MustCompile(`^` + TokenPattern + `$`)

// bearerRe 不加引号限定，所以 JSON 的 "Bearer hd_x" 和 YAML 的 'Bearer hd_x' 都能命中。
var bearerRe = regexp.MustCompile(`Bearer\s+(` + TokenPattern + `)`)

// State 描述单个目标文件的同步结果。
type State int

const (
	// Updated 表示已写入新 token。
	Updated State = iota
	// Unchanged 表示文件里已经是这枚 token。
	Unchanged
	// NoToken 表示文件里没有 handoff token，跳过不动。
	NoToken
	// CheckOK 表示 --check 下与源 token 一致。
	CheckOK
	// CheckDiff 表示 --check 下与源 token 不一致。
	CheckDiff
	// Missing 表示目标文件不存在。
	Missing
	// Failed 表示读或写失败，原因在 Result.Err。
	Failed
)

func (s State) String() string {
	switch s {
	case Updated:
		return "已更新"
	case Unchanged:
		return "已是最新"
	case NoToken:
		return "无 handoff token"
	case CheckOK:
		return "一致"
	case CheckDiff:
		return "不一致"
	case Missing:
		return "文件不存在"
	case Failed:
		return "写入失败"
	}
	return "未知"
}

// Target 是一个待同步的配置文件。
type Target struct {
	Name string
	Path string
}

// Result 是单个文件的同步结果。
type Result struct {
	Target
	State State
	// Count 是文件里命中并被替换的 token 个数。
	Count int
	Err   error
}

// IsToken 判断字符串是否具备 handoff 明文 token 的形状。
func IsToken(s string) bool { return tokenRe.MatchString(s) }

// Mask 返回可以安全打印的 token 片段。
func Mask(token string) string {
	if len(token) <= 9 {
		return "hd_…"
	}
	return token[:5] + "…" + token[len(token)-4:]
}

// Known 返回本机常见 MCP 客户端配置里存在的文件。
// 只返回确实存在的路径，没装的客户端直接略过。
func Known() []Target {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	direct := []Target{
		{Name: "zed", Path: filepath.Join(home, ".config", "zed", "settings.json")},
		{Name: "workbuddy", Path: filepath.Join(home, ".workbuddy", "mcp.json")},
		{Name: "cursor", Path: filepath.Join(home, ".cursor", "mcp.json")},
		{Name: "claude-desktop", Path: filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")},
	}
	var out []Target
	for _, t := range direct {
		if isFile(t.Path) {
			out = append(out, t)
		}
	}
	// DSH 每个 profile 一个 patch 文件。
	matches, _ := filepath.Glob(filepath.Join(home, ".dsh", "profiles", "*", "cordis.patch.yml"))
	sort.Strings(matches)
	for _, m := range matches {
		out = append(out, Target{Name: "dsh:" + filepath.Base(filepath.Dir(m)), Path: m})
	}
	return out
}

// Sync 把 token 写进各目标文件。dryRun 为 true 时只比对，不落盘。
func Sync(targets []Target, token string, dryRun bool) ([]Result, error) {
	if !IsToken(token) {
		return nil, errors.New("clientsync: token 不是 hd_ 形状，拒绝写入")
	}
	results := make([]Result, 0, len(targets))
	for _, t := range targets {
		results = append(results, syncOne(t, token, dryRun))
	}
	return results, nil
}

func syncOne(t Target, token string, dryRun bool) Result {
	res := Result{Target: t}

	info, err := os.Stat(t.Path)
	if err != nil {
		res.State = Missing
		res.Err = err
		return res
	}
	if !info.Mode().IsRegular() {
		res.State = Failed
		res.Err = fmt.Errorf("clientsync: %s 不是普通文件", t.Path)
		return res
	}
	data, err := os.ReadFile(t.Path)
	if err != nil {
		res.State = Failed
		res.Err = err
		return res
	}

	hits := bearerRe.FindAllSubmatchIndex(data, -1)
	if len(hits) == 0 {
		res.State = NoToken
		return res
	}
	res.Count = len(hits)

	patched := replaceToken(data, hits, token)
	if bytes.Equal(patched, data) {
		res.State = Unchanged
		if dryRun {
			res.State = CheckOK
		}
		return res
	}
	if dryRun {
		res.State = CheckDiff
		return res
	}
	// 写之前解析软链：配置用 stow 之类软链部署时，rename 会把软链换成普通文件，
	// 导致 dotfiles 里那份静默分叉。写到真实文件上，软链保留。
	realPath, err := filepath.EvalSymlinks(t.Path)
	if err != nil {
		res.State = Failed
		res.Err = fmt.Errorf("clientsync: 解析软链: %w", err)
		return res
	}
	if err := writeAtomic(realPath, patched, info.Mode().Perm()); err != nil {
		res.State = Failed
		res.Err = err
		return res
	}
	res.State = Updated
	return res
}

// replaceToken 只替换捕获组里的 token 本体，保留 Bearer 前缀与引号写法。
func replaceToken(data []byte, hits [][]int, token string) []byte {
	var buf bytes.Buffer
	buf.Grow(len(data))
	last := 0
	for _, m := range hits {
		start, end := m[2], m[3]
		buf.Write(data[last:start])
		buf.WriteString(token)
		last = end
	}
	buf.Write(data[last:])
	return buf.Bytes()
}

// writeAtomic 先写同目录临时文件再 rename，避免写一半留下坏配置。
func writeAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".handoff-sync-*")
	if err != nil {
		return fmt.Errorf("clientsync: 创建临时文件: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("clientsync: 写临时文件: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("clientsync: 关闭临时文件: %w", err)
	}
	if err := os.Chmod(tmp, perm); err != nil {
		return fmt.Errorf("clientsync: 设置权限: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("clientsync: 替换文件: %w", err)
	}
	return nil
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
