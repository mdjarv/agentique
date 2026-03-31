#!/bin/sh
# Agentique installer
# Usage: curl -fsSL https://raw.githubusercontent.com/mdjarv/agentique/master/install.sh | sh
# Pin a version: curl -fsSL ... | sh -s v0.2.0
set -e

REPO="mdjarv/agentique"
INSTALL_DIR="$HOME/.local/bin"
BINARY_NAME="agentique"

main() {
    check_dependencies
    detect_platform
    resolve_version "$1"
    download_and_install
    check_path
    print_next_steps
}

check_dependencies() {
    for cmd in curl sha256sum; do
        if ! command -v "$cmd" >/dev/null 2>&1; then
            # macOS uses shasum instead of sha256sum
            if [ "$cmd" = "sha256sum" ] && command -v shasum >/dev/null 2>&1; then
                continue
            fi
            echo "Error: $cmd is required but not installed." >&2
            exit 1
        fi
    done
}

detect_platform() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case "$OS" in
        linux)  OS="linux" ;;
        darwin) OS="darwin" ;;
        *)
            echo "Error: unsupported OS: $OS" >&2
            exit 1
            ;;
    esac

    case "$ARCH" in
        x86_64|amd64)  ARCH="amd64" ;;
        aarch64|arm64) ARCH="arm64" ;;
        *)
            echo "Error: unsupported architecture: $ARCH" >&2
            exit 1
            ;;
    esac

    PLATFORM="${OS}-${ARCH}"
    echo "Detected platform: $PLATFORM"
}

resolve_version() {
    if [ -n "${1:-}" ]; then
        VERSION="$1"
        echo "Using specified version: $VERSION"
    else
        echo "Fetching latest version..."
        VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | \
            grep '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')
        if [ -z "$VERSION" ]; then
            echo "Error: could not determine latest version." >&2
            echo "Check https://github.com/$REPO/releases" >&2
            exit 1
        fi
        echo "Latest version: $VERSION"
    fi
}

download_and_install() {
    BINARY="agentique-${PLATFORM}"
    BASE_URL="https://github.com/$REPO/releases/download/$VERSION"
    BINARY_URL="${BASE_URL}/${BINARY}"
    CHECKSUMS_URL="${BASE_URL}/checksums.txt"

    TMPDIR=$(mktemp -d)
    trap 'rm -rf "$TMPDIR"' EXIT

    echo "Downloading $BINARY..."
    curl -fsSL -o "$TMPDIR/$BINARY" "$BINARY_URL"
    curl -fsSL -o "$TMPDIR/checksums.txt" "$CHECKSUMS_URL"

    echo "Verifying checksum..."
    cd "$TMPDIR"
    if command -v sha256sum >/dev/null 2>&1; then
        grep "$BINARY" checksums.txt | sha256sum -c --quiet
    else
        EXPECTED=$(grep "$BINARY" checksums.txt | awk '{print $1}')
        ACTUAL=$(shasum -a 256 "$BINARY" | awk '{print $1}')
        if [ "$EXPECTED" != "$ACTUAL" ]; then
            echo "Error: checksum mismatch" >&2
            echo "  expected: $EXPECTED" >&2
            echo "  actual:   $ACTUAL" >&2
            exit 1
        fi
    fi
    cd - >/dev/null

    mkdir -p "$INSTALL_DIR"
    cp "$TMPDIR/$BINARY" "$INSTALL_DIR/$BINARY_NAME"
    chmod +x "$INSTALL_DIR/$BINARY_NAME"
    echo "Installed $BINARY_NAME $VERSION to $INSTALL_DIR/$BINARY_NAME"
}

check_path() {
    case ":$PATH:" in
        *":$INSTALL_DIR:"*) return ;;
    esac

    echo ""
    echo "Warning: $INSTALL_DIR is not on your PATH."

    SHELL_NAME=$(basename "${SHELL:-/bin/sh}")
    case "$SHELL_NAME" in
        fish)
            echo "  Run: fish_add_path $INSTALL_DIR"
            ;;
        zsh)
            echo "  Add to ~/.zshrc:  export PATH=\"$INSTALL_DIR:\$PATH\""
            ;;
        bash)
            echo "  Add to ~/.bashrc: export PATH=\"$INSTALL_DIR:\$PATH\""
            ;;
        *)
            echo "  Add $INSTALL_DIR to your PATH"
            ;;
    esac
    echo "  Then restart your shell."
}

print_next_steps() {
    echo ""
    echo "Next steps:"
    echo "  agentique setup     Guided configuration (recommended)"
    echo "  agentique serve     Start with defaults"
    echo "  agentique doctor    Check prerequisites"
}

main "$@"
