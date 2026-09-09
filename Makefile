.PHONY: up down db storage backend frontend build test chart microk8s-setup microk8s-deploy microk8s-test microk8s-forward
up:
	python3 scripts/microk8s.py setup
	docker compose build app
	docker compose stop
	python3 scripts/microk8s.py deploy
	python3 scripts/microk8s.py forward
down:
	python3 scripts/microk8s.py stop
db:
	docker compose up -d postgres --wait
storage:
	docker compose up -d minio-init
backend: storage
	APP_MODE=local COURSE_STORAGE_PROVIDER=s3 COURSE_S3_ENDPOINT=http://127.0.0.1:9000 COURSE_STORAGE_BUCKET=courses COURSE_STORAGE_PREFIX=courses/ COURSE_S3_REGION=us-east-1 COURSE_S3_ACCESS_KEY=romanian-local COURSE_S3_SECRET_KEY=local-minio-development-only DATABASE_URL='postgres://romanian:local-development-only@127.0.0.1:5432/romanian?sslmode=disable' go run ./cmd/server
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

microk8s-setup:
	python3 scripts/microk8s.py setup
microk8s-deploy:
	python3 scripts/microk8s.py deploy
microk8s-test:
	python3 scripts/microk8s.py test
microk8s-forward:
	python3 scripts/microk8s.py forward
