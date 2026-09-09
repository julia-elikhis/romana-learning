# Romana Learning

**Puțin** is a Romanian practice app built with Go, React, TypeScript, and PostgreSQL. Anyone can practise published questions. GitHub sign-in unlocks private course materials, question management, and personal practice history. Anonymous answers are graded without saving history.

## Run locally

Requirements on macOS: Lima, Python 3, and a running Docker engine with Compose.

```sh
make up
```

Open http://localhost:8080. The command builds the image, deploys the Helm chart into a local MicroK8s VM, and forwards the app and MinIO console to localhost. PostgreSQL and MinIO use persistent Kubernetes volumes. Any old Compose services are stopped so there is one running application. Uploads and question management require GitHub sign-in, including when running locally.

MinIO stores uploaded originals in the private `courses` bucket. Its console is at http://localhost:9001. Credentials are generated at setup and stored in the `test-minio` Kubernetes Secret, under `rootUser` and `rootPassword`.

Keep the forwarding command running while using the app. Ctrl-C stops forwarding; `make microk8s-forward` opens it again. To stop the VM:

```sh
make down
```

Stopping preserves the VM and its volumes. Removing the VM or namespace can delete stored data. `make up` starts it again. Existing Compose data is separate; back up and migrate its database and originals before switching an older installation to MicroK8s.

## GitHub sign-in

[Register a GitHub OAuth app](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/creating-an-oauth-app) with homepage `http://localhost:8080` and authorization callback `http://localhost:8080/auth/github/callback`. Put `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET` in the private `.env` file using `.env.example` as a template. Set `APP_BASE_URL` to the same origin. Preserve any existing API settings in that file.

Run `make up` after configuration changes. The helper copies OAuth credentials into a Kubernetes Secret. Without both credentials, sign-in is unavailable and uploads, generation, and question management remain locked. Published practice stays available.

The app requests only the public GitHub profile. It uses a seven-day, HttpOnly session cookie; sessions and individual history live in PostgreSQL. GitHub access tokens are used during sign-in and are not retained. Anonymous practice does not create a session, save answers, or use browser storage to track progress. Signing in does not import earlier anonymous answers.

When upgrading an older single-user database, `AUTH_LEGACY_GITHUB_ID` can identify the numeric GitHub account ID that owns its existing materials and answers. Those records are assigned after that exact account signs in. Leaving it blank keeps the older records unassigned; a new user cannot claim them automatically. Back up the database before upgrading.

## Course materials

Sign in and open **Course library** to upload DOCX, text-based PDF, or UTF-8 TXT files, up to 20 MB. Review the extracted teaching text, generate draft questions, check their answers, and publish them into lesson practice. Homework and learner submissions are stored as reference material. Scanned PDFs require OCR before import.

The local generator creates sentence gaps and multiple-choice questions from source examples. Each draft retains a source quote. Select draft questions, review their answers, and use **Publish selected** to publish them together, including current edits. **Delete question** removes a draft or published question from the library and practice while preserving previously saved answers. Changed course documents can be uploaded as new revisions.

Practice starts with five built-in questions and draws up to ten random questions from that starter set and published course questions. It shows one question at a time after starting, with no preview of upcoming questions. Publishing makes questions and their feedback available to everyone; original documents, unpublished drafts, and personal history remain private to their owner.

Original files use MinIO locally. Deployment storage can use an S3-compatible service, Google Cloud Storage, or a persistent filesystem. Document metadata, reviewed text, drafts, and progress are stored in PostgreSQL. Back up both the database and original-file storage. The Docker image includes PDF extraction; host development requires `pdftotext` for PDF uploads.

### Optional API generation

