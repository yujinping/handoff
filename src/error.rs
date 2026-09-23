// Handoff / 交接 — 错误类型
//
// @author yujinping
// @design 仅承载「必须显式区分」的领域错误；其余路径错误统一用 anyhow 透传。
// 两道归属违规分别建模，便于 scan/save 精确判定与提示（§11 / §4.5）：
//   - 信任域违规（跨 store）——防外泄；
//   - owner 违规（同 store 内跨 users 命名空间）——防误写。

/// 信任域违规：文档的 project_id 归属与所在 store 不一致。
#[derive(Debug, thiserror::Error)]
#[error("信任域违规：project_id `{project_id}` 归属 store `{expected}`，但文档位于 store `{actual}`")]
pub struct TrustDomainViolation {
    /// 文档 front-matter 中的 project_id。
    pub project_id: String,
    /// project_id 应归属的 store。
    pub expected: String,
    /// 文档实际所在的 store。
    pub actual: String,
}

/// owner 违规：文档 `owner` 与所在 `users/<user_id>/` 命名空间不符（§4.5）。
///
/// 拦截两类真实事故：① 在他人命名空间内续写；② 改 `owner` 却未平移目录（或反之）。
#[derive(Debug, thiserror::Error)]
#[error("owner 归属违规：文档 owner `{owner}` 与所在命名空间 `users/{expected}/` 不符（{}）", file.display())]
pub struct OwnerMismatch {
    /// 文档 front-matter 中的 owner。
    pub owner: String,
    /// 文档实际所在命名空间的 user_id。
    pub expected: String,
    /// 文档路径。
    pub file: std::path::PathBuf,
}
