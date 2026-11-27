#!/bin/sh
# handoff 安装脚本：识别系统和架构，下载最新 Release，校验 SHA256 后安装。
#
#   curl -fsSL https://raw.githubusercontent.com/yujinping/handoff/main/scripts/install.sh | sh
#
# 可选参数：
#   --version v0.2.0   指定版本（默认取最新 Release）
#   --dir /path        安装目录（默认 ~/.local/bin）
#
# 环境变量：
#   HANDOFF_VERSION     同 --version
#   HANDOFF_INSTALL_DIR 同 --dir
#   HANDOFF_BASE_URL    下载地址前缀，默认 GitHub Release；网络受限时可指向镜像
set -eu

REPO="yujinping/handoff"
BIN="handoff"
DEFAULT_BASE="https://github.com/${REPO}/releases/download"

INSTALL_DIR="${HANDOFF_INSTALL_DIR:-${HOME}/.local/bin}"
VERSION="${HANDOFF_VERSION:-}"
BASE="${HANDOFF_BASE_URL:-${DEFAULT_BASE}}"

usage() {
	cat <<'EOF'
用法: install.sh [--version vX.Y.Z] [--dir 目录]

  --version   指定版本，默认最新 Release
  --dir       安装目录，默认 ~/.local/bin
  -h, --help  显示帮助

支持: Linux/macOS，amd64/arm64。Windows 请直接下载 .exe。
EOF
}

err() {
	printf 'install: %s\n' "$*" >&2
	exit 1
}

while [ $# -gt 0 ]; do
	case "$1" in
	--version)
		[ $# -ge 2 ] || err "--version 需要一个值"
		VERSION="$2"
		shift 2
		;;
	--dir)
		[ $# -ge 2 ] || err "--dir 需要一个值"
		INSTALL_DIR="$2"
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		usage >&2
		err "未知参数: $1"
		;;
	esac
done

case "$(uname -s)" in
Darwin) os="darwin" ;;
Linux) os="linux" ;;
*)
	err "不支持的系统 $(uname -s)。Windows 请从 Release 下载 handoff_*_windows_amd64.exe"
	;;
esac

case "$(uname -m)" in
x86_64 | amd64) arch="amd64" ;;
arm64 | aarch64) arch="arm64" ;;
*) err "不支持的架构 $(uname -m)" ;;
esac

command -v curl >/dev/null 2>&1 || err "需要 curl"

# 未指定版本时查询最新 Release；API 失败则退回 releases/latest 的重定向地址。
if [ -z "$VERSION" ] || [ "$VERSION" = "latest" ]; then
	VERSION="$(curl -fsSL --max-time 20 "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null |
		sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)" || VERSION=""
	if [ -z "$VERSION" ]; then
		VERSION="$(curl -fsSIL --max-time 20 -o /dev/null -w '%{url_effective}' \
			"https://github.com/${REPO}/releases/latest" 2>/dev/null | sed 's|.*/tag/||')" || VERSION=""
	fi
fi
[ -n "$VERSION" ] || err "无法确定最新版本，请用 --version vX.Y.Z 指定"

case "$VERSION" in
v*) ;;
*) VERSION="v${VERSION}" ;;
esac
ver="${VERSION#v}"
asset="${BIN}_${ver}_${os}_${arch}"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/handoff.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

fetch() {
	if [ "${1#https://}" != "$1" ]; then
		curl -fsSL --proto '=https' --tlsv1.2 --retry 5 --retry-delay 2 \
			--connect-timeout 15 --max-time 300 -o "$2" "$1"
	else
		curl -fsSL --retry 2 --connect-timeout 15 --max-time 300 -o "$2" "$1"
	fi
}

base_url="${BASE%/}/${VERSION}"
printf '下载 %s ...\n' "${asset}" >&2
fetch "${base_url}/${asset}" "${tmp}/${BIN}" || err "下载失败: ${base_url}/${asset}"
fetch "${base_url}/checksums.txt" "${tmp}/checksums.txt" || err "下载 checksums.txt 失败"

want="$(awk -v a="${asset}" '$2 == a { print $1 }' "${tmp}/checksums.txt")"
[ -n "$want" ] || err "checksums.txt 里没有 ${asset}"
if command -v sha256sum >/dev/null 2>&1; then
	got="$(sha256sum "${tmp}/${BIN}" | awk '{ print $1 }')"
else
	got="$(shasum -a 256 "${tmp}/${BIN}" | awk '{ print $1 }')"
fi
[ "$got" = "$want" ] || err "SHA256 校验失败（期望 ${want}，实际 ${got}）"

chmod 0755 "${tmp}/${BIN}"
"${tmp}/${BIN}" version >/dev/null 2>&1 || err "下载的二进制无法运行，可能平台不匹配"

mkdir -p "$INSTALL_DIR" || err "无法创建目录 ${INSTALL_DIR}"
if command -v install >/dev/null 2>&1; then
	install -m 0755 "${tmp}/${BIN}" "${INSTALL_DIR}/${BIN}" || err "写入 ${INSTALL_DIR} 失败"
else
	cp "${tmp}/${BIN}" "${INSTALL_DIR}/${BIN}" && chmod 0755 "${INSTALL_DIR}/${BIN}" || err "写入 ${INSTALL_DIR} 失败"
fi

printf '已安装 handoff %s 到 %s\n' "$ver" "${INSTALL_DIR}/${BIN}"
"${INSTALL_DIR}/${BIN}" version || true

case ":${PATH}:" in
*":${INSTALL_DIR}:"*) ;;
*)
	printf '\n%s 不在 PATH 中，加到 shell 配置里：\n  export PATH="%s:$PATH"\n' "$INSTALL_DIR" "$INSTALL_DIR"
	;;
esac

printf '\n下一步：\n  handoff init\n  handoff mcp        # 在 AI 客户端里配置\n'