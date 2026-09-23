// Handoff / 交接 — project_id 派生
//
// @author yujinping
// @design 可靠性基石（§4.2 / §7）：project_id 必须由 remote **确定性派生**，任何机器/客户端结果一致，
// 不依赖模型判断。派生流程：取 remote → 归一化（去 scheme/凭据/`.git`、scp 转路径、去端口）→
// host 小写 → 别名表映射 → 路径逐段 sanitize → 校验 ≥2 段。
// SSH 别名处理：remote 的 host 段可能是 `~/.ssh/config` 别名而非真实域名，故对未知 host 先用
// `ssh -G <alias>` 解析真实 hostname，再匹配别名表；解析失败则以别名本身充当 host_alias。

use crate::config::{Config, DEFAULT_ALIASES};
use crate::repo;
use anyhow::{bail, Context, Result};
use std::path::Path;

/// 项目标识：`<host_alias>:<path...>`。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProjectId {
    /// 托管域别名（codeup / gitee / github / local / …）。
    pub host_alias: String,
    /// 仓库在该托管域内的路径分段（仅仓库级，不含 monorepo 模块）。
    pub path: Vec<String>,
}

impl ProjectId {
    /// 构造（不做校验；校验在派生 / 解析处完成）。
    pub fn new(host_alias: impl Into<String>, path: Vec<String>) -> Self {
        Self {
            host_alias: host_alias.into(),
            path,
        }
    }

    /// 标准书写：`codeup:iptv/android/v4`。
    pub fn as_str(&self) -> String {
        format!("{}:{}", self.host_alias, self.path.join("/"))
    }

    /// 归属 store。
    pub fn store(&self, cfg: &Config) -> String {
        cfg.store_of(&self.host_alias)
    }
}

/// 单段 sanitize：小写 + `[^a-z0-9._-]` → `-`。
pub fn sanitize_seg(seg: &str) -> String {
    let lower = seg.to_lowercase();
    let mut out = String::with_capacity(lower.len());
    for ch in lower.chars() {
        if ch.is_ascii_alphanumeric() || matches!(ch, '.' | '_' | '-') {
            out.push(ch);
        } else {
            out.push('-');
        }
    }
    out
}

/// 归一化 remote url → (host, path 分段)。纯函数，便于单测。
///
/// 支持形态：`git@host:path`（scp）、`https://host/path`、`ssh://git@host:port/path`。
pub fn normalize_remote(url: &str) -> Option<(String, Vec<String>)> {
    let raw = url.trim();
    let has_scheme = raw.contains("://");
    let mut s = raw.to_string();
    if has_scheme {
        let idx = s.find("://")?;
        s = s[idx + 3..].to_string();
    }

    // 去 user[:pass]@（仅当 `@` 位于 host 段，即首个 `/` 或 `:` 之前）。
    if let Some(at) = s.find('@') {
        let boundary = s.find(['/', ':']).unwrap_or(s.len());
        if at < boundary {
            s = s[at + 1..].to_string();
        }
    }

    let (host, path_str) = if has_scheme {
        // host[:port]/path
        let i = s.find('/')?;
        let hostport = &s[..i];
        let host = hostport.split(':').next().unwrap_or(hostport).to_string();
        (host, s[i + 1..].to_string())
    } else {
        // scp: host:path
        let i = s.find(':')?;
        (s[..i].to_string(), s[i + 1..].to_string())
    };

    if host.is_empty() {
        return None;
    }

    let mut p = path_str.trim_end_matches('/').to_string();
    if let Some(stripped) = p.strip_suffix(".git") {
        p = stripped.to_string();
    }
    let p = p.trim_end_matches('/');
    let path: Vec<String> = p
        .split('/')
        .filter(|x| !x.is_empty())
        .map(sanitize_seg)
        .collect();
    if path.len() < 2 {
        return None;
    }
    Some((host.to_lowercase(), path))
}

/// host → host_alias（别名表；未知取域名首段 sanitize）。
pub fn host_alias_for(host: &str) -> String {
    for (h, alias) in DEFAULT_ALIASES {
        if *h == host {
            return (*alias).to_string();
        }
    }
    let first = host.split('.').next().unwrap_or(host);
    sanitize_seg(first)
}

/// 是否已知托管域（可直接匹配别名表，无需 ssh 解析）。
pub fn is_known_host(host: &str) -> bool {
    DEFAULT_ALIASES.iter().any(|(h, _)| *h == host)
}

/// 由 remote url 派生 project_id（纯派生，不含 override）。
pub fn derive_from_remote(url: &str) -> Option<ProjectId> {
    let (host, path) = normalize_remote(url)?;
    Some(ProjectId::new(host_alias_for(&host), path))
}

/// 用 `ssh -G <alias>` 解析 SSH 别名对应的真实 hostname。
pub fn resolve_ssh_host(alias: &str) -> Option<String> {
    let out = std::process::Command::new("ssh")
        .arg("-G")
        .arg(alias)
        .output()
        .ok()?;
    if !out.status.success() {
        return None;
    }
    let text = String::from_utf8_lossy(&out.stdout);
    for line in text.lines() {
        if let Some(rest) = line.strip_prefix("hostname ") {
            let v = rest.trim();
            if !v.is_empty() {
                return Some(v.to_string());
            }
        }
    }
    None
}

