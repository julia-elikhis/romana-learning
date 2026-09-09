# Romana Learning

**Puțin** is a Romanian practice app built with Go, React, TypeScript, and PostgreSQL. Answers are graded by the server, and progress persists across browser refreshes and container restarts.

## Run locally

Requirements: Docker with Compose.

```sh
make up
```

Open http://localhost:8080. The app and PostgreSQL run in containers with ports bound to localhost. This is a single-user application; it does not provide account authentication.

```sh
make down
```

Stopping the stack preserves its data volumes. `docker compose down -v` deletes them.

## Development

Requirements: Go 1.26+, Node 24+, and Docker with Compose.

```sh
make db
cd web && npm ci && npm run build
```

Run `make backend` and `make frontend` in separate terminals. Open http://localhost:5173; Vite proxies API requests to Go. Stop the containerized app before running the host backend on the same port.

## Configuration

PostgreSQL settings come from environment variables. Use `DATABASE_URL`, or configure `PGHOST`, `PGPORT`, `PGDATABASE`, `PGUSER`, `PGPASSWORD`, and `PGSSLMODE`. Select a dedicated application database.

Keep private configuration and credentials outside version control. Environment files, credential files, and private deployment values are ignored by Git.

## Checks

```sh
make test
go vet ./...
```

With the local stack running:

```sh
python3 scripts/smoke.py
python3 scripts/smoke_database_config.py
cd web && npx playwright test
```

Browser tests require Google Chrome. The API smoke test restarts the local containers. Tests remove their own temporary records; the database configuration test creates and removes a separate temporary database.

Health endpoints: `/healthz` and `/readyz`. Retrying an answer with the same attempt ID does not duplicate progress.

## Kubernetes

The [Helm chart](charts/romanian/README.md) connects to an existing PostgreSQL instance and supports configurable storage, TLS, Secret references, and node placement. It requires Kubernetes and does not provision a database server, logical database, bucket, or node. Access is private through a ClusterIP service; public Ingress is disabled.
