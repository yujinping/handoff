// Handoff / 交接 — 配置
//
// @author yujinping
// @design 两层配置（§4.4）：
//   1) 本机层 `~/.config/handoff/config.toml`（不入库）：store 根路径、host_alias→store 覆盖、remote→id 固定。
//   2) 仓库层 `_meta/projects.toml`（入库）：canonical 覆盖 / 改名 alias（见 id.rs 消费）。
// 解析顺序：环境变量 HANDOFF_ROOT > 本机 config.toml > 内置默认（~/data/workspace/personal-open-source/ai-handoff）。
// 刻意不引 serde：直接解析 toml::Value，避免 flatten 在本场景下的兼容坑。

use std::collections::{BTreeSet, HashMap};
use std::path::PathBuf;
use std::process::Command;

/// 默认 store 根（相对 home）。
pub const DEFAULT_REL_ROOT: &str = "data/workspace/personal-open-source/ai-handoff";

/// host → 托管域别名（§4.2 别名表）。
pub const DEFAULT_ALIASES: &[(&str, &str)] = &[
    ("codeup.aliyun.com", "codeup"),
    ("gitee.com", "gitee"),
    ("git.gitee.com", "gitee"),
    ("github.com", "github"),
];

/// 别名 → store 默认归属（§4.1 / §4.2）。
pub const DEFAULT_STORE_OF: &[(&str, &str)] = &[
    ("codeup", "work"),
    ("gitee", "personal"),
    ("github", "personal"),
    ("local", "personal"),
];

/// user_id 全链失效时的兜底常量（§4.5：**绝不产出空路径段**）。
pub const FALLBACK_USER: &str = "user";

/// user_id 命中级（供 `handoff whoami` 诊断，§4.5）。
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum UserSource {
    /// 1) 环境变量 `HANDOFF_USER`（临时 / CI，最高优先）
    Env,
    /// 2) `~/.config/handoff/config.toml` 顶层 `user`
    ConfigFile,
    /// 3) `git config --global user.name`
    GitGlobal,
    /// 4) `whoami`
    System,
    /// 全链失败 → 兜底常量
    Fallback,
}

impl UserSource {
    /// 展示名。
    pub fn as_str(&self) -> &'static str {
        match self {
            UserSource::Env => "env:HANDOFF_USER",
            UserSource::ConfigFile => "config:user",
            UserSource::GitGlobal => "git:user.name",
            UserSource::System => "whoami",
            UserSource::Fallback => "fallback",
        }
    }
}

/// 运行期配置。
#[derive(Debug, Clone)]
pub struct Config {
    /// store 根容器目录（其下为 work/ personal/ 等独立仓库）。
    pub root: PathBuf,
    /// 当前 user_id（`users/<user_id>/` 命名空间，§4.5）。
    pub user: String,
    /// user_id 的命中级（诊断用）。
    pub user_source: UserSource,
    /// host_alias → store 归属（可由本机 config.toml 覆盖）。
    pub host_alias_store: HashMap<String, String>,
    /// remote url → project_id（仅本机有效，优先于派生）。
    pub local_override: HashMap<String, String>,
}

/// home 目录（macOS/Linux 均以 HOME 为准）。
pub fn home_dir() -> PathBuf {
    std::env::var_os("HOME")
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from("."))
}

/// 展开开头的 `~/`。
pub fn expand_tilde(s: &str) -> PathBuf {
    if let Some(rest) = s.strip_prefix("~/") {
        home_dir().join(rest)
    } else if s == "~" {
        home_dir()
    } else {
        PathBuf::from(s)
    }
}

/// user_id 可用性判定：sanitize（小写 + `[^a-z0-9._-]`→`-`）后须
/// **含至少一个 ASCII 字母数字、且不以 `.` 开头**，否则视为无效。
/// 这条规则挡住了中文 `user.name`（如「于金平」→ 全是 `-`）与空值。
pub fn usable_user(raw: &str) -> Option<String> {
    let s = crate::id::sanitize_seg(raw.trim());
    if s.is_empty() || s.starts_with('.') {
        return None;
    }
    if !s.chars().any(|c| c.is_ascii_alphanumeric()) {
        return None;
    }
    Some(s)
}

/// `git config --global user.name`。
/// **必须强制 `--global`**：否则在 store 目录内执行会读到仓库级配置，结果随所在目录变化。
fn git_global_user_name() -> Option<String> {
    let out = Command::new("git")
        .args(["config", "--global", "user.name"])
        .output()
        .ok()?;
    if !out.status.success() {
        return None;
    }
    let s = String::from_utf8_lossy(&out.stdout).trim().to_string();
    (!s.is_empty()).then_some(s)
}

