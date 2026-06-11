#!/usr/bin/env bash
# Build and install goloop from local source as goloop-dev.
#
# Usage:
#   ./scripts/local-install.sh
#   INSTALL_DIR=~/.local/bin ./scripts/local-install.sh
#   ./scripts/local-install.sh --dir /usr/local/bin
set -euo pipefail

INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
BIN_NAME="goloop-dev"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

usage() {
  cat <<EOF
Usage: local-install.sh [options]

Build ./cmd/goloop from this repo and install it as ${BIN_NAME}.

Options:
  --dir PATH   Install directory (default: ~/.local/bin)
  -h, --help   Show this help
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
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

need_cmd go

VERSION="dev"
if command -v git >/dev/null 2>&1 && git -C "$ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  if v="$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null)"; then
    VERSION="$v"
  fi
fi

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

BIN="${TMPDIR}/${BIN_NAME}"
LDFLAGS="-s -w -X main.version=${VERSION}"

echo "Building ${BIN_NAME} (${VERSION}) from ${ROOT}..."
(
  cd "$ROOT"
  CGO_ENABLED=0 go build -ldflags "$LDFLAGS" -o "$BIN" ./cmd/goloop
)

DEST="${INSTALL_DIR}/${BIN_NAME}"
mkdir -p "$INSTALL_DIR"

if [[ -w "$INSTALL_DIR" ]]; then
  install -m 0755 "$BIN" "$DEST"
else
  need_cmd sudo
  sudo install -m 0755 "$BIN" "$DEST"
fi

echo "Installed ${BIN_NAME} ${VERSION} -> ${DEST}"

if [[ ":$PATH:" != *":${INSTALL_DIR}:"* ]]; then
  echo "Note: ${INSTALL_DIR} is not on your PATH"
fi

"$DEST" version
