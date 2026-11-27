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
	if _, err := Detect(t.TempDir()); !errors.Is(err, ErrNoOrigin) {
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
