// Handoff / 交接 — 信任域路由与目录映射
//
// @author yujinping
// @design 两层职责：
//   1) 信任域路由：host_alias → store（work / personal），由 Config 提供并允许覆盖。
//   2) 目录镜像：project_id 的 `:` 作目录第一级，`/` 原样作层级 →
//      `<root>/<store>/tasks/<host_alias>/<path...>/<slug>.md`。
// 层级镜像而非扁平拼接，因此不存在拼接歧义（a/b-c 与 a-b/c 不会撞，§4.2）。

use crate::config::Config;
use crate::id::ProjectId;
use std::path::{Path, PathBuf};

/// store 仓库根：`<root>/<store>`。
pub fn store_dir(root: &Path, store: &str) -> PathBuf {
    root.join(store)
}

/// store 下 tasks 根：`<root>/<store>/tasks`。
pub fn tasks_dir(root: &Path, store: &str) -> PathBuf {
    store_dir(root, store).join("tasks")
}

/// project 目录：`<root>/<store>/tasks/<host_alias>/<path...>`。
pub fn project_dir(root: &Path, cfg: &Config, pid: &ProjectId) -> PathBuf {
    let store = pid.store(cfg);
    let mut p = tasks_dir(root, &store).join(&pid.host_alias);
    for seg in &pid.path {
        p.push(seg);
    }
    p
}

/// 任务文档路径：`<root>/<store>/tasks/<host_alias>/<path...>/<slug>.md`。
/// slug 中的路径分隔符会被替换，避免逃逸到子目录。
pub fn task_path(root: &Path, cfg: &Config, pid: &ProjectId, slug: &str) -> PathBuf {
    let safe_slug = slug.replace(['/', '\\'], "-");
    project_dir(root, cfg, pid).join(format!("{safe_slug}.md"))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn cfg() -> Config {
        Config::default()
    }

    #[test]
    fn hierarchical_mapping() {
        let pid = ProjectId::new("codeup", vec!["iptv".into(), "android".into(), "v4".into()]);
        let p = task_path(Path::new("/root"), &cfg(), &pid, "p1-skeleton");
        assert_eq!(
            p,
            PathBuf::from("/root/work/tasks/codeup/iptv/android/v4/p1-skeleton.md")
        );
    }

    #[test]
    fn personal_store_mapping() {
        let pid = ProjectId::new("gitee", vec!["yujinping8620".into(), "memora".into()]);
        let p = task_path(Path::new("/root"), &cfg(), &pid, "s1");
        assert_eq!(
            p,
            PathBuf::from("/root/personal/tasks/gitee/yujinping8620/memora/s1.md")
        );
    }

    #[test]
    fn no_flatten_collision() {
        let a = task_path(
            Path::new("/root"),
            &cfg(),
            &ProjectId::new("gitee", vec!["a".into(), "b-c".into()]),
            "s",
        );
        let b = task_path(
            Path::new("/root"),
            &cfg(),
            &ProjectId::new("gitee", vec!["a-b".into(), "c".into()]),
            "s",
        );
        assert_ne!(a, b);
    }

    #[test]
    fn slug_with_slash_is_neutralized() {
        let pid = ProjectId::new("gitee", vec!["o".into(), "r".into()]);
        let p = task_path(Path::new("/root"), &cfg(), &pid, "bad/slug");
        assert_eq!(p.file_name().unwrap(), "bad-slug.md");
    }
}
