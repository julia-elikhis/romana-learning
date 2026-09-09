# Romana Learning

A Romanian learning platform for student practice, with planned tools for teachers to organize materials, classes, and homework. **Puțin** is the app's display name.

GitHub repository: `julia-elikhis/romana-learning`. The local folder and existing Docker/Helm resource names remain `romanian`.

Go API, React/TypeScript frontend, PostgreSQL persistence, Docker Compose for local use, and a Helm chart for a future Kubernetes host.

This first runnable slice has one five-question foundation mission. Answers are graded on the server and saved immediately. Progress survives browser refreshes and container restarts. It is a **single-user local development mode**, not the complete B1 app: accounts, scheduling, longer missions, course import, and AI/speech are planned.

## Start locally

Requirements: Docker with Compose. On this Mac we use Colima rather than Docker Desktop.

```sh
colima start --cpu 2 --memory 4 --disk 30
make up
```

Open **http://localhost:8080**. Both the app and Postgres run in Docker. Ports bind to loopback only. No cloud account, cookies, or AI key is required. The database uses development-only credentials in `compose.yaml` and a named persistent volume.

On Homebrew installations where `docker compose` is not discovered, use `docker-compose up -d --build --wait`, or configure the CLI plugin directory `/opt/homebrew/lib/docker/cli-plugins` in Docker's user configuration.

```sh
docker compose logs -f app
make down
```

`make down` keeps database data. Avoid `docker compose down -v` unless you explicitly intend to delete all local progress.

## Development with hot reload

Install Go 1.26+ and Node 24+, then:

```sh
make db
cd web && npm ci && npm run build
```

In separate terminals from the repository root:

```sh
make backend
make frontend
```

Open http://localhost:5173. Vite proxies API requests to Go. Restart Go after backend changes; frontend changes reload automatically. `make up` is the simpler all-container alternative; stop that app container before running the host backend on port 8080.

## Checks

```sh
make test
go vet ./...
helm template romanian charts/romanian
```

With the Compose stack running, verify database persistence and the browser flow:

```sh
python3 scripts/smoke.py
python3 scripts/smoke_database_config.py
cd web && npx playwright test
```

The browser checks use an installed Google Chrome. Smoke tests create uniquely identified attempts and remove only their own records. The API smoke test restarts the local app and Postgres containers, so run it outside an active practice session. The database-configuration check creates and removes its own temporary database/container on the local instance to verify database isolation.

The server exposes `/healthz` (process health), `/readyz` (database connection), `/api/exercises`, `/api/attempts`, and `/api/progress`. Duplicate retries with the same attempt ID cannot increment progress twice. A reused ID with different content returns a conflict.

## Helm: existing Google Cloud resources

The chart connects to an existing Postgres instance with a separate application database. It supports course-file PVCs (existing or optionally created), GCS bucket/identity configuration, database TLS CA mounts, Secret references, configurable node placement and resource limits. It does not provision a Postgres instance, logical database, bucket, or node.

- [All values](charts/romanian/values.yaml)
- [Existing node and disk example](charts/romanian/values-gcp-node.example.yaml)
- [Google Cloud Storage example](charts/romanian/values-gcp-gcs.example.yaml)
- [Deployment and secret setup](charts/romanian/README.md)

Database parameters are consumed by Go; the course-file configuration prepares infrastructure for the upcoming upload/storage implementation. The app still accepts only single-user local mode and blocks public Ingress. A private Kubernetes test can use port-forwarding. No cluster deployment has been performed. Helm requires Kubernetes; a standalone Google Cloud VM can run the containers via Compose instead.

Chart regression tests run with `go test ./internal/charttest` when Helm is installed. They check both profiles, PVC/Secret reuse, dedicated database selection, TLS, and rejection of invalid settings.

Database initialization runs at startup using idempotent schema creation inside the selected database. Use versioned migrations and a dedicated migration job before introducing evolving production schemas or multiple replicas. Backups of the database and course storage are managed separately.

## Planning

- [App plan](APP_PLAN.md)
- [December exam plan](EXAM_PLAN.md)
- [Technical plan](TECH_PLAN.md)
