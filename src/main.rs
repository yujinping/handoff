// Handoff / 交接 — 入口与命令分发
//
// @author yujinping
// @design 详见 handoff-system-plan.md（§0 基础设定 / §7 模块划分）。
// 本工具为纯 CLI、单二进制、非服务；真相源是 git 仓库里的 markdown，
// CLI 只负责标识解析（project_id 派生）与传输（git pull/push），不持有状态。

mod cli;
mod config;
mod error;
mod id;
mod repo;
mod scan;
mod skill;
mod store;
mod template;

use anyhow::{bail, Context, Result};
use clap::Parser;
use std::path::PathBuf;

use crate::cli::{Cli, Cmd};
use crate::config::Config;
use crate::id::ProjectId;

fn main() {
    if let Err(e) = run() {
        eprintln!("error: {:#}", e);
        std::process::exit(1);
    }
}

fn run() -> Result<()> {
    let args = Cli::parse();
    let cfg = Config::load();
    match args.cmd {
        Cmd::Id { path } => cmd_id(&cfg, path),
        Cmd::Where { project_id } => cmd_where(&cfg, &project_id),
        Cmd::New { target, scope } => cmd_new(&cfg, &target, scope.as_deref()),
        Cmd::Show { target } => cmd_show(&cfg, &target),
        Cmd::List { open, store } => cmd_list(&cfg, open, store.as_deref()),
        Cmd::Save { target, message } => cmd_save(&cfg, &target, &message),
        Cmd::Sync { store } => cmd_sync(&cfg, store.as_deref()),
        Cmd::Scan { store } => cmd_scan(&cfg, store.as_deref()),
        Cmd::InstallSkill { from, roots } => cmd_install_skill(from, &roots),
    }
}

/// 解析 target → (ProjectId, slug)。
/// 含 `:` 视为显式全量（末段为 slug）；否则视为 slug，project_id 从当前目录派生。
fn resolve_target(cfg: &Config, target: &str) -> Result<(ProjectId, String)> {
    if let Some((host, rest)) = target.split_once(':') {
        let segs: Vec<&str> = rest.split('/').filter(|s| !s.is_empty()).collect();
        if segs.len() < 3 {
            bail!(
                "显式 target 需形如 <host_alias>:<ns>/<repo>/<slug>（当前 `{target}`）"
            );
        }
        let slug = segs[segs.len() - 1].to_string();
        let path: Vec<String> = segs[..segs.len() - 1].iter().map(|s| s.to_string()).collect();
        Ok((ProjectId::new(host.to_string(), path), slug))
    } else {
        let dir = std::env::current_dir().context("无法获取当前目录")?;
        let pid = id::derive_from_dir(&dir, cfg)?;
        Ok((pid, target.to_string()))
    }
}

/// 目标 store 列表（--store 限定或全部）。
fn target_stores(cfg: &Config, filter: Option<&str>) -> Vec<String> {
    match filter {
        Some(s) => vec![s.to_string()],
        None => cfg.stores(),
    }
}

fn cmd_id(cfg: &Config, path: Option<PathBuf>) -> Result<()> {
    let dir = match path {
        Some(p) => p,
        None => std::env::current_dir().context("无法获取当前目录")?,
    };
    let pid = id::derive_from_dir(&dir, cfg)?;
    let store_name = pid.store(cfg);
    println!("project_id: {}", pid.as_str());
    println!("store:      {store_name}");
    println!(
        "remote:     {}",
        repo::remote_url(&dir).unwrap_or_else(|| "(none)".to_string())
    );
    println!("repo_root:  {}", store::store_dir(&cfg.root, &store_name).display());
    Ok(())
}

fn cmd_where(cfg: &Config, project_id: &str) -> Result<()> {
    let pid = id::parse_project_id(project_id)?;
    let store_name = pid.store(cfg);
    println!("project_id: {}", pid.as_str());
    println!("store:      {store_name}");
    println!("tasks_dir:  {}", store::project_dir(&cfg.root, cfg, &pid).display());
    Ok(())
}

fn cmd_new(cfg: &Config, target: &str, scope: Option<&str>) -> Result<()> {
    let (pid, slug) = resolve_target(cfg, target)?;
    let store_name = pid.store(cfg);
    let path = store::task_path(&cfg.root, cfg, &pid, &slug);
    if path.exists() {
        bail!("任务文档已存在：{}", path.display());
    }

    let remote = std::env::current_dir()
        .ok()
        .and_then(|d| repo::remote_url(&d))
        .unwrap_or_default();
    let tpl = template::load_template(&cfg.root, &store_name)
        .unwrap_or_else(|| template::DEFAULT_TEMPLATE.to_string());
    let content = template::render(&tpl, &pid, &slug, &remote, scope);

    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent)
            .with_context(|| format!("创建目录失败：{}", parent.display()))?;
    }
    std::fs::write(&path, content).with_context(|| format!("写入失败：{}", path.display()))?;

    println!("created:    {}", path.display());
    println!("project_id: {}", pid.as_str());
    println!("store:      {store_name}");
    Ok(())
}

