#!/usr/bin/env bash
# Install goloop from GitHub releases.
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/mantyx-io/goloop/main/scripts/install.sh | bash
#   curl -fsSL ... | bash -s -- --version v0.1.0
#   INSTALL_DIR=~/.local/bin curl -fsSL ... | bash
set -euo pipefail

REPO="mantyx-io/goloop"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
REQUESTED_VERSION=""

usage() {
  cat <<EOF
Usage: install.sh [options]

Options:
  --version TAG   Install a specific release tag (e.g. v0.1.0)
  --dir PATH      Install directory (default: /usr/local/bin)
  -h, --help      Show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      REQUESTED_VERSION="${2:-}"
      shift 2
      ;;
    --dir)
      INSTALL_DIR="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: required command not found: $1" >&2
    exit 1
  fi
}

detect_os() {
  case "$(uname -s)" in
    Linux) echo "linux" ;;
    Darwin) echo "darwin" ;;
    MINGW*|MSYS*|CYGWIN*) echo "windows" ;;
    *)
      echo "error: unsupported OS: $(uname -s)" >&2
      exit 1
      ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *)
      echo "error: unsupported architecture: $(uname -m)" >&2
      exit 1
      ;;
  esac
}

latest_tag() {
  curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' \
    | head -n1
}

OS="$(detect_os)"
ARCH="$(detect_arch)"
TAG="${REQUESTED_VERSION:-$(latest_tag)}"

if [[ -z "$TAG" ]]; then
  echo "error: could not resolve latest release tag" >&2
  exit 1
fi

ASSET="goloop-${OS}-${ARCH}"
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

need_cmd curl
need_cmd mkdir
need_cmd chmod

if [[ "$OS" == "windows" ]]; then
  need_cmd unzip
  ARCHIVE="${TMPDIR}/${ASSET}.zip"
  curl -fsSL -o "$ARCHIVE" "https://github.com/${REPO}/releases/download/${TAG}/${ASSET}.zip"
  unzip -q "$ARCHIVE" -d "$TMPDIR"
  BIN="${TMPDIR}/goloop.exe"
  DEST="${INSTALL_DIR}/goloop.exe"
else
  need_cmd tar
  ARCHIVE="${TMPDIR}/${ASSET}.tar.gz"
  curl -fsSL -o "$ARCHIVE" "https://github.com/${REPO}/releases/download/${TAG}/${ASSET}.tar.gz"
  tar -xzf "$ARCHIVE" -C "$TMPDIR"
  BIN="${TMPDIR}/goloop"
  DEST="${INSTALL_DIR}/goloop"
fi

if [[ ! -f "$BIN" ]]; then
  echo "error: binary not found in release archive" >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR"
if [[ -w "$INSTALL_DIR" ]]; then
  install -m 0755 "$BIN" "$DEST"
else
  need_cmd sudo
  sudo install -m 0755 "$BIN" "$DEST"
fi

echo "Installed goloop ${TAG} -> ${DEST}"
"$DEST" version 2>/dev/null || true
