// Handoff / 交接 — 技能安装（跨客户端部署）
//
// @author yujinping
// @design 计划 §8：SKILL.md 是开放标准（Agent Skills），同一份文件放进各客户端技能根目录即可复用。
// 为避免「多处副本漂移」，unix 平台把客户端技能目录里的 `handoff` 项**符号链接**到工具仓库根
// （即 SKILL.md 所在目录），实现「一处修改、处处生效」。
// Windows 上符号链接需管理员/开发者模式，普通用户不可靠，改用**复制目录**方式安装：
// 语义与符号链接一致（幂等 + 冲突不覆盖），代价是升级需重跑 install-skill 刷新副本。
//
// 已核实的技能根目录（2026-09）：
//   - WorkBuddy：`~/.workbuddy/skills/`
//   - Zed：`~/.agents/skills/`（全局）或 `<worktree>/.agents/skills/`（项目级）
//   - Codex CLI：`~/.agents/skills/`
// 即 **Zed 与 Codex CLI 共用 `~/.agents/skills/`**，故默认只需维护两个根目录。

use anyhow::{bail, Context, Result};
use std::path::{Path, PathBuf};

/// 技能项名（同时是符号链接名与 SKILL.md 的 `name` 字段，须一致）。
pub const SKILL_NAME: &str = "handoff";

/// 一个目标技能根目录及其所属客户端（仅用于输出可读性）。
pub struct SkillRoot {
    /// 客户端名称（WorkBuddy / Zed+Codex / 自定义）
    pub client: String,
    /// 技能根目录的绝对路径
    pub path: PathBuf,
}

/// 单个技能根的安装结果。
pub enum LinkState {
    /// 本次新建了符号链接
    Created,
    /// 已存在且指向正确源，无需处理（幂等）
    AlreadyLinked,
    /// 已存在但指向别处或为实体目录 → 不覆盖，交由用户处理
    Conflict(String),
}

/// 默认技能根：WorkBuddy 与 agents（Zed + Codex 共用）。
/// home 目录跨平台解析见 config::home_dir（unix: $HOME；windows: %USERPROFILE%）。
pub fn default_roots() -> Result<Vec<SkillRoot>> {
    let home = crate::config::home_dir();
    if home == Path::new(".") {
        bail!("未定位到 home 目录，无法确定技能根；请用 --root 显式指定");
    }
    Ok(vec![
        SkillRoot {
            client: "WorkBuddy".to_string(),
            path: home.join(".workbuddy").join("skills"),
        },
        SkillRoot {
            client: "Zed+Codex".to_string(),
            path: home.join(".agents").join("skills"),
        },
    ])
}

/// 用显式路径构造技能根列表（`--root` 覆盖默认）。
pub fn roots_from(paths: &[PathBuf]) -> Vec<SkillRoot> {
    paths
        .iter()
        .map(|p| SkillRoot {
            client: "custom".to_string(),
            path: p.clone(),
        })
        .collect()
}

/// 校验技能源目录：必须存在且含 `SKILL.md`。
pub fn resolve_source(from: &Path) -> Result<PathBuf> {
    let src = std::fs::canonicalize(from)
        .with_context(|| format!("技能源目录不存在或不可访问：{}", from.display()))?;
    let skill_md = src.join("SKILL.md");
    if !skill_md.is_file() {
        bail!("技能源目录缺少 SKILL.md：{}", skill_md.display());
    }
    Ok(src)
}

/// 在 `root` 下安装（符号链接）技能项。幂等：已正确链接则跳过，冲突则报错不覆盖。
#[cfg(unix)]
pub fn install_one(src: &Path, root: &Path) -> Result<LinkState> {
    std::fs::create_dir_all(root)
        .with_context(|| format!("无法创建/访问技能根目录：{}", root.display()))?;
    let link = root.join(SKILL_NAME);

    // 已存在：区分「软链指向正确」与「冲突」。
    if let Ok(meta) = std::fs::symlink_metadata(&link) {
        if meta.file_type().is_symlink() {
            let target = std::fs::read_link(&link)
                .with_context(|| format!("读取符号链接失败：{}", link.display()))?;
            let target_abs = if target.is_absolute() {
                target.clone()
            } else {
                root.join(&target)
            };
            let same = std::fs::canonicalize(&target_abs)
                .map(|t| t == src)
                .unwrap_or(false);
            return if same {
                Ok(LinkState::AlreadyLinked)
            } else {
                Ok(LinkState::Conflict(format!(
                    "已存在符号链接但指向 {}",
                    target.display()
                )))
            };
        }
        return Ok(LinkState::Conflict("已存在同名实体（非符号链接）".to_string()));
    }

    std::os::unix::fs::symlink(src, &link)
        .with_context(|| format!("创建符号链接失败：{}", link.display()))?;
    Ok(LinkState::Created)
}