The adapter uses the [OpenAI-compatible chat-completions format](https://developers.openai.com/api/reference/python/resources/chat/subresources/completions/methods/create) with JSON responses. Configure a complete HTTPS endpoint in a private `.env` file using `.env.example` as a template:

- `EXERCISE_API_URL`: complete endpoint or API base URL ending in `/v1`, including any required query parameters.
- `EXERCISE_API_KEY`: optional bearer token.
- `EXERCISE_API_MODEL`: model name; required for base URLs. It can be omitted for a complete endpoint that selects its own model.
- `EXERCISE_API_EFFORT`: optional `reasoning_effort` value (`none`, `minimal`, `low`, `medium`, `high`, `xhigh`, or `max`). Leave blank to omit the field and use the provider default. Supported levels depend on the selected model.
- `EXERCISE_API_EFFORT_FORMAT`: leave blank or use `reasoning_effort` for standard Chat Completions. Set `reasoning` for compatible gateways that require the nested `reasoning.effort` field. The chosen effort level is unchanged.

`make up` and `make microk8s-deploy` read these settings through a short-lived container and synchronize the API variables to a dedicated Kubernetes Secret. Values are passed through stdin and are not printed or stored in Helm values. The app restarts when settings change. Stop forwarding before redeploying, then run `make microk8s-forward` again. Without a configured URL, AI generation is disabled. For other clusters, set `generation.existingSecret` to an existing Secret with these variable names as keys.

Choose **AI API** in the generation controls to send the reviewed teaching text to the configured provider. Uploading documents does not trigger API calls. The adapter locates exact source quotes in the teaching text and computes their line references. It then makes a separate LLM request to check Romanian grammar, spelling, diacritics, answer correctness, ambiguity, and explanations against the lesson context. Only candidates approved by that review are saved as drafts; skipped candidates are reported. If the review fails or returns incomplete decisions, nothing is saved. The loader stays visible during both steps, which use the configured provider, model, and effort and can take up to two minutes. AI generation is selected by default when configured. The explicitly labeled local method remains offline and does not receive this AI review. If every candidate fails, nothing is saved and the error identifies a failed rule. Provider errors remain redacted. Review remains necessary for language accuracy. API generation accepts up to 30 KB of reviewed text per request; split larger documents into lessons.

## Development

Requirements: Go 1.26+, Node 24+, and Docker with Compose.

```sh
make build
make up
```

`make build` compiles the frontend and Go binary. `make up` rebuilds and deploys the container image into the same local MicroK8s environment.

## Configuration

PostgreSQL settings come from environment variables. Use `DATABASE_URL`, or configure `PGHOST`, `PGPORT`, `PGDATABASE`, `PGUSER`, `PGPASSWORD`, and `PGSSLMODE`. Select a dedicated application database.

Keep private configuration and credentials outside version control. Environment files, credential files, and private deployment values are ignored by Git.

## Checks

```sh
make test
go vet ./...
```

Set `TEST_DATABASE_URL` to a local test instance to include the database integration test in `go test ./...`. It creates and removes an isolated schema and checks migrations, source review, publication, deletion, revisions, grading retries, OAuth state/PKCE, and account isolation. OAuth tests use a simulated GitHub provider.

With the MicroK8s app running and forwarding active:

```sh
cd web && npx playwright test
```

Browser tests require Google Chrome. They provision temporary sessions through the test database and remove their own accounts, records, and MinIO objects through the MicroK8s helper. No test login endpoint is exposed by the app.

Health endpoints: `/healthz` and `/readyz`. For signed-in users, retrying an answer with the same attempt ID does not duplicate progress.

## Kubernetes

The [Helm chart](charts/romanian/README.md) connects to an existing PostgreSQL instance and supports configurable storage, TLS, Secret references, and node placement. Its optional MinIO dependency provisions object storage and configured buckets. The chart does not provision PostgreSQL, a logical database, or a node. The default Service is ClusterIP. Public Ingress requires GitHub credentials and an HTTPS application origin.

### Local MicroK8s checks

On macOS, install Lima and run Docker before these commands:

```sh
make microk8s-setup
docker compose build app
make microk8s-deploy
make microk8s-test
make microk8s-forward
```

The setup creates a dedicated Ubuntu VM with 2 CPUs, 4 GiB RAM, and a 40 GiB virtual disk, then installs MicroK8s 1.35 with DNS and hostpath storage. Deployment imports the local app image and installs the Helm chart with MinIO. The helper supplies PostgreSQL and generated database/storage credentials in the application namespace. Configured API and GitHub variables from `.env` are synchronized to separate Secrets.

The test creates a temporary account, uploads a synthetic lesson, generates and publishes a question, verifies anonymous and signed-in grading, then checks the original file and personal progress after restarting the app, MinIO, and PostgreSQL pods. It removes its temporary account, lesson, and answer. Stop any existing forwarding process before testing.

With forwarding running, open http://localhost:8080 or the MinIO console at http://localhost:9001. Ctrl-C stops forwarding. `make down` stops the VM while preserving its data; `make microk8s-setup` starts it again. `MICROK8S_VM`, `MICROK8S_NAMESPACE` (starting with `romana-test`), and `MICROK8S_PORT` can override the helper defaults.
