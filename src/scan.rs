// Handoff / 交接 — 扫描与信任域归属校验
//
// @author yujinping
// @design §11 安全：
//   - 归属校验（关键）：文档 front-matter 的 project_id 归属 store 必须与文档所在 store 一致，
//     不一致即视为跨信任域写入（如 `codeup:` 项目落入 personal store），save 时阻断。
//   - 敏感信息扫描：保守关键字命中（私钥头、常见 token/secret 前缀），push 前拦截。
// 关键字匹配刻意用 `str::contains`（不引正则库，§0.3）。

use crate::config::Config;
use crate::error::TrustDomainViolation;
use crate::id::{parse_project_id, ProjectId};
use crate::store;
use anyhow::{Context, Result};
use std::path::{Path, PathBuf};

/// 扫描发现项。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Finding {
    /// 命中文件。
    pub file: PathBuf,
    /// 类别：`trust-domain` | `secret` | `unparsable`。
    pub kind: String,
    /// 细节说明。
    pub detail: String,
}

/// 敏感信息关键字（保守、低误报）。
pub const SECRET_KEYS: &[&str] = &[
    "BEGIN RSA PRIVATE KEY",
    "BEGIN OPENSSH PRIVATE KEY",
    "BEGIN PRIVATE KEY",
    "password=",
    "passwd=",
    "secret=",
    "token=",
    "api_key=",
    "apikey=",
    "AKIA",
    "ghp_",
    "sk-",
];

/// 递归收集目录下全部 `.md` 文件。
pub fn walk_md(dir: &Path, out: &mut Vec<PathBuf>) {
    let Ok(rd) = std::fs::read_dir(dir) else {
        return;
    };
    for entry in rd.flatten() {
        let p = entry.path();
        if p.is_dir() {
            walk_md(&p, out);
        } else if p.extension().map(|x| x == "md").unwrap_or(false) {
            out.push(p);
        }
    }
}

/// 抽取 front-matter 字段：匹配首个 `- <key>: value` 行（值非空）。
pub fn extract_field(content: &str, key: &str) -> Option<String> {
    let prefix = format!("- {key}:");
    for line in content.lines() {
        let t = line.trim();
        if let Some(rest) = t.strip_prefix(&prefix) {
            let v = rest.trim();
            if !v.is_empty() {
                return Some(v.to_string());
            }
        }
    }
    None
}

/// project_id 归属是否与所在 store 一致。
pub fn belongs_to_store(pid: &ProjectId, cfg: &Config, store_name: &str) -> bool {
    pid.store(cfg) == store_name
}

/// 命中敏感信息的行摘要列表。
pub fn secret_hits(content: &str) -> Vec<String> {
    let mut hits = Vec::new();
    for (i, line) in content.lines().enumerate() {
        for key in SECRET_KEYS {
            if line.contains(key) {
                hits.push(format!("L{} `{}`", i + 1, key));
            }
        }
    }
    hits
}

/// 校验单个文档的信任域归属；违规返回 `TrustDomainViolation`。
/// 无 `project_id` 字段时不判定归属（交由 scan 报告缺失）。
pub fn check_trust_domain(
    content: &str,
    cfg: &Config,
    actual_store: &str,
    file: &Path,
) -> Result<()> {
    let Some(raw) = extract_field(content, "project_id") else {
        return Ok(());
    };
    let pid = parse_project_id(&raw)
        .with_context(|| format!("{} 的 project_id 无法解析", file.display()))?;
    let expected = pid.store(cfg);
    if expected != actual_store {
        return Err(TrustDomainViolation {
            project_id: raw,
            expected,
            actual: actual_store.to_string(),
        }
        .into());
    }
    Ok(())
}

/// 扫描单个 store 的 tasks 目录：归属违规 + 敏感信息。
pub fn scan_store(root: &Path, cfg: &Config, store_name: &str) -> Vec<Finding> {
    let mut findings = Vec::new();
    let mut files = Vec::new();
    walk_md(&store::tasks_dir(root, store_name), &mut files);
    files.sort();

    for file in files {
        let Ok(content) = std::fs::read_to_string(&file) else {
            continue;
        };

        // 归属校验
        if let Some(raw) = extract_field(&content, "project_id") {
            match parse_project_id(&raw) {
                Ok(pid) => {
                    if !belongs_to_store(&pid, cfg, store_name) {
                        findings.push(Finding {
                            file: file.clone(),
                            kind: "trust-domain".into(),
                            detail: format!(
                                "project_id `{raw}` 归属 `{}`，但位于 store `{store_name}`",
                                pid.store(cfg)
                            ),
                        });
                    }
                }
                Err(e) => findings.push(Finding {
                    file: file.clone(),
                    kind: "unparsable".into(),
                    detail: format!("project_id `{raw}` 无法解析：{e}"),
                }),
            }
        } else {
            findings.push(Finding {
                file: file.clone(),
                kind: "trust-domain".into(),
                detail: "缺少 project_id 字段".into(),
            });
        }

        // 敏感信息
        for detail in secret_hits(&content) {
            findings.push(Finding {
                file: file.clone(),
                kind: "secret".into(),
                detail,
            });
        }
    }
    findings
}

#[cfg(test)]
mod tests {
    use super::*;

    const DOC: &str = "# codeup:iptv/android/v4 / x\n- project_id: codeup:iptv/android/v4\n- status: in-progress\n";

    #[test]
    fn extract_field_works() {
        assert_eq!(
            extract_field(DOC, "project_id").unwrap(),
            "codeup:iptv/android/v4"
        );
        assert_eq!(extract_field(DOC, "status").unwrap(), "in-progress");
        assert!(extract_field(DOC, "missing").is_none());
    }

    #[test]
    fn belongs_to_store_matches_host_domain() {
        let cfg = Config::default();
        let pid = parse_project_id("codeup:iptv/android/v4").unwrap();
        assert!(belongs_to_store(&pid, &cfg, "work"));
        assert!(!belongs_to_store(&pid, &cfg, "personal"));
    }

    #[test]
    fn trust_domain_violation_blocks_cross_store() {
        let cfg = Config::default();
        // codeup 项目文档放在 personal store → 违规
        let err = check_trust_domain(DOC, &cfg, "personal", Path::new("p/x.md"));
        assert!(err.is_err());
        // 放在 work store → 通过
        assert!(check_trust_domain(DOC, &cfg, "work", Path::new("w/x.md")).is_ok());
    }

    #[test]
    fn secret_detection() {
        let bad = "- env: token=abcdefghijklmnop\n- note: -----BEGIN RSA PRIVATE KEY-----\n";
        let hits = secret_hits(bad);
        assert!(hits.iter().any(|h| h.contains("token=")));
        assert!(hits.iter().any(|h| h.contains("RSA PRIVATE KEY")));
        assert!(secret_hits(DOC).is_empty());
    }
}
