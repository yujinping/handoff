package clientsync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	oldToken = "hd_deadbeef_OLDsecret000000000000000000000000000"
	newToken = "hd_0a1b2c3d_NEWsecret111111111111111111111111111"
)

func write(t *testing.T, dir, name, content string, perm os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		t.Fatalf("写测试文件: %v", err)
	}
	if err := os.Chmod(path, perm); err != nil {
		t.Fatalf("设置权限: %v", err)
	}
	return path
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读回文件: %v", err)
	}
	return string(data)
}

func TestSyncJSONReplacesTokenAndKeepsRest(t *testing.T) {
	dir := t.TempDir()
	original := `{
  "context_servers": {
    "handoff": {
      "url": "https://handoff.example.com/mcp",
      "headers": { "Authorization": "Bearer ` + oldToken + `" }
    },
    "other": {
      "headers": { "Authorization": "Bearer sk-not-a-handoff-token" }
    }
  }
}`
	path := write(t, dir, "settings.json", original, 0o600)

	results, err := Sync([]Target{{Name: "zed", Path: path}}, newToken, false)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("结果条数 = %d，想要 1", len(results))
	}
	if results[0].State != Updated {
		t.Fatalf("State = %v，想要 Updated", results[0].State)
	}
	if results[0].Count != 1 {
		t.Fatalf("Count = %d，想要 1", results[0].Count)
	}

	got := read(t, path)
	if !strings.Contains(got, `"Bearer `+newToken+`"`) {
		t.Fatalf("新 token 未写入:\n%s", got)
	}
	if strings.Contains(got, oldToken) {
		t.Fatalf("旧 token 仍在:\n%s", got)
	}
	// 别的服务商 token 不能被误伤。
	if !strings.Contains(got, `Bearer sk-not-a-handoff-token`) {
		t.Fatalf("非 handoff token 被改了:\n%s", got)
	}
	// 注释以外的内容保持原样。
	if !strings.Contains(got, "https://handoff.example.com/mcp") {
		t.Fatalf("url 被改坏了:\n%s", got)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("权限 = %o，想要 600", perm)
	}
}

func TestSyncYAMLSingleQuoted(t *testing.T) {
	dir := t.TempDir()
	original := `- insert:
    - id: mcp-handoff
      config:
        headers:
          Authorization: 'Bearer ` + oldToken + `'
`
	path := write(t, dir, "cordis.patch.yml", original, 0o600)

	results, err := Sync([]Target{{Name: "dsh:web", Path: path}}, newToken, false)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if results[0].State != Updated {
		t.Fatalf("State = %v，想要 Updated", results[0].State)
	}
	got := read(t, path)
	if !strings.Contains(got, `'Bearer `+newToken+`'`) {
		t.Fatalf("YAML 单引号形式未保留:\n%s", got)
	}
	if !strings.Contains(got, "id: mcp-handoff") {
		t.Fatalf("结构被改坏:\n%s", got)
	}
}

func TestSyncReplacesEveryOccurrence(t *testing.T) {
	dir := t.TempDir()
	original := "a: 'Bearer " + oldToken + "'\nb: \"Bearer " + oldToken + "\"\n"
	path := write(t, dir, "multi.yml", original, 0o644)

	results, err := Sync([]Target{{Name: "multi", Path: path}}, newToken, false)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if results[0].Count != 2 || results[0].State != Updated {
		t.Fatalf("Count/State = %d/%v，想要 2/Updated", results[0].Count, results[0].State)
	}
	got := read(t, path)
	if strings.Contains(got, oldToken) {
		t.Fatalf("还有旧 token:\n%s", got)
	}
	if strings.Count(got, newToken) != 2 {
		t.Fatalf("新 token 出现 %d 次，想要 2:\n%s", strings.Count(got, newToken), got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("权限 = %o，想要 644（应保持原样）", perm)
	}
}

func TestSyncNoHandoffTokenLeavesFileAlone(t *testing.T) {
	dir := t.TempDir()
	original := `{"mcpServers": {"other": {"headers": {"Authorization": "Bearer sk-xxx"}}}}`
	path := write(t, dir, "mcp.json", original, 0o600)

	results, err := Sync([]Target{{Name: "cursor", Path: path}}, newToken, false)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if results[0].State != NoToken {
		t.Fatalf("State = %v，想要 NoToken", results[0].State)
	}
	if got := read(t, path); got != original {
		t.Fatalf("文件被改了:\n%s", got)
	}
}

func TestSyncIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "mcp.json", `{"h": "Bearer `+oldToken+`"}`, 0o600)

	if _, err := Sync([]Target{{Name: "x", Path: path}}, newToken, false); err != nil {
		t.Fatalf("首次 Sync: %v", err)
	}
	results, err := Sync([]Target{{Name: "x", Path: path}}, newToken, false)
	if err != nil {
		t.Fatalf("二次 Sync: %v", err)
	}
	if results[0].State != Unchanged {
		t.Fatalf("State = %v，想要 Unchanged", results[0].State)
	}
}

func TestSyncDryRunReportsWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	original := `{"h": "Bearer ` + oldToken + `"}`
	path := write(t, dir, "mcp.json", original, 0o600)

	results, err := Sync([]Target{{Name: "x", Path: path}}, newToken, true)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if results[0].State != CheckDiff {
		t.Fatalf("State = %v，想要 CheckDiff", results[0].State)
	}
	if got := read(t, path); got != original {
		t.Fatalf("--check 不该写文件:\n%s", got)
	}

	if _, err := Sync([]Target{{Name: "x", Path: path}}, newToken, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	results, err = Sync([]Target{{Name: "x", Path: path}}, newToken, true)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if results[0].State != CheckOK {
		t.Fatalf("State = %v，想要 CheckOK", results[0].State)
	}
}

func TestSyncRejectsNonToken(t *testing.T) {
	dir := t.TempDir()
	path := write(t, dir, "mcp.json", `{"h": "Bearer `+oldToken+`"}`, 0o600)

	for _, bad := range []string{"", "not-a-token", "sk_abc", "hd_ bad"} {
		if _, err := Sync([]Target{{Name: "x", Path: path}}, bad, false); err == nil {
			t.Fatalf("token %q 应被拒绝", bad)
		}
	}
	if got := read(t, path); !strings.Contains(got, oldToken) {
		t.Fatalf("拒绝时不该改文件:\n%s", got)
	}
}

func TestSyncKeepsSymlink(t *testing.T) {
	dir := t.TempDir()
	real := write(t, dir, "dotfiles-settings.json", `{"h": "Bearer `+oldToken+`"}`, 0o600)
	link := filepath.Join(dir, "settings.json")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("建软链: %v", err)
	}

	results, err := Sync([]Target{{Name: "zed", Path: link}}, newToken, false)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if results[0].State != Updated {
		t.Fatalf("State = %v，想要 Updated", results[0].State)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("软链被 rename 换成了普通文件")
	}
	if got := read(t, real); !strings.Contains(got, newToken) {
		t.Fatalf("软链目标没更新，dotfiles 会静默分叉:\n%s", got)
	}

	realInfo, err := os.Stat(real)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := realInfo.Mode().Perm(); perm != 0o600 {
		t.Fatalf("目标文件权限 = %o，想要 600", perm)
	}
}

func TestSyncBrokenSymlinkFails(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "dangling.json")
	if err := os.Symlink(filepath.Join(dir, "gone.json"), link); err != nil {
		t.Fatalf("建软链: %v", err)
	}

	results, err := Sync([]Target{{Name: "x", Path: link}}, newToken, false)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if results[0].State != Missing {
		t.Fatalf("State = %v，想要 Missing", results[0].State)
	}
}

func TestSyncMissingFile(t *testing.T) {
	dir := t.TempDir()
	results, err := Sync([]Target{{Name: "x", Path: filepath.Join(dir, "nope.json")}}, newToken, false)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if results[0].State != Missing {
		t.Fatalf("State = %v，想要 Missing", results[0].State)
	}
}

func TestKnownRespectsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if got := Known(); len(got) != 0 {
		t.Fatalf("空 home 下应无目标，得到 %v", got)
	}

	zed := filepath.Join(home, ".config", "zed", "settings.json")
	if err := os.MkdirAll(filepath.Dir(zed), 0o700); err != nil {
		t.Fatalf("建目录: %v", err)
	}
	if err := os.WriteFile(zed, []byte("{}"), 0o600); err != nil {
		t.Fatalf("写 zed: %v", err)
	}
	dsh := filepath.Join(home, ".dsh", "profiles", "web", "cordis.patch.yml")
	if err := os.MkdirAll(filepath.Dir(dsh), 0o700); err != nil {
		t.Fatalf("建目录: %v", err)
	}
	if err := os.WriteFile(dsh, []byte("x: 1\n"), 0o600); err != nil {
		t.Fatalf("写 dsh: %v", err)
	}

	var names []string
	for _, target := range Known() {
		names = append(names, target.Name)
	}
	want := []string{"zed", "dsh:web"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("Known() = %v，想要 %v", names, want)
	}
}

func TestMaskNeverLeaksWholeToken(t *testing.T) {
	masked := Mask(newToken)
	if masked == newToken || strings.Contains(masked, newToken[9:len(newToken)-4]) {
		t.Fatalf("掩码泄露了 token 中段: %s", masked)
	}
	if Mask("hd_x") != "hd_…" {
		t.Fatalf("短 token 掩码 = %q", Mask("hd_x"))
	}
}
