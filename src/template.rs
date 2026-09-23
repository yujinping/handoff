// Handoff / 交接 — 任务文档模板渲染
//
// @author yujinping
// @design 模板单一来源：优先取 store 仓库内 `templates/task-handoff.md`，缺失时回退内置默认，
// 保证工具在裸仓库下也能工作。project_id / remote / owner / host / updated 由 CLI 自动填充（§6），
// 使文档自描述、可 grep、可被 scan 校验归属。
// 「人」与「机器」拆为三个正交字段（§4.5）：`owner` 表归属（须等于所在 users/<id> 目录名）、
// `host` + `updated` 表最后写入者与时间——旧版把两者混写在一个 `by:` 里，无法做归属校验。

use crate::id::ProjectId;
use std::path::Path;
use std::process::Command;

/// 内置默认模板（与 `templates/task-handoff.md` 保持一致）。
pub const DEFAULT_TEMPLATE: &str = "\
# <project_id> / <slug>
- project_id: <host_alias>:<ns>/<repo>
- scope: <可选：monorepo 模块，如 v4-core>
- remote: <git remote url>
- status: in-progress
- owner: <owner>
- host: <host>
- updated: YYYY-MM-DD HH:mm
- goal: 一句话目标
- current: 已完成到什么程度
- decisions: 已定决策 + 理由（避免重复讨论）
- files: 涉及文件/链接（含 @file:line 引用）
- next: 编号的可执行下一步
- open: 待确认问题
- blockers: 阻塞项
- env: 关键命令 / 环境变量说明
- refs: 相关文档/链接
";

/// 当前本地时间 `YYYY-MM-DD HH:mm`（用 `date` 命令，跨 macOS/Linux 一致）。
pub fn now_str() -> String {
    Command::new("date")
        .arg("+%Y-%m-%d %H:%M")
        .output()
        .ok()
        .filter(|o| o.status.success())
        .map(|o| String::from_utf8_lossy(&o.stdout).trim().to_string())
        .filter(|s| !s.is_empty())
        .unwrap_or_else(|| "1970-01-01 00:00".to_string())
}

/// 当前主机名（写入 `by:` 字段，便于多机冲突归因）。
pub fn host_str() -> String {
    if let Ok(o) = Command::new("hostname").output() {
        if o.status.success() {
            let h = String::from_utf8_lossy(&o.stdout).trim().to_string();
            if !h.is_empty() {
                return h;
            }
        }
    }
    std::env::var("HOSTNAME").unwrap_or_else(|_| "unknown-host".to_string())
}

/// 渲染模板：填充 project_id / slug / host_alias / 路径 / scope / remote / owner / 时间 / 主机。
pub fn render(
    template: &str,
    pid: &ProjectId,
    slug: &str,
    remote: &str,
    scope: Option<&str>,
    owner: &str,
) -> String {
    template
        // 先替换更长、更具体的占位符，避免子串误替。
        .replace("<可选：monorepo 模块，如 v4-core>", scope.unwrap_or(""))
        .replace("<git remote url>", remote)
        .replace("<ns>/<repo>", &pid.path.join("/"))
        .replace("<project_id>", &pid.as_str())
        .replace("<host_alias>", &pid.host_alias)
        .replace("<slug>", slug)
        .replace("YYYY-MM-DD HH:mm", &now_str())
        .replace("<host>", &host_str())
        .replace("<owner>", owner)
}

/// 读取 store 仓库内的模板；缺失返回 None（调用方回退内置）。
pub fn load_template(root: &Path, store: &str) -> Option<String> {
    let p = root.join(store).join("templates").join("task-handoff.md");
    std::fs::read_to_string(p).ok()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn render_fills_placeholders() {
        let pid = ProjectId::new("codeup", vec!["iptv".into(), "android".into(), "v4".into()]);
        let out = render(
            DEFAULT_TEMPLATE,
            &pid,
            "p1-skeleton",
            "git@codeup.aliyun.com:iptv/android/v4.git",
            Some("v4-core"),
            "yujinping",
        );
        assert!(out.contains("# codeup:iptv/android/v4 / p1-skeleton"));
        assert!(out.contains("project_id: codeup:iptv/android/v4"));
        assert!(out.contains("scope: v4-core"));
        assert!(out.contains("remote: git@codeup.aliyun.com:iptv/android/v4.git"));
        // owner / host / updated 三字段各自独立填充（§4.5 拆分）
        assert!(out.contains("- owner: yujinping"));
        assert!(out.contains("- host: "));
        assert!(!out.contains("by:"), "旧的 by: 混写字段应已移除");
        // 所有占位符都应被替换。
        assert!(!out.contains("<project_id>"));
        assert!(!out.contains("<host_alias>"));
        assert!(!out.contains("<ns>/<repo>"));
        assert!(!out.contains("<slug>"));
        assert!(!out.contains("<host>"));
        assert!(!out.contains("<owner>"));
        assert!(!out.contains("YYYY-MM-DD"));
    }

    #[test]
    fn render_without_scope_leaves_empty() {
        let pid = ProjectId::new("gitee", vec!["o".into(), "r".into()]);
        let out = render(DEFAULT_TEMPLATE, &pid, "s", "url", None, "bob");
        assert!(out.contains("scope: \n") || out.contains("scope:\n") || out.contains("scope: "));
        assert!(!out.contains("<可选"));
        assert!(out.contains("- owner: bob"));
    }
}
