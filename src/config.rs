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

/// 运行期配置。
#[derive(Debug, Clone)]
pub struct Config {
    /// store 根容器目录（其下为 work/ personal/ 等独立仓库）。
    pub root: PathBuf,
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

impl Default for Config {
    fn default() -> Self {
        let host_alias_store = DEFAULT_STORE_OF
            .iter()
            .map(|(k, v)| (k.to_string(), v.to_string()))
            .collect();
        Config {
            root: home_dir().join(DEFAULT_REL_ROOT),
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
    pub fn load() -> Config {
        let mut cfg = Config::default();

        if let Some(root) = std::env::var_os("HANDOFF_ROOT") {
            cfg.root = expand_tilde(&root.to_string_lossy());
        }

        if let Ok(text) = std::fs::read_to_string(Self::config_file()) {
            if let Ok(value) = toml::from_str::<toml::Value>(&text) {
                cfg.apply(&value);
            }
        }
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
}
