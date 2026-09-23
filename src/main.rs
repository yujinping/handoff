// Handoff / 交接 — 跨客户端会话交接 CLI
//
// @author yujinping
// @design 详见 handoff-system-plan.md（§0 基础设定 / §7 模块划分）
//
// P1 骨架：仅占位，打印用法。完整实现见 P2（id/where/new/show/list/save/sync/scan）。
// 设计意图：本工具为纯 CLI、单二进制、非服务；真相源是 git 仓库里的 markdown，
// CLI 只负责标识解析（project_id 派生）与传输（git pull/push），不持有状态。

fn main() {
    eprintln!("handoff: 跨客户端会话交接 CLI（P1 骨架）");
    eprintln!("  状态：仓库骨架已建，CLI 逻辑待 P2 实现");
    eprintln!("  计划子命令：id / where / new / show / list / save / sync / scan");
    eprintln!("  详见 handoff-system-plan.md §7");
    std::process::exit(0);
}