/// 解析 project_id 字符串（用于 `where` 参数与 local_override 值）。
pub fn parse_project_id(s: &str) -> Result<ProjectId> {
    let (host, rest) = s
        .split_once(':')
        .with_context(|| format!("project_id `{s}` 需形如 <host_alias>:<ns>/<repo>"))?;
    if host.is_empty() {
        bail!("project_id `{s}` 缺少 host_alias 前缀");
    }
    let path: Vec<String> = rest
        .split('/')
        .filter(|x| !x.is_empty())
        .map(|x| x.to_string())
        .collect();
    if path.is_empty() {
        bail!("project_id `{s}` 缺少路径");
    }
    Ok(ProjectId::new(host.to_string(), path))
}

/// 由目录派生 project_id（含 git remote 探测 + local_override 优先 + ssh 别名解析）。
pub fn derive_from_dir(dir: &Path, cfg: &Config) -> Result<ProjectId> {
    let Some(remote) = repo::remote_url(dir) else {
        // 无 remote → local:<目录基名>（§4.3）
        let base = dir
            .file_name()
            .and_then(|s| s.to_str())
            .unwrap_or("unknown");
        return Ok(ProjectId::new("local", vec![sanitize_seg(base)]));
    };

    if let Some(id) = cfg.local_override.get(&remote) {
        return parse_project_id(id);
    }

    let (host_raw, path) = normalize_remote(&remote)
        .with_context(|| format!("无法从 remote 派生 project_id：`{remote}`"))?;

    // 已知托管域：纯派生（复用 derive_from_remote 的同一逻辑）。
    if is_known_host(&host_raw) {
        return derive_from_remote(&remote)
            .with_context(|| format!("无法从 remote 派生 project_id：`{remote}`"));
    }

    // 未知 host：可能是 `~/.ssh/config` 别名，先解析真实 hostname 再匹配别名表（§4.2）；
    // 解析失败则以别名本身充当 host_alias，要求在注册表固定 canonical。
    let host_alias = resolve_ssh_host(&host_raw)
        .map(|h| host_alias_for(&h))
        .unwrap_or_else(|| host_alias_for(&host_raw));
    Ok(ProjectId::new(host_alias, path))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn scp_codeup() {
        let pid = derive_from_remote("git@codeup.aliyun.com:iptv/android/v4.git").unwrap();
        assert_eq!(pid.as_str(), "codeup:iptv/android/v4");
        assert_eq!(pid.store(&Config::default()), "work");
    }

    #[test]
    fn https_gitee() {
        let pid = derive_from_remote("https://gitee.com/yujinping8620/memora.git").unwrap();
        assert_eq!(pid.as_str(), "gitee:yujinping8620/memora");
        assert_eq!(pid.store(&Config::default()), "personal");
    }

    #[test]
    fn ssh_scheme_with_port() {
        let pid = derive_from_remote("ssh://git@example.com:2222/team/tool.git").unwrap();
        assert_eq!(pid.as_str(), "example:team/tool");
    }

    #[test]
    fn case_and_sanitize() {
        let pid = derive_from_remote("git@Codeup.Aliyun.com:IPTV/Android V4.git").unwrap();
        assert_eq!(pid.as_str(), "codeup:iptv/android-v4");
    }

    #[test]
    fn trailing_slash_and_no_git_suffix() {
        let pid = derive_from_remote("https://gitee.com/yujinping8620/memora/").unwrap();
        assert_eq!(pid.as_str(), "gitee:yujinping8620/memora");
    }

    #[test]
    fn path_too_short_is_none() {
        assert!(derive_from_remote("git@host.com:onlyone.git").is_none());
        assert!(normalize_remote("https://gitee.com/").is_none());
    }

    #[test]
    fn parse_project_id_ok_and_err() {
        let pid = parse_project_id("codeup:iptv/android/v4").unwrap();
        assert_eq!(pid.host_alias, "codeup");
        assert_eq!(pid.path, vec!["iptv", "android", "v4"]);
        assert!(parse_project_id("no-colon").is_err());
        assert!(parse_project_id("codeup:").is_err());
    }

    #[test]
    fn standalone_repo_is_repo_level_not_module() {
        // monorepo 下 project_id 保持仓库级：派生只看 remote，与模块无关（§4.3）。
        let pid = derive_from_remote("git@codeup.aliyun.com:iptv/android/v4.git").unwrap();
        assert!(!pid.path.iter().any(|s| s == "v4-core"));
        assert_eq!(pid.path.last().unwrap(), "v4");
    }

    #[test]
    fn local_when_no_remote() {
        let dir = std::env::temp_dir().join("handoff-no-remote-dir");
        std::fs::create_dir_all(&dir).unwrap();
        let pid = derive_from_dir(&dir, &Config::default()).unwrap();
        assert!(pid.as_str().starts_with("local:"));
        assert_eq!(pid.store(&Config::default()), "personal");
    }

    #[test]
    fn local_override_wins() {
        let mut cfg = Config::default();
        cfg.local_override.insert(
            "git@codeup-sh:iptv/android/v4.git".to_string(),
            "codeup:iptv/android/v4".to_string(),
        );
        assert_eq!(
            cfg.local_override
                .get("git@codeup-sh:iptv/android/v4.git")
                .unwrap(),
            "codeup:iptv/android/v4"
        );
    }
}
