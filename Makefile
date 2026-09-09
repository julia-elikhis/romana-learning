.PHONY: up up-local down db storage backend frontend build test chart microk8s-setup microk8s-deploy microk8s-test microk8s-forward secrets-init secrets-import secrets-edit secrets-check secrets-exec secrets-use-gcp workload-identity-setup workload-identity-check require-local
APP_ENV ?= $(if $(ROMANA_ENV),$(ROMANA_ENV),local)
export ROMANA_ENV := $(APP_ENV)
SECRET_EXEC = python3 scripts/sops_secrets.py exec --
require-local:
	python3 scripts/sops_secrets.py require-local
up: require-local
	python3 scripts/sops_secrets.py check
	python3 scripts/microk8s.py setup
	$(SECRET_EXEC) docker compose build app
	$(SECRET_EXEC) docker compose stop
	python3 scripts/microk8s.py deploy
	python3 scripts/microk8s.py forward
up-local:
	ROMANA_STORAGE=minio $(MAKE) up
down: require-local
	python3 scripts/microk8s.py stop
db: require-local
	$(SECRET_EXEC) docker compose up -d postgres --wait
storage: require-local
	$(SECRET_EXEC) docker compose up -d minio-init
backend: storage
	$(SECRET_EXEC) sh -c 'export APP_MODE=local PGHOST=127.0.0.1 PGDATABASE=romanian PGSSLMODE=disable COURSE_STORAGE_PROVIDER=s3 COURSE_S3_ENDPOINT=http://127.0.0.1:9000 COURSE_STORAGE_BUCKET=courses COURSE_STORAGE_PREFIX=courses/ COURSE_S3_REGION=us-east-1 COURSE_S3_ACCESS_KEY="$$MINIO_ROOT_USER" COURSE_S3_SECRET_KEY="$$MINIO_ROOT_PASSWORD"; exec go run ./cmd/server'
frontend:
	cd web && npm run dev
build:
	cd web && npm ci && npm run build
	go build -o bin/romanian ./cmd/server
test:
	python3 -m unittest discover -s scripts -p 'test_*.py'
	go test ./...
	cd web && npm run build
	helm lint charts/romanian
chart:
	python3 scripts/helm_chart.py template

microk8s-setup: require-local
	python3 scripts/microk8s.py setup
microk8s-deploy: require-local
	python3 scripts/microk8s.py deploy
microk8s-test: require-local
	python3 scripts/microk8s.py test
microk8s-forward: require-local
	python3 scripts/microk8s.py forward
workload-identity-setup: require-local
	python3 scripts/workload_identity.py setup
workload-identity-check: require-local
	python3 scripts/workload_identity.py check

secrets-init:
	python3 scripts/sops_secrets.py init
secrets-import: require-local
	python3 scripts/microk8s.py encrypt-secrets
secrets-edit:
	python3 scripts/sops_secrets.py edit
secrets-check:
	python3 scripts/sops_secrets.py check
secrets-exec:
	$(SECRET_EXEC) $(CMD)
secrets-use-gcp:
	python3 scripts/sops_secrets.py use-gcp '$(KMS_KEY)'