fn cmd_show(cfg: &Config, target: &str) -> Result<()> {
    let (pid, slug) = resolve_target(cfg, target)?;
    let path = store::task_path(&cfg.root, cfg, &pid, &slug);
    let content =
        std::fs::read_to_string(&path).with_context(|| format!("读取失败：{}", path.display()))?;
    print!("{content}");
    Ok(())
}

fn cmd_list(cfg: &Config, open: bool, filter: Option<&str>) -> Result<()> {
    for store_name in target_stores(cfg, filter) {
        let mut files = Vec::new();
        scan::walk_md(&store::tasks_dir(&cfg.root, &store_name), &mut files);
        files.sort();
        for f in files {
            let Ok(content) = std::fs::read_to_string(&f) else {
                continue;
            };
            let status = scan::extract_field(&content, "status").unwrap_or_else(|| "?".into());
            if open && status == "done" {
                continue;
            }
            let pid = scan::extract_field(&content, "project_id").unwrap_or_else(|| "?".into());
            let updated = scan::extract_field(&content, "updated").unwrap_or_default();
            println!("[{store_name}] {pid:<42} {status:<12} {updated}");
        }
    }
    Ok(())
}

fn cmd_save(cfg: &Config, target: &str, message: &str) -> Result<()> {
    let (pid, slug) = resolve_target(cfg, target)?;
    let store_name = pid.store(cfg);
    let path = store::task_path(&cfg.root, cfg, &pid, &slug);
    let content =
        std::fs::read_to_string(&path).with_context(|| format!("读取失败：{}", path.display()))?;

    // 归属校验：跨信任域写入阻断（§11）
    scan::check_trust_domain(&content, cfg, &store_name, &path)?;
    // 敏感信息阻断
    let secrets = scan::secret_hits(&content);
    if !secrets.is_empty() {
        bail!("文档命中敏感信息，已阻断推送：{}", secrets.join(", "));
    }

    let repo_dir = store::store_dir(&cfg.root, &store_name);
    repo::commit_all(&repo_dir, message)?;
    repo::push(&repo_dir)?;
    println!("saved: {} ({slug})", pid.as_str());
    Ok(())
}

fn cmd_sync(cfg: &Config, filter: Option<&str>) -> Result<()> {
    for store_name in target_stores(cfg, filter) {
        let dir = store::store_dir(&cfg.root, &store_name);
        if !dir.join(".git").exists() {
            eprintln!("skip: store `{store_name}` 不是 git 仓库（{}）", dir.display());
            continue;
        }
        repo::pull_rebase(&dir)?;
        repo::push(&dir)?;
        println!("synced: {store_name}");
    }
    Ok(())
}

fn cmd_scan(cfg: &Config, filter: Option<&str>) -> Result<()> {
    let mut total = 0usize;
    for store_name in target_stores(cfg, filter) {
        for f in scan::scan_store(&cfg.root, cfg, &store_name) {
            println!("[{}] {} — {}", f.kind, f.file.display(), f.detail);
            total += 1;
        }
    }
    if total == 0 {
        println!("clean: 未发现违规");
        Ok(())
    } else {
        bail!("发现 {total} 处问题")
    }
}

/// 把技能（SKILL.md 所在目录）符号链接到各客户端技能根，实现「一处修改、处处生效」。
/// 幂等：已正确链接则跳过；遇到冲突不覆盖，累计后以非 0 退出。
fn cmd_install_skill(from: Option<PathBuf>, roots: &[PathBuf]) -> Result<()> {
    let src_arg = from.unwrap_or_else(|| PathBuf::from(env!("CARGO_MANIFEST_DIR")));
    let src = skill::resolve_source(&src_arg)?;
    let targets = if roots.is_empty() {
        skill::default_roots()?
    } else {
        skill::roots_from(roots)
    };

    println!("source: {}", src.display());
    let mut problems = 0usize;
    for (client, root, res) in skill::install_all(&src, &targets) {
        let link = root.join(skill::SKILL_NAME);
        match res {
            Ok(skill::LinkState::Created) => {
                println!("  [created]   {client:<11} {}", link.display());
            }
            Ok(skill::LinkState::AlreadyLinked) => {
                println!("  [already]   {client:<11} {}", link.display());
            }
            Ok(skill::LinkState::Conflict(why)) => {
                problems += 1;
                println!("  [conflict]  {client:<11} {} — {why}", link.display());
            }
            Err(e) => {
                problems += 1;
                println!("  [error]     {client:<11} {} — {e:#}", root.display());
            }
        }
    }
    if problems > 0 {
        bail!("{problems} 处未完成；请处理冲突（不覆盖已有目录）后重试");
    }
    Ok(())
}
