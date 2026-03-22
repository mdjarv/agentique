.PHONY: build frontend-build backend-build dev-backend dev-frontend clean test-backend test-e2e sqlc

build: frontend-build backend-build

frontend-build:
	cd frontend && npm ci && npm run build

backend-build: frontend-build
	rm -rf backend/internal/server/frontend_dist
	mkdir -p backend/internal/server/frontend_dist
	cp -r frontend/dist/* backend/internal/server/frontend_dist/
	cd backend && go build -o ../agentique ./cmd/agentique

dev-backend:
	cd backend && air

dev-frontend:
	cd frontend && npm run dev

test-backend:
	cd backend && go test ./...

test-e2e:
	cd frontend && npx playwright test

sqlc:
	cd backend/db && sqlc generate

clean:
	rm -rf frontend/dist
	rm -rf backend/internal/server/frontend_dist
	rm -f agentique agentique.exe
	rm -f *.db
