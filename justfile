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
dev-backend:
    cd backend && go run ./cmd/agentique serve --addr 0.0.0.0:9201 --disable-auth

# Go backend with TLS
dev-backend-tls:
    cd backend && go run ./cmd/agentique serve --addr 0.0.0.0:9201 \
        --tls-cert ../certs/server.crt --tls-key ../certs/server.key \
        --rp-id {{tls-host}} --rp-origin https://{{tls-host}}:9200

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
    rm -rf backend/internal/server/frontend_dist
    mkdir -p backend/internal/server/frontend_dist
    cp -r frontend/dist/* backend/internal/server/frontend_dist/
    cd backend && go build -o ../agentique ./cmd/agentique

# Test
test-backend:
    cd backend && go test ./... -count=1

test-e2e: backend-build
    cd frontend && npx playwright test

test: test-backend test-e2e

# Run DB migrations
migrate:
    cd backend && goose -dir db/migrations sqlite3 agentique.db up

# Code generation
sqlc:
    cd backend/db && sqlc generate

# Lint & typecheck
check:
    cd frontend && npx biome check src/ && npx tsc --noEmit

# Reset (cleans DB from both project root and backend/)
reset:
    rm -f agentique.db agentique.db-journal agentique.db-wal agentique.db-shm
    rm -f backend/agentique.db backend/agentique.db-journal backend/agentique.db-wal backend/agentique.db-shm
    @echo "Reset complete. Restart server for fresh state."

# Clean build artifacts
clean:
    rm -rf frontend/dist
    rm -rf backend/internal/server/frontend_dist
    rm -f agentique agentique.exe
    rm -f *.db
