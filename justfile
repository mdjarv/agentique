# Load .env if present (sets AGENTIQUE_DB for production DB in main repo).
set dotenv-load

# TLS hostname — override per-machine: just --set tls-host myhost.ts.net dev-tls
tls-host := env("AGENTIQUE_TLS_HOST", "localhost")

# Executable suffix. `go build -o <name>` takes the name literally, so nothing
# adds this for us, and the rest of the repo already assumes it is there: both
# Playwright configs resolve `agentique.exe` on win32, and a Windows shell will
# not run an extensionless PE off PATH. One definition, because every recipe
# that names the binary needs the same answer.
exe := if os() == "windows" { ".exe" } else { "" }

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
    cd backend && go run ./cmd/agentique serve --addr 127.0.0.1:9201 \
        --disable-auth --rp-origin http://localhost:9200 {{args}}

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

# Remote dev slot: Vite bound to the given port, HMR via wss://<host>:443,
# API/WS proxied to the installed agentique service (default port 19201).
# Acquire {port, host} from the AcquireDevUrl MCP tool first.
dev-frontend-remote port host backend-port="19201":
    cd frontend && VITE_TLS=false \
        VITE_PORT={{port}} \
        VITE_PUBLIC_HOST={{host}} \
        VITE_BACKEND_PORT={{backend-port}} \
        npm run dev

# Stop dev servers
stop:
    -lsof -ti:9200 | xargs kill 2>/dev/null
    -lsof -ti:9201 | xargs kill 2>/dev/null

# Build
build: frontend-build backend-build

# `--no-audit` because an install is not an audit. `npm ci` otherwise POSTs the
# whole tree to registry.npmjs.org/-/npm/v1/security/advisories/bulk, and when
# that endpoint is degraded it hangs through npm's retries — five minutes of
# `just upgrade` for a 13-second install, with the tarballs already in cache.
# CI keeps an explicit `npm audit` step (ci.yml), which is where the advisory
# gate belongs: it can fail loudly there instead of taxing every local build.
frontend-build:
    cd frontend && npm ci --no-audit --no-fund && npm run build

backend-build: frontend-build
    #!/usr/bin/env bash
    set -euo pipefail
    DIST="backend/internal/server/frontend_dist"
    mkdir -p "$DIST"
    # Clear stale build output but preserve the tracked .gitkeep — otherwise
    # `just install` leaves the repo dirty and blocks fast-forward merges.
    find "$DIST" -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
    cp -r frontend/dist/. "$DIST/"
    # Stamp version so isRelease() is true and the binary uses paths.DBPath()
    # (XDG data dir) instead of a cwd-relative "agentique.db".
    VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo local)"
    COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
    DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    # main.buildOrigin=local is what turns the source channel on (docs/upgrades.md).
    # Nothing else can tell a local build from a downloaded one: building at an
    # exact tag stamps the bare tag, identical to what CI stamps. Keep this
    # `local` here and `release` in the release recipe and release.yml.
    cd backend && go build \
        -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE} -X main.buildOrigin=local" \
        -o ../agentique{{exe}} ./cmd/agentique

# Test
test-backend:
    cd backend && go test ./... -count=1 -race -short

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
    DIST="backend/internal/server/frontend_dist"
    mkdir -p "$DIST"
    find "$DIST" -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
    cp -r frontend/dist/. "$DIST/"
    VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
    COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "none")
    DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    # buildOrigin=release, NOT local: these are the artifacts that get uploaded
    # and installed on other machines, where the local checkout — if there even
    # is one — is not where this binary came from.
    LDFLAGS="-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE} -X main.buildOrigin=release"
    # Keep this list in step with .github/workflows/release.yml and the asset
    # names in backend/internal/update/platform.go. A name that disagrees with
    # the workflow produces a local dist/ that does not match a real release.
    for target in linux/amd64 linux/arm64 windows/amd64 darwin/arm64; do
      GOOS="${target%%/*}"; GOARCH="${target##*/}"
      out="dist/agentique-${GOOS}-${GOARCH}"
      if [ "$GOOS" = "windows" ]; then out="${out}.exe"; fi
      echo "Building ${out}..."
      (cd backend && GOOS="$GOOS" GOARCH="$GOARCH" CGO_ENABLED=0 \
        go build -ldflags "$LDFLAGS" -o "../${out}" ./cmd/agentique)
    done
    (cd dist && sha256sum * > checksums.txt)
    echo "Release binaries in dist/:"
    ls -lh dist/

