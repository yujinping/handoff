# handoff v0.1.1

跨客户端会话交接 CLI：本地 markdown 任务文档 + git 同步。

## v0.1.1 变更

补齐 Windows 平台功能支持（Windows x64 二进制完整可用）：

- `install-skill` Windows 版改为**复制目录**安装（unix 仍为符号链接），
  语义与符号链接一致：幂等、冲突不覆盖，Windows 上技能可真正安装启用
- home 目录解析支持 Windows：`%USERPROFILE%`（退 `%HOMEDRIVE%%HOMEPATH%`），
  修复 store 根落在当前目录的路径错位
- 模板时间/主机名按平台分体：Windows 用 PowerShell `Get-Date` 与 `%COMPUTERNAME%`

## 功能

- 任务文档本地存储：`.handoff` 默认根目录，新机器克隆即用
- git 同步：store 由 git 仓库承载，支持多客户端交接
- 技能安装：`install-skill` 从运行期定位源
- `latest` / `list` 可复制 target / sync 尾部提示最新任务

## 安装

各平台二进制见下方 Assets（Linux / macOS x64 / macOS arm64 / Windows x64），解压即用。

## 支持平台

| 平台 | 架构 |
|------|------|
| Linux | x86_64 |
| macOS | x86_64 / arm64 |
| Windows | x86_64 |