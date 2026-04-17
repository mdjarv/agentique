# Load .env if present (sets AGENTIQUE_DB for production DB in main repo).
set dotenv-load

# TLS hostname — override per-machine: just --set tls-host myhost.ts.net dev-tls
tls-host := env("AGENTIQUE_TLS_HOST", "localhost")

# List available tasks
default:
    @just --list

# Run both servers in parallel
dev:
    just stop
    just dev-backend & just dev-frontend & wait

# Run both servers with TLS (requires certs/server.{crt,key})
dev-tls:
    just stop
    just dev-backend-tls & just dev-frontend-tls & wait

# Go backend
dev-backend *args:
    cd backend && go run ./cmd/agentique serve --addr 0.0.0.0:9201 --disable-auth {{args}}

# Go backend with TLS
dev-backend-tls *args:
    cd backend && go run ./cmd/agentique serve --addr 0.0.0.0:9201 \
        --tls-cert ../certs/server.crt --tls-key ../certs/server.key \
        --rp-id {{tls-host}} --rp-origin https://{{tls-host}}:9200 {{args}}

# React frontend
dev-frontend:
    cd frontend && VITE_TLS=false npm run dev

# React frontend with TLS
dev-frontend-tls:
    cd frontend && npm run dev

# Frontend with MSW mock backend (port 9210, no real backend needed)
dev-mock:
    cd frontend && VITE_TLS=false VITE_MSW=true npx vite --port 9210

# Stop dev servers
stop:
    -lsof -ti:9200 | xargs kill 2>/dev/null
    -lsof -ti:9201 | xargs kill 2>/dev/null

# Build
build: frontend-build backend-build

frontend-build:
    cd frontend && npm ci && npm run build

backend-build: frontend-build
    #!/usr/bin/env bash
    set -euo pipefail
    rm -rf backend/internal/server/frontend_dist
    mkdir -p backend/internal/server/frontend_dist
    cp -r frontend/dist/* backend/internal/server/frontend_dist/
    # Stamp version so isRelease() is true and the binary uses paths.DBPath()
    # (XDG data dir) instead of a cwd-relative "agentique.db".
    VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo local)"
    COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
    DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    cd backend && go build \
        -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
        -o ../agentique ./cmd/agentique

# Test
test-backend:
    cd backend && go test ./... -count=1

test-frontend:
    cd frontend && npx vitest run

test-e2e: backend-build
    cd frontend && AGENTIQUE_DB="$(mktemp -d)/agentique-e2e.db" npx playwright test

test-e2e-hybrid: backend-build
    cd frontend && AGENTIQUE_DB="$(mktemp -d)/agentique-e2e.db" npx playwright test --config playwright-hybrid.config.ts

test: test-backend test-frontend test-e2e

# Run DB migrations
migrate:
    cd backend && goose -dir db/migrations sqlite3 agentique.db up

# Code generation
sqlc:
    cd backend/db && sqlc generate

typegen:
    cd backend && go run ./cmd/typegen --out ../frontend/src/lib

# Lint & typecheck
check:
    cd frontend && npx biome check src/ && npx tsc --noEmit

# Reset (cleans local dev DB files, NOT the production DB)
reset:
    rm -f agentique.db agentique.db-journal agentique.db-wal agentique.db-shm
    rm -f backend/agentique.db backend/agentique.db-journal backend/agentique.db-wal backend/agentique.db-shm
    @echo "Reset complete. Restart server for fresh state."

# Cross-compile release binaries for distribution
release: frontend-build
    #!/usr/bin/env bash
    set -euo pipefail
    rm -rf dist
    mkdir -p dist
    rm -rf backend/internal/server/frontend_dist
    mkdir -p backend/internal/server/frontend_dist
    cp -r frontend/dist/* backend/internal/server/frontend_dist/
    VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
    COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "none")
    DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    LDFLAGS="-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}"
    echo "Building dist/agentique-linux-amd64..."
    cd backend && GOOS=linux GOARCH=amd64 go build -ldflags "$LDFLAGS" -o "../dist/agentique-linux-amd64" ./cmd/agentique && cd ..
    echo "Release binary in dist/:"
    ls -lh dist/

# Install locally from source (mirrors install.sh but uses local build)
install: build
    #!/usr/bin/env bash
    set -euo pipefail
    INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
    mkdir -p "$INSTALL_DIR"
    # Atomic replace via rename(2) — works even if the binary is currently running
    # (kernel unlinks the busy inode; the running process keeps it; path now points
    # to a fresh inode). Plain cp would fail with "Text file busy".
    cp agentique "${INSTALL_DIR}/agentique.new"
    chmod +x "${INSTALL_DIR}/agentique.new"
    mv "${INSTALL_DIR}/agentique.new" "${INSTALL_DIR}/agentique"
    VERSION="$("${INSTALL_DIR}/agentique" --version 2>/dev/null | awk '{print $2}' || echo unknown)"
    echo "Installed agentique ${VERSION} to ${INSTALL_DIR}/agentique"
    if ! echo "$PATH" | tr ':' '\n' | grep -qx "$INSTALL_DIR"; then
      echo ""
      echo "WARNING: ${INSTALL_DIR} is not in your PATH. Add it:"
      echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
      echo ""
    fi
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
    if systemctl --user is-enabled agentique &>/dev/null; then
      "${INSTALL_DIR}/agentique" service install
      if systemctl --user is-active agentique &>/dev/null; then
        echo ""
        echo "Service is running the OLD binary. To pick up this build, run:"
        echo "  agentique service restart"
      fi
      echo ""
    fi
    echo "Checking dependencies..."
    echo ""
    "${INSTALL_DIR}/agentique" doctor || true

# Clean build artifacts
clean:
    rm -rf frontend/dist
    rm -rf backend/internal/server/frontend_dist
    rm -f agentique agentique.exe
    rm -f *.db
