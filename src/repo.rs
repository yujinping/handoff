// Handoff / 交接 — git 操作封装
//
// @author yujinping
// @design 直接 shell-out `git`（不引 git2/libgit2，§0.3）：避免 libgit2 编译重与 SSH 凭证链复杂。
// 所有操作以 `git -C <dir>` 形式在指定仓库目录内执行；失败即返回错误（含 stderr）。

use anyhow::{bail, Context, Result};
use std::path::Path;
use std::process::Command;

/// 在 dir 内执行 git 子命令，成功返回 stdout。
pub fn git(dir: &Path, args: &[&str]) -> Result<String> {
    let out = Command::new("git")
        .arg("-C")
        .arg(dir)
        .args(args)
        .output()
        .with_context(|| format!("执行 `git {}` 失败", args.join(" ")))?;
    if !out.status.success() {
        // 诊断信息保留退出码与 stdout 摘录：git 的部分失败（如 nothing to commit）走 stdout，
        // 仅凭 stderr 会得到空消息，无法定位（P5 真机测试实测踩坑，2026-09-24）。
        let stdout_tail = {
            let s = String::from_utf8_lossy(&out.stdout);
            let t = s.trim();
            if t.len() > 200 { format!("{}…", &t[t.len() - 200..]) } else { t.to_string() }
        };
        bail!(
            "`git {}` 退出非零(code={}): {}{}",
            args.join(" "),
            out.status.code().map(|c| c.to_string()).unwrap_or_else(|| "signal".into()),
            String::from_utf8_lossy(&out.stderr).trim(),
            if stdout_tail.is_empty() { String::new() } else { format!(" | stdout: {stdout_tail}") }
        );
    }
    Ok(String::from_utf8_lossy(&out.stdout).to_string())
}

/// 静默执行（不因非零退出而报错），返回 (success, stdout)。
pub fn git_quiet(dir: &Path, args: &[&str]) -> (bool, String) {
    match Command::new("git").arg("-C").arg(dir).args(args).output() {
        Ok(out) => (
            out.status.success(),
            String::from_utf8_lossy(&out.stdout).to_string(),
        ),
        Err(_) => (false, String::new()),
    }
}

/// 取 remote url：优先 origin，其次首个 remote。无 remote 返回 None。
pub fn remote_url(dir: &Path) -> Option<String> {
    if let (true, out) = git_quiet(dir, &["config", "--get", "remote.origin.url"]) {
        let url = out.trim();
        if !url.is_empty() {
            return Some(url.to_string());
        }
    }
    let (ok, out) = git_quiet(dir, &["remote"]);
    if ok {
        let first = out.lines().next().map(|s| s.trim()).filter(|s| !s.is_empty())?;
        let (ok2, url) = git_quiet(dir, &["config", &format!("remote.{}.url", first)]);
        if ok2 {
            let url = url.trim();
            if !url.is_empty() {
                return Some(url.to_string());
            }
        }
    }
    None
}

/// add -A + commit（幂等）：无变更可提交视为成功，返回是否产生了新提交。
///
/// @design P5 真机测试结论（2026-09-24）：AI 客户端的沙箱升级重跑会让同一 save 命令
/// 执行两次，第二次必然 nothing to commit（该信息走 stdout、exit 1），若按失败处理
/// 会误报。重试环境下 save 必须幂等。
pub fn commit_all_idempotent(dir: &Path, message: &str) -> Result<bool> {
    git(dir, &["add", "-A"])?;
    match git(dir, &["commit", "-m", message]) {
        Ok(_) => Ok(true),
        Err(e) => {
            if format!("{e:#}").contains("nothing to commit") {
                Ok(false)
            } else {
                Err(e)
            }
        }
    }
}

/// push；首次无 upstream 时回退 `push --set-upstream origin HEAD`。
pub fn push(dir: &Path) -> Result<()> {
    if git(dir, &["push"]).is_err() {
        git(dir, &["push", "--set-upstream", "origin", "HEAD"])?;
    }
    Ok(())
}

/// pull --rebase。
pub fn pull_rebase(dir: &Path) -> Result<()> {
    git(dir, &["pull", "--rebase"])?;
    Ok(())
}