# Install locally from source (mirrors install.sh but uses local build)
install: build
    #!/usr/bin/env bash
    set -euo pipefail
    INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
    TARGET="${INSTALL_DIR}/agentique{{exe}}"
    mkdir -p "$INSTALL_DIR"
    # The same two renames within one directory that internal/update's installOver
    # does, and for the same reasons. On POSIX rename(2) is atomic and the running
    # process keeps its open inode, where a plain cp fails with "Text file busy".
    # On Windows a running .exe can be renamed aside but never overwritten, so
    # moving the old one out of the way first is what makes the swap possible at
    # all. Keeping it as .prev is also what lets `agentique rollback` undo a
    # source install, exactly as it undoes an in-app upgrade.
    cp "agentique{{exe}}" "${TARGET}.new"
    chmod +x "${TARGET}.new"
    rm -f "${TARGET}.prev"
    if [ -e "$TARGET" ]; then
      mv "$TARGET" "${TARGET}.prev"
    fi
    mv "${TARGET}.new" "$TARGET"
    VERSION="$("$TARGET" --version 2>/dev/null | awk '{print $2}' || echo unknown)"
    echo "Installed agentique ${VERSION} to ${TARGET}"
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
        "$TARGET" completion fish > "$COMP_DIR/agentique.fish" 2>/dev/null && \
          echo "Installed fish completions to $COMP_DIR/agentique.fish" || true
        ;;
      zsh)
        COMP_DIR="$HOME/.zsh/completions"
        mkdir -p "$COMP_DIR"
        "$TARGET" completion zsh > "$COMP_DIR/_agentique" 2>/dev/null && \
          echo "Installed zsh completions to $COMP_DIR/_agentique" || true
        ;;
      bash)
        COMP_DIR="$HOME/.local/share/bash-completion/completions"
        mkdir -p "$COMP_DIR"
        "$TARGET" completion bash > "$COMP_DIR/agentique" 2>/dev/null && \
          echo "Installed bash completions to $COMP_DIR/agentique" || true
        ;;
    esac
    # Refreshing the unit is a systemd-only concern — the unit's contents can
    # change between builds, where the launchd plist and the Windows task name a
    # path that has not moved.
    if systemctl --user is-enabled agentique &>/dev/null; then
      "$TARGET" service install
    fi
    # Being left on the binary this build just replaced is not systemd-only,
    # though, and `service status` is the cross-platform answer: systemd, launchd
    # and schtasks all live behind it.
    if "$TARGET" service status 2>/dev/null | grep -q '^Running'; then
      echo ""
      echo "Service is running the OLD binary. To pick up this build, run:"
      echo "  agentique service restart"
      echo ""
    fi
    echo "Checking dependencies..."
    echo ""
    "$TARGET" doctor || true

# Install, restart the service, and report doctor status
upgrade: install
    #!/usr/bin/env bash
    set -euo pipefail
    INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
    TARGET="${INSTALL_DIR}/agentique{{exe}}"
    # No systemctl probe. `service restart` already owns systemd, launchd and
    # schtasks behind one command, and reports "Service not installed" itself.
    # Asking systemctl first duplicated that knowledge and got it wrong on two
    # platforms of three: on Windows and macOS the probe simply failed, so the
    # upgrade installed a new binary and then left the old one running, saying
    # it had skipped the restart because no service was installed.
    #
    # Note this ends any turn in flight on this machine — a restart reaps the
    # CLI process groups (docs/process-lifecycle.md). The in-app upgrade has a
    # drain gate for that; this recipe is the blunt instrument and does not.
    echo "Restarting agentique service..."
    "$TARGET" service restart
    echo ""
    "$TARGET" doctor || true

# Clean build artifacts
clean:
    rm -rf frontend/dist
    find backend/internal/server/frontend_dist -mindepth 1 ! -name .gitkeep -exec rm -rf {} + 2>/dev/null || true
    rm -f agentique agentique.exe
    rm -f *.db
