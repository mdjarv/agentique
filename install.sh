#!/usr/bin/env bash
set -euo pipefail

REPO="mdjarv/agentique"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

# Detect platform
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  *) echo "Error: unsupported architecture: $ARCH"; exit 1 ;;
esac

case "$OS" in
  linux) ;;
  *) echo "Error: unsupported OS: $OS"; exit 1 ;;
esac

ASSET="agentique-${OS}-${ARCH}"

# Get latest release tag
TAG="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)"
if [ -z "$TAG" ]; then
  echo "Error: failed to fetch latest release"
  exit 1
fi

URL="https://github.com/${REPO}/releases/download/${TAG}/${ASSET}"
CHECKSUM_URL="https://github.com/${REPO}/releases/download/${TAG}/checksums.txt"

# Check for existing install
if [ -f "${INSTALL_DIR}/agentique" ]; then
  EXISTING="$("${INSTALL_DIR}/agentique" --version 2>/dev/null | awk '{print $2}' || echo "")"
  if [ "$EXISTING" = "$TAG" ]; then
    echo "agentique ${TAG} already installed."
    exit 0
  fi
  echo "Upgrading agentique (${EXISTING:-unknown} -> ${TAG})"
else
  echo "Installing agentique ${TAG}..."
fi

# Download binary to temp file
TMPFILE="$(mktemp)"
trap 'rm -f "$TMPFILE"' EXIT
curl -fsSL "$URL" -o "$TMPFILE"

# Verify checksum if available
CHECKSUMS="$(curl -fsSL "$CHECKSUM_URL" 2>/dev/null || true)"
if [ -n "$CHECKSUMS" ]; then
  EXPECTED="$(echo "$CHECKSUMS" | grep "$ASSET" | awk '{print $1}')"
  ACTUAL="$(sha256sum "$TMPFILE" | awk '{print $1}')"
  if [ "$EXPECTED" != "$ACTUAL" ]; then
    echo "Error: checksum mismatch"
    echo "  expected: $EXPECTED"
    echo "  got:      $ACTUAL"
    exit 1
  fi
  echo "Checksum verified."
else
  echo "Warning: no checksums available, skipping verification."
fi

# Install
mkdir -p "$INSTALL_DIR"
mv "$TMPFILE" "${INSTALL_DIR}/agentique"
chmod +x "${INSTALL_DIR}/agentique"

echo "Installed agentique ${TAG} to ${INSTALL_DIR}/agentique"

# Check if install dir is in PATH
if ! echo "$PATH" | tr ':' '\n' | grep -qx "$INSTALL_DIR"; then
  echo ""
  echo "WARNING: ${INSTALL_DIR} is not in your PATH. Add it:"
  echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
  echo ""
fi

# Install shell completions
SHELL_NAME="$(basename "${SHELL:-}")"
case "$SHELL_NAME" in
  fish)
    COMP_DIR="$HOME/.config/fish/completions"
    mkdir -p "$COMP_DIR"
    "${INSTALL_DIR}/agentique" completion fish > "$COMP_DIR/agentique.fish" 2>/dev/null && \
      echo "Installed fish completions to $COMP_DIR/agentique.fish" || true
    ;;
  zsh)
    COMP_DIR="$HOME/.zsh/completions"
    mkdir -p "$COMP_DIR"
    "${INSTALL_DIR}/agentique" completion zsh > "$COMP_DIR/_agentique" 2>/dev/null && \
      echo "Installed zsh completions to $COMP_DIR/_agentique" || true
    ;;
  bash)
    COMP_DIR="$HOME/.local/share/bash-completion/completions"
    mkdir -p "$COMP_DIR"
    "${INSTALL_DIR}/agentique" completion bash > "$COMP_DIR/agentique" 2>/dev/null && \
      echo "Installed bash completions to $COMP_DIR/agentique" || true
    ;;
esac

# Restart service if running
if systemctl --user is-active agentique &>/dev/null; then
  echo "Service is running, restarting..."
  systemctl --user restart agentique
  echo "Service restarted."
  echo ""
fi

# Run doctor to check dependencies
echo "Checking dependencies..."
echo ""
"${INSTALL_DIR}/agentique" doctor || true
