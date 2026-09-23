// Handoff / 交接 — 错误类型
//
// @author yujinping
// @design 仅承载「必须显式区分」的领域错误；其余路径错误统一用 anyhow 透传。
// 信任域违规是安全边界错误，单独建模便于 scan/save 精确判定与提示。

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
