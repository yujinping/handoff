// Handoff / 交接 — 信任域路由与目录映射
//
// @author yujinping
// @design 三层职责：
//   1) 信任域路由：host_alias → store（work / personal），由 Config 提供并允许覆盖。
//   2) 用户命名空间：store 之下先分 `users/<user_id>/`（§4.5），user_id 由 Config 四级解析。
//   3) 目录镜像：project_id 的 `:` 作目录第一级，`/` 原样作层级 →
//      `<root>/<store>/users/<user_id>/tasks/<host_alias>/<path...>/<slug>.md`。
// 层级镜像而非扁平拼接，因此不存在拼接歧义（a/b-c 与 a-b/c 不会撞，§4.2）。
// 命名空间只提供「误写防护 + 归档清晰」，**不构成读隔离**（§4.5）。

use crate::config::Config;
use crate::id::ProjectId;
use std::path::{Path, PathBuf};

/// store 仓库根：`<root>/<store>`。
pub fn store_dir(root: &Path, store: &str) -> PathBuf {
    root.join(store)
}

/// store 内的全部用户命名空间根：`<root>/<store>/users`。
pub fn users_root(root: &Path, store: &str) -> PathBuf {
    store_dir(root, store).join("users")
}

/// 单个用户命名空间根：`<root>/<store>/users/<user_id>`（§4.5）。
pub fn user_dir(root: &Path, store: &str, user: &str) -> PathBuf {
    users_root(root, store).join(user)
}

/// 用户命名空间下的 tasks 根。
pub fn tasks_dir(root: &Path, store: &str, user: &str) -> PathBuf {
    user_dir(root, store, user).join("tasks")
}

/// 用户命名空间下的 sessions 目录。
pub fn sessions_dir(root: &Path, store: &str, user: &str) -> PathBuf {
    user_dir(root, store, user).join("sessions")
}

/// 用户命名空间下的 archive 目录。
pub fn archive_dir(root: &Path, store: &str, user: &str) -> PathBuf {
    user_dir(root, store, user).join("archive")
}

/// project 目录：`<root>/<store>/users/<user>/tasks/<host_alias>/<path...>`。
pub fn project_dir(root: &Path, cfg: &Config, pid: &ProjectId) -> PathBuf {
    let store = pid.store(cfg);
    let mut p = tasks_dir(root, &store, &cfg.user).join(&pid.host_alias);
    for seg in &pid.path {
        p.push(seg);
    }
    p
}

/// 任务文档路径：`<root>/<store>/users/<user>/tasks/<host_alias>/<path...>/<slug>.md`。
/// slug 中的路径分隔符会被替换，避免逃逸到子目录。
pub fn task_path(root: &Path, cfg: &Config, pid: &ProjectId, slug: &str) -> PathBuf {
    let safe_slug = slug.replace(['/', '\\'], "-");
    project_dir(root, cfg, pid).join(format!("{safe_slug}.md"))
}

/// 从文档的**实际磁盘路径**反推其所属 user_id（§4.5 owner 归属校验用）。
///
/// 命中 `<root>/<store>/users/<user_id>/<rest>` 时返回 `<user_id>`，否则 None。
/// 判定一律以**目录**为准，不采信文档自称的 `owner` 字段——两者不符正是要拦截的情形。
pub fn owner_of_path(root: &Path, store: &str, file: &Path) -> Option<String> {
    let rest = file.strip_prefix(users_root(root, store)).ok()?;
    rest.components()
        .next()
        .and_then(|c| c.as_os_str().to_str())
        .map(|s| s.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn cfg() -> Config {
        Config::default()
    }

    #[test]
    fn hierarchical_mapping() {
        let cfg = cfg();
        let pid = ProjectId::new("codeup", vec!["iptv".into(), "android".into(), "v4".into()]);
        let p = task_path(Path::new("/root"), &cfg, &pid, "p1-skeleton");
        assert_eq!(
            p,
            PathBuf::from(format!(
                "/root/work/users/{}/tasks/codeup/iptv/android/v4/p1-skeleton.md",
                cfg.user
            ))
        );
    }

    #[test]
    fn personal_store_mapping() {
        let cfg = cfg();
        let pid = ProjectId::new("gitee", vec!["yujinping8620".into(), "memora".into()]);
        let p = task_path(Path::new("/root"), &cfg, &pid, "s1");
        assert_eq!(
            p,
            PathBuf::from(format!(
                "/root/personal/users/{}/tasks/gitee/yujinping8620/memora/s1.md",
                cfg.user
            ))
        );
    }

    #[test]
    fn user_namespace_is_partitioned() {
        // 同一 project、不同 user → 不同文件（Q7=A 的核心保证）
        let cfg_a = Config {
            user: "alice".to_string(),
            ..Config::default()
        };
        let cfg_b = Config {
            user: "bob".to_string(),
            ..Config::default()
        };
        let pid = ProjectId::new("codeup", vec!["iptv".into(), "android".into()]);
        let a = task_path(Path::new("/root"), &cfg_a, &pid, "s");
        let b = task_path(Path::new("/root"), &cfg_b, &pid, "s");
        assert_ne!(a, b);
        assert!(a.to_string_lossy().contains("/users/alice/"));
        assert!(b.to_string_lossy().contains("/users/bob/"));
    }

    #[test]
    fn no_flatten_collision() {
        let cfg = cfg();
        let a = task_path(
            Path::new("/root"),
            &cfg,
            &ProjectId::new("gitee", vec!["a".into(), "b-c".into()]),
            "s",
        );
        let b = task_path(
            Path::new("/root"),
            &cfg,
            &ProjectId::new("gitee", vec!["a-b".into(), "c".into()]),
            "s",
        );
        assert_ne!(a, b);
    }

    #[test]
    fn slug_with_slash_is_neutralized() {
        let cfg = cfg();
        let pid = ProjectId::new("gitee", vec!["o".into(), "r".into()]);
        let p = task_path(Path::new("/root"), &cfg, &pid, "bad/slug");
        assert_eq!(p.file_name().unwrap(), "bad-slug.md");
    }

    #[test]
    fn owner_of_path_reads_user_segment() {
        let root = Path::new("/root");
        let f = Path::new("/root/work/users/alice/tasks/codeup/iptv/v4/x.md");
        assert_eq!(owner_of_path(root, "work", f).as_deref(), Some("alice"));
        // 不在 users/ 之下 → None（不误判）
        let g = Path::new("/root/work/tasks/codeup/iptv/v4/x.md");
        assert_eq!(owner_of_path(root, "work", g), None);
        // store 不匹配 → None
        assert_eq!(owner_of_path(root, "personal", f), None);
    }
}
