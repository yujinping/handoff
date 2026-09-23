// Handoff / 交接 — CLI 子命令定义（clap）
//
// @author yujinping
// @design §7：CLI 只做传输与标识解析；子命令 id/where/new/show/list/save/sync/scan。
// target 允许两种书写：
//   - 显式 `<host_alias>:<ns>/<repo...>/<slug>`（末段为 slug，脚本内推荐）
//   - 仅 `<slug>`（project_id 从当前目录派生，交互使用）

use clap::{Parser, Subcommand};
use std::path::PathBuf;

/// 跨客户端会话交接 CLI：本地 markdown 任务文档 + git 同步。
#[derive(Parser, Debug)]
#[command(name = "handoff", version, about = "跨客户端会话交接 CLI：本地 markdown 任务文档 + git 同步")]
pub struct Cli {
    #[command(subcommand)]
    pub cmd: Cmd,
}

#[derive(Subcommand, Debug)]
pub enum Cmd {
    /// 派生当前目录（或 --path）的 project_id
    Id {
        /// 指定目录（默认当前目录）
        #[arg(long)]
        path: Option<PathBuf>,
    },
    /// 打印当前 user_id 与其命中级（§4.5 四级解析诊断）
    Whoami,
    /// 显示 project 的归属 store 与磁盘路径
    Where {
        /// 全量 project_id，如 codeup:iptv/android/v4
        project_id: String,
    },
    /// 从模板创建任务交接文档
    New {
        /// <project_id>/<slug> 或仅 <slug>（后者从当前目录派生 project_id）
        target: String,
        /// monorepo 模块（写入 front-matter scope）
        #[arg(long)]
        scope: Option<String>,
    },
    /// 打印任务文档（供任意客户端注入上下文）
    Show {
        /// <project_id>/<slug> 或仅 <slug>
        target: String,
    },
    /// 列出任务
    List {
        /// 仅列出未完成（status != done）
        #[arg(long)]
        open: bool,
        /// 限定 store（默认全部）
        #[arg(long)]
        store: Option<String>,
    },
    /// 提交并推送任务文档（含信任域与敏感信息校验）
    Save {
        /// <project_id>/<slug> 或仅 <slug>
        target: String,
        /// 提交信息
        #[arg(short, long)]
        message: String,
    },
    /// 同步 store 仓库：pull --rebase && push
    Sync {
        /// 限定 store（默认全部）
        #[arg(long)]
        store: Option<String>,
    },
    /// 敏感信息 + 信任域归属扫描
    Scan {
        /// 限定 store（默认全部）
        #[arg(long)]
        store: Option<String>,
    },
    /// 把 SKILL.md 安装（符号链接）到各客户端技能目录
    InstallSkill {
        /// 技能源目录（默认：本二进制的编译期工具仓库路径）
        #[arg(long)]
        from: Option<PathBuf>,
        /// 指定技能根目录（可多次）；缺省用内置的 WorkBuddy + Zed/Codex
        #[arg(long = "root")]
        roots: Vec<PathBuf>,
    },
}
