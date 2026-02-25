#!/bin/sh
set -eu

REPO="millcake666/sonar-gitlab-commenter"
BIN_BASENAME="sonar-gitlab-commenter"
INSTALL_DIR="/usr/local/bin"

usage() {
  cat <<EOF
Usage: $0 [version]

Install sonar-gitlab-commenter from GitHub Releases.

Arguments:
  version    Optional release tag (example: v1.0.0). Default: latest
EOF
}

fail() {
  echo "Error: $*" >&2
  exit 1
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  usage
  exit 0
fi

VERSION="${1:-latest}"

command -v curl >/dev/null 2>&1 || fail "curl is required"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$OS" in
  linux|darwin) ;;
  *)
    fail "unsupported OS: $OS (supported: linux, darwin)"
    ;;
esac

ARCH_RAW="$(uname -m)"
case "$ARCH_RAW" in
  x86_64|amd64)
    ARCH="amd64"
    ;;
  arm64|aarch64)
    ARCH="arm64"
    ;;
  *)
    fail "unsupported architecture: $ARCH_RAW (supported: amd64, arm64)"
    ;;
esac

ASSET_NAME="${BIN_BASENAME}-${OS}-${ARCH}"

if [ "$VERSION" = "latest" ]; then
  BASE_URL="https://github.com/${REPO}/releases/latest/download"
else
  BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

BIN_PATH="${TMP_DIR}/${ASSET_NAME}"
CHECKSUMS_PATH="${TMP_DIR}/checksums.txt"

echo "Downloading ${ASSET_NAME}..."
curl -fsSL -o "$BIN_PATH" "${BASE_URL}/${ASSET_NAME}" || fail "failed to download binary"
curl -fsSL -o "$CHECKSUMS_PATH" "${BASE_URL}/checksums.txt" || fail "failed to download checksums.txt"

CHECKSUM_LINE="$(grep "  ${ASSET_NAME}$" "$CHECKSUMS_PATH" || true)"
[ -n "$CHECKSUM_LINE" ] || fail "checksum entry not found for ${ASSET_NAME}"
EXPECTED_SUM="$(printf "%s\n" "$CHECKSUM_LINE" | awk '{print $1}')"

if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL_SUM="$(sha256sum "$BIN_PATH" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL_SUM="$(shasum -a 256 "$BIN_PATH" | awk '{print $1}')"
else
  fail "sha256sum or shasum is required for checksum verification"
fi

[ "$EXPECTED_SUM" = "$ACTUAL_SUM" ] || fail "checksum verification failed"

chmod +x "$BIN_PATH"

DEST_PATH="${INSTALL_DIR}/${BIN_BASENAME}"
if [ -w "$INSTALL_DIR" ]; then
  install -m 0755 "$BIN_PATH" "$DEST_PATH"
else
  command -v sudo >/dev/null 2>&1 || fail "no write access to ${INSTALL_DIR}, and sudo not found"
  sudo install -m 0755 "$BIN_PATH" "$DEST_PATH"
fi

echo "Installed ${BIN_BASENAME} to ${DEST_PATH}"
echo "Run: ${BIN_BASENAME} --help"
