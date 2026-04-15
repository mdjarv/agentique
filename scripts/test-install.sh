#!/usr/bin/env bash
#
# Test the full Agentique install + onboarding experience in an isolated
# temp directory. Your live database and config are never touched.
#
# Usage:
#   ./scripts/test-install.sh                  # latest release
#   ./scripts/test-install.sh v0.1.0-alpha.1   # specific tag
#
set -euo pipefail

REPO="mdjarv/agentique"
TAG="${1:-latest}"
TMPDIR=$(mktemp -d /tmp/agentique-install-test.XXXX)

cleanup() {
  echo
  echo "Cleaning up $TMPDIR ..."
  rm -rf "$TMPDIR"
  echo "Done. Your live Agentique data was not touched."
}
trap cleanup EXIT

export AGENTIQUE_HOME="$TMPDIR/data"
mkdir -p "$AGENTIQUE_HOME"

BIN_DIR="$TMPDIR/bin"
mkdir -p "$BIN_DIR"
export PATH="$BIN_DIR:$PATH"

# --- Detect platform ---
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64) ARCH="arm64" ;;
esac
ASSET="agentique-${OS}-${ARCH}"

# --- Download ---
echo "=== Agentique Install Test ==="
echo "Temp dir:  $TMPDIR"
echo "Data dir:  $AGENTIQUE_HOME"
echo "Platform:  ${OS}-${ARCH}"
echo

if [ "$TAG" = "latest" ]; then
  echo "Downloading latest release..."
  gh release download --repo "$REPO" --pattern "$ASSET" --dir "$BIN_DIR"
else
  echo "Downloading $TAG..."
  gh release download "$TAG" --repo "$REPO" --pattern "$ASSET" --dir "$BIN_DIR"
fi

chmod +x "$BIN_DIR/$ASSET"
ln -sf "$BIN_DIR/$ASSET" "$BIN_DIR/agentique"

echo
agentique --version
echo

# --- Setup wizard ---
echo "=== Running: agentique setup ==="
echo
agentique setup

# --- Serve ---
echo
echo "=== Starting server ==="
echo "Press Ctrl+C to stop and clean up."
echo
agentique serve
