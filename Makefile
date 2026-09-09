.PHONY: up down db backend frontend build test chart
up:
	docker compose up -d --build --wait
down:
	docker compose down
db:
	docker compose up -d postgres --wait
backend:
	APP_MODE=local DATABASE_URL='postgres://romanian:local-development-only@127.0.0.1:5432/romanian?sslmode=disable' go run ./cmd/server
frontend:
	cd web && npm run dev
build:
	cd web && npm ci && npm run build
	go build -o bin/romanian ./cmd/server
test:
	go test ./...
	cd web && npm run build
	helm lint charts/romanian
chart:
	helm template romanian charts/romanian