/// `whoami`（系统兜底）。
fn whoami_user() -> Option<String> {
    let out = Command::new("whoami").output().ok()?;
    if !out.status.success() {
        return None;
    }
    let s = String::from_utf8_lossy(&out.stdout).trim().to_string();
    (!s.is_empty()).then_some(s)
}

/// 四级解析核心（Q8=A，§4.5）。git/whoami 取值器以闭包注入，便于单测且保证惰性：
/// 前级命中即**不 spawn 进程**。
pub fn resolve_user_with(
    env_user: Option<&str>,
    file_user: Option<&str>,
    git_fn: impl FnOnce() -> Option<String>,
    sys_fn: impl FnOnce() -> Option<String>,
) -> (String, UserSource) {
    if let Some(v) = env_user.and_then(usable_user) {
        return (v, UserSource::Env);
    }
    if let Some(v) = file_user.and_then(usable_user) {
        return (v, UserSource::ConfigFile);
    }
    if let Some(v) = git_fn().and_then(|s| usable_user(&s)) {
        return (v, UserSource::GitGlobal);
    }
    if let Some(v) = sys_fn().and_then(|s| usable_user(&s)) {
        return (v, UserSource::System);
    }
    (FALLBACK_USER.to_string(), UserSource::Fallback)
}

/// 运行期入口：`HANDOFF_USER` → config.toml `user` → `git config --global user.name` → `whoami`。
pub fn resolve_user(file_user: Option<&str>) -> (String, UserSource) {
    let env = std::env::var("HANDOFF_USER").ok();
    resolve_user_with(
        env.as_deref(),
        file_user,
        git_global_user_name,
        whoami_user,
    )
}

impl Default for Config {
    fn default() -> Self {
        let host_alias_store = DEFAULT_STORE_OF
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_string()))
            .collect();
        let (user, user_source) = resolve_user(None);
        Config {
            root: home_dir().join(DEFAULT_REL_ROOT),
            user,
            user_source,
            host_alias_store,
            local_override: HashMap::new(),
        }
    }
}

impl Config {
    /// 本机配置文件路径。
    pub fn config_file() -> PathBuf {
        home_dir().join(".config/handoff/config.toml")
    }

    /// 加载配置（env > 文件 > 默认），任何解析失败都不致命，回退默认。
    /// user_id 走完整四级解析：顶层 `user` 作为第 2 级输入（env 仍高于它）。
    pub fn load() -> Config {
        let mut cfg = Config::default();

        if let Some(root) = std::env::var_os("HANDOFF_ROOT") {
            cfg.root = expand_tilde(&root.to_string_lossy());
        }

        let mut file_user: Option<String> = None;
        if let Ok(text) = std::fs::read_to_string(Self::config_file()) {
            if let Ok(value) = toml::from_str::<toml::Value>(&text) {
                file_user = value
                    .get("user")
                    .and_then(|v| v.as_str())
                    .map(|s| s.to_string());
                cfg.apply(&value);
            }
        }

        let (user, user_source) = resolve_user(file_user.as_deref());
        cfg.user = user;
        cfg.user_source = user_source;
        cfg
    }

    fn apply(&mut self, value: &toml::Value) {
        if let Some(stores) = value.get("stores").and_then(|v| v.as_table()) {
            if let Some(root) = stores.get("root").and_then(|v| v.as_str()) {
                self.root = expand_tilde(root);
            }
            for (store, val) in stores {
                if store == "root" {
                    continue;
                }
                match val {
                    toml::Value::String(alias) => {
                        self.host_alias_store.insert(alias.clone(), store.clone());
                    }
                    toml::Value::Array(items) => {
                        for item in items {
                            if let Some(alias) = item.as_str() {
                                self.host_alias_store.insert(alias.to_string(), store.clone());
                            }
                        }
                    }
                    _ => {}
                }
            }
        }

        if let Some(over) = value.get("local_override").and_then(|v| v.as_table()) {
            for (remote, id) in over {
                if let Some(id) = id.as_str() {
                    self.local_override.insert(remote.clone(), id.to_string());
                }
            }
        }
    }

    /// host_alias → store；未知别名回退 personal（§4.2）。
    pub fn store_of(&self, host_alias: &str) -> String {
        self.host_alias_store
            .get(host_alias)
            .cloned()
            .unwrap_or_else(|| "personal".to_string())
    }

