package repo

import (
	"errors"
	"os/exec"
	"testing"
)

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
}

func TestDetect(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 未安装")
	}
	dir := t.TempDir()
	run(t, dir, "git", "init", "-q")
	run(t, dir, "git", "remote", "add", "origin", "git@github.com:Owner/Repo.git")

	got, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if want := "github.com/owner/repo"; got != want {
		t.Fatalf("Detect = %q, want %q", got, want)
	}
}

func TestDetectNoOrigin(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 未安装")
	}
	// 测试目录可能落在某个仓库内部（例如 GOTMPDIR 指向项目内），
	// 先初始化一个没有 origin 的仓库，保证 git 找到的是它而不是上层仓库。
	dir := t.TempDir()
	if err := exec.Command("git", "-C", dir, "init", "-q").Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if _, err := Detect(dir); !errors.Is(err, ErrNoOrigin) {
		t.Fatalf("err = %v, want ErrNoOrigin", err)
	}
}

func TestDetectLocalPath(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git 未安装")
	}
	dir := t.TempDir()
	run(t, dir, "git", "init", "-q")
	run(t, dir, "git", "remote", "add", "origin", "/tmp/some/local/repo")

	if _, err := Detect(dir); err == nil {
		t.Fatal("本地路径 remote 应当被拒绝")
	}
}
