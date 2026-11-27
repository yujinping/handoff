package repo

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNoOrigin 表示当前目录不是 git 仓库或没有 origin 远程。
var ErrNoOrigin = errors.New("没有找到 origin 远程")

// Detect 在 dir 下读取 git 的 origin 地址并归一化。dir 为空表示当前目录。
func Detect(dir string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("%w（在 %s 下运行 git remote get-url origin 失败）", ErrNoOrigin, dirOrCwd(dir))
		}
		return "", fmt.Errorf("%w: %v", ErrNoOrigin, err)
	}
	key, err := Normalize(strings.TrimSpace(string(out)))
	if err != nil {
		return "", err
	}
	return key, nil
}

func dirOrCwd(dir string) string {
	if dir == "" {
		return "当前目录"
	}
	return dir
}
