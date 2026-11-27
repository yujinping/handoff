// Package repo 把各种写法的 git remote 地址归一化成稳定的项目键。
//
// 项目键的形态是 host/path，例如 github.com/yujinping/handoff。
// 它只描述“哪个仓库”，不包含协议、用户、端口和 .git 后缀，
// 这样同一仓库的 ssh 与 https 地址会落到同一条记录。
package repo

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// ErrNotRemote 表示输入不是一个可识别的远程 git 地址。
var ErrNotRemote = errors.New("not a remote git URL")

var hostPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)

// 这些托管平台的仓库路径大小写不敏感，统一转小写。
var caseInsensitiveHosts = map[string]bool{
	"github.com":        true,
	"gitlab.com":        true,
	"bitbucket.org":     true,
	"gitee.com":         true,
	"codeup.aliyun.com": true,
}

// Normalize 返回归一化后的项目键。
func Normalize(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("%w: 地址为空", ErrNotRemote)
	}
	if strings.ContainsAny(s, "\x00\r\n\t") {
		return "", fmt.Errorf("%w: 含控制字符", ErrNotRemote)
	}
	// 本地路径与 Windows 路径直接拒绝：本项目按远程仓库归属，不按目录归属。
	if strings.ContainsRune(s, '\\') ||
		strings.HasPrefix(s, "/") ||
		strings.HasPrefix(s, "./") ||
		strings.HasPrefix(s, "../") ||
		strings.HasPrefix(s, "~") ||
		strings.HasPrefix(strings.ToLower(s), "file://") {
		return "", fmt.Errorf("%w: %q 是本地路径；请传项目的 origin 远程地址", ErrNotRemote, raw)
	}

	var host, path string
	if i := strings.Index(s, "://"); i >= 0 {
		u, err := url.Parse(s)
		if err != nil {
			return "", fmt.Errorf("%w: %q: %v", ErrNotRemote, raw, err)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "ssh", "git":
		default:
			return "", fmt.Errorf("%w: 不支持的协议 %q", ErrNotRemote, u.Scheme)
		}
		host = u.Hostname()
		path = u.Path
	} else if i := strings.IndexByte(s, ':'); i > 0 && !strings.Contains(s[:i], "/") {
		// scp 风格：git@github.com:owner/repo.git
		host, path = s[:i], s[i+1:]
		if at := strings.LastIndexByte(host, '@'); at >= 0 {
			host = host[at+1:]
		}
	} else if i := strings.IndexByte(s, '/'); i > 0 {
		// 无协议：github.com/owner/repo
		host, path = s[:i], s[i+1:]
	} else {
		return "", fmt.Errorf("%w: %q 缺少仓库路径", ErrNotRemote, raw)
	}

	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || !hostPattern.MatchString(host) {
		return "", fmt.Errorf("%w: %q 的主机名无效", ErrNotRemote, raw)
	}

	path = strings.Trim(path, "/")
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	if strings.HasSuffix(strings.ToLower(path), ".git") {
		path = path[:len(path)-4]
	}
	path = strings.Trim(path, "/")
	if path == "" {
		return "", fmt.Errorf("%w: %q 缺少仓库路径", ErrNotRemote, raw)
	}

	for _, seg := range strings.Split(path, "/") {
		switch seg {
		case "", ".", "..":
			return "", fmt.Errorf("%w: %q 的仓库路径无效", ErrNotRemote, raw)
		}
		if strings.ContainsAny(seg, " \x00\r\n\t?#") {
			return "", fmt.Errorf("%w: %q 的仓库路径含非法字符", ErrNotRemote, raw)
		}
	}

	if caseInsensitiveHosts[host] {
		path = strings.ToLower(path)
	}
	return host + "/" + path, nil
}

// DisplayName 返回项目的人类可读短名，用作 projects.display_name 的默认值。
func DisplayName(key string) string {
	if i := strings.LastIndexByte(key, '/'); i >= 0 && i+1 < len(key) {
		return key[i+1:]
	}
	return key
}