/// 非 Unix 平台：符号链接需管理员/开发者模式，改用**复制目录**安装（语义等价的幂等）。
/// 已存在且内容一致 → AlreadyLinked；已存在但不一致 → Conflict（不覆盖，交由用户处理）。
#[cfg(not(unix))]
pub fn install_one(src: &Path, root: &Path) -> Result<LinkState> {
    std::fs::create_dir_all(root)
        .with_context(|| format!("无法创建/访问技能根目录：{}", root.display()))?;
    let dest = root.join(SKILL_NAME);

    // 已存在：区分「内容一致（幂等）」与「冲突（不覆盖）」。
    if dest.exists() {
        return if dirs_equal(src, &dest) {
            Ok(LinkState::AlreadyLinked)
        } else {
            Ok(LinkState::Conflict(
                "已存在同名目录且内容与源不一致（不覆盖）".to_string(),
            ))
        };
    }

    copy_tree(src, &dest)
        .with_context(|| format!("复制技能目录失败：{} → {}", src.display(), dest.display()))?;
    Ok(LinkState::Created)
}

/// 递归复制目录树（复制实现专用）。
#[cfg(not(unix))]
fn copy_tree(src: &Path, dest: &Path) -> std::io::Result<()> {
    std::fs::create_dir_all(dest)?;
    for entry in std::fs::read_dir(src)? {
        let entry = entry?;
        let from = entry.path();
        let to = dest.join(entry.file_name());
        if from.is_dir() {
            copy_tree(&from, &to)?;
        } else {
            std::fs::copy(&from, &to)?;
        }
    }
    Ok(())
}

/// 递归比较两个目录树是否完全一致（文件名 + 文件内容）。
#[cfg(not(unix))]
fn dirs_equal(a: &Path, b: &Path) -> bool {
    let (Ok(ra), Ok(rb)) = (std::fs::read_dir(a), std::fs::read_dir(b)) else {
        return false;
    };
    let mut names_a: Vec<_> = ra.flatten().map(|e| e.file_name()).collect();
    let mut names_b: Vec<_> = rb.flatten().map(|e| e.file_name()).collect();
    names_a.sort();
    names_b.sort();
    if names_a != names_b {
        return false;
    }
    for name in names_a {
        let pa = a.join(&name);
        let pb = b.join(&name);
        if pa.is_dir() {
            if !pb.is_dir() || !dirs_equal(&pa, &pb) {
                return false;
            }
        } else if std::fs::read(pa).ok() != std::fs::read(pb).ok() {
            return false;
        }
    }
    true
}

/// 对多个技能根依次安装，返回 (根, 结果) 列表。
pub fn install_all(src: &Path, roots: &[SkillRoot]) -> Vec<(String, PathBuf, Result<LinkState>)> {
    roots
        .iter()
        .map(|r| {
            (
                r.client.clone(),
                r.path.clone(),
                install_one(src, &r.path),
            )
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;

    /// 建立唯一的临时目录（同进程内多个测试并行亦不冲突）。
    fn tmp(name: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!("handoff-skill-{}-{}", std::process::id(), name));
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&dir).unwrap();
        dir
    }

    /// 造一个最小的技能源目录（含 SKILL.md）。
    fn fake_skill(dir: &Path) -> PathBuf {
        let src = dir.join("src-skill");
        fs::create_dir_all(&src).unwrap();
        fs::write(src.join("SKILL.md"), "---\nname: handoff\n---\n").unwrap();
        src
    }

    #[test]
    fn resolve_source_requires_skill_md() {
        let d = tmp("resolve");
        let empty = d.join("empty");
        fs::create_dir_all(&empty).unwrap();
        assert!(resolve_source(&empty).is_err());

        let src = fake_skill(&d);
        assert!(resolve_source(&src).is_ok());
    }

    #[test]
    fn install_creates_then_is_idempotent() {
        let d = tmp("idem");
        let src = resolve_source(&fake_skill(&d)).unwrap();
        let root = d.join("skills");

        let first = install_one(&src, &root).unwrap();
        assert!(matches!(first, LinkState::Created));
        assert!(root.join(SKILL_NAME).join("SKILL.md").is_file());

        let second = install_one(&src, &root).unwrap();
        assert!(matches!(second, LinkState::AlreadyLinked));
    }

    #[test]
    fn install_reports_conflict_without_clobber() {
        let d = tmp("conflict");
        let src = resolve_source(&fake_skill(&d)).unwrap();
        let root = d.join("skills");
        // 先占位一个实体目录
        fs::create_dir_all(root.join(SKILL_NAME)).unwrap();
        fs::write(root.join(SKILL_NAME).join("SKILL.md"), "占位").unwrap();

        let st = install_one(&src, &root).unwrap();
        assert!(matches!(st, LinkState::Conflict(_)));
        // 原内容未被覆盖
        let kept = fs::read_to_string(root.join(SKILL_NAME).join("SKILL.md")).unwrap();
        assert_eq!(kept, "占位");
    }

    #[test]
    fn roots_from_labels_custom() {
        let rs = roots_from(&[PathBuf::from("/tmp/x")]);
        assert_eq!(rs.len(), 1);
        assert_eq!(rs[0].client, "custom");
    }
}