    /// 全部 store 名（去重、有序）。
    pub fn stores(&self) -> Vec<String> {
        let set: BTreeSet<String> = self.host_alias_store.values().cloned().collect();
        set.into_iter().collect()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn default_stores_sorted_and_dedup() {
        let cfg = Config::default();
        // 默认仅 work / personal 两个 store。
        assert_eq!(cfg.stores(), vec!["personal".to_string(), "work".to_string()]);
    }

    #[test]
    fn store_of_known_and_unknown() {
        let cfg = Config::default();
        assert_eq!(cfg.store_of("codeup"), "work");
        assert_eq!(cfg.store_of("gitee"), "personal");
        assert_eq!(cfg.store_of("unknown-alias"), "personal");
    }

    #[test]
    fn apply_overrides_root_and_alias() {
        let mut cfg = Config::default();
        let value: toml::Value = toml::from_str(
            r#"
            [stores]
            root = "/tmp/handoff-x"
            work = "codeup"
            personal = ["gitee", "github", "local"]
            [local_override]
            "git@codeup-sh:iptv/android/v4.git" = "codeup:iptv/android/v4"
            "#,
        )
        .unwrap();
        cfg.apply(&value);
        assert_eq!(cfg.root, PathBuf::from("/tmp/handoff-x"));
        assert_eq!(cfg.store_of("local"), "personal");
        assert_eq!(
            cfg.local_override
                .get("git@codeup-sh:iptv/android/v4.git")
                .unwrap(),
            "codeup:iptv/android/v4"
        );
    }

    #[test]
    fn expand_tilde_works() {
        let p = expand_tilde("~/a/b");
        assert!(p.ends_with("a/b"));
        assert!(!p.to_string_lossy().starts_with('~'));
    }

    #[test]
    fn usable_user_rejects_invalid() {
        assert_eq!(usable_user("yujinping").as_deref(), Some("yujinping"));
        assert_eq!(usable_user("Yujin Ping").as_deref(), Some("yujin-ping"));
        assert_eq!(usable_user("a.b_c-d").as_deref(), Some("a.b_c-d"));
        // 中文 → sanitize 后全是 `-` → 无效（§4.5 的关键边界）
        assert_eq!(usable_user("于金平"), None);
        assert_eq!(usable_user(""), None);
        assert_eq!(usable_user("---"), None);
        // 不以 `.` 开头（避免生成隐藏目录）
        assert_eq!(usable_user(".hidden"), None);
    }

    #[test]
    fn user_fallback_chain_order() {
        assert_eq!(
            resolve_user_with(
                Some("alice"),
                Some("bob"),
                || Some("carol".to_string()),
                || Some("dave".to_string())
            ),
            ("alice".to_string(), UserSource::Env)
        );
        assert_eq!(
            resolve_user_with(
                None,
                Some("bob"),
                || Some("carol".to_string()),
                || Some("dave".to_string())
            ),
            ("bob".to_string(), UserSource::ConfigFile)
        );
        assert_eq!(
            resolve_user_with(
                None,
                None,
                || Some("carol".to_string()),
                || Some("dave".to_string())
            ),
            ("carol".to_string(), UserSource::GitGlobal)
        );
        assert_eq!(
            resolve_user_with(None, None, || None, || Some("dave".to_string())),
            ("dave".to_string(), UserSource::System)
        );
        assert_eq!(
            resolve_user_with(None, None, || None, || None),
            (FALLBACK_USER.to_string(), UserSource::Fallback)
        );
    }

    #[test]
    fn invalid_candidate_degrades_to_next_level() {
        // 中文 git user.name → sanitize 后无效 → 降级到 whoami
        assert_eq!(
            resolve_user_with(
                None,
                None,
                || Some("于金平".to_string()),
                || Some("dave".to_string())
            ),
            ("dave".to_string(), UserSource::System)
        );
        // 空串同样降级
        assert_eq!(
            resolve_user_with(
                Some(""),
                Some(""),
                || Some(String::new()),
                || Some("dave".to_string())
            ),
            ("dave".to_string(), UserSource::System)
        );
    }

    #[test]
    fn user_is_never_empty() {
        // 无论环境如何，user 必须非空且非隐藏名——否则路径会塌缩为 users//tasks
        let cfg = Config::default();
        assert!(!cfg.user.is_empty());
        assert!(!cfg.user.starts_with('.'));
    }

    #[test]
    fn top_level_user_key_is_read() {
        let value: toml::Value = toml::from_str(r#"user = "alice""#).unwrap();
        let file_user = value.get("user").and_then(|v| v.as_str());
        assert_eq!(file_user, Some("alice"));
        // 非字符串类型不应被误取
        let bad: toml::Value = toml::from_str("user = 42").unwrap();
        assert!(bad.get("user").and_then(|v| v.as_str()).is_none());
    }
}
