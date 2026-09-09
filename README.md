# Romana Learning

**Puțin** is a Romanian practice app built with Go, React, TypeScript, and PostgreSQL. Anyone can practise published questions. GitHub sign-in unlocks private course materials, question management, and personal practice history. Anonymous answers are graded without saving history.

## Run locally

Requirements on macOS: Lima, Python 3, SOPS, the Google Cloud CLI, and a running Docker engine with Compose. Install SOPS with `brew install sops`.

Set up Google authentication and a Cloud KMS key as described under [Encrypted secrets](#encrypted-secrets). For a new installation, run `make secrets-init`, then `make secrets-edit` to enter API and GitHub settings. To migrate an existing local MicroK8s installation and its `.env`, use `make secrets-import` instead.

```sh
make workload-identity-setup  # Once, with an authorized Google setup account
make up
```

Open http://localhost:8080. `make up` builds the image and deploys the Helm chart into a local MicroK8s VM, using Google Cloud Storage by default and persistent PostgreSQL. For local Google storage, the app authenticates through its federated Kubernetes identity. Uploads and question management require GitHub sign-in.

Set `GOOGLE_CLOUD_PROJECT`, `COURSE_STORAGE_BUCKET`, `COURSE_STORAGE_PREFIX`, and `GCS_WIF_PROVIDER` with `make secrets-edit`. `GCS_SERVICE_ACCOUNT` belongs only in production secrets; local runs do not use the Google service-account annotation. The provider uses the form `projects/NUMBER/locations/global/workloadIdentityPools/POOL/providers/PROVIDER`. The bucket must already exist. Run `make microk8s-setup` before the initial identity setup. See [Workload Identity configuration](charts/romanian/README.md#workload-identity-on-microk8s).

For local MinIO storage, run `make up-local` instead, or set `ROMANA_STORAGE=minio` for `make microk8s-deploy`. MinIO stores originals in its private `courses` bucket and exposes its console at http://localhost:9001. Its credentials come from SOPS. Local storage still needs Google access to decrypt the KMS-backed settings; once running, it does not need Google Storage.

When switching a running MicroK8s installation between storage modes, the helper first checks identity access, pauses uploads, copies database-referenced originals, and verifies every SHA-256 hash before bringing the app back. It retains source copies and restores the previous deployment if copying or installation fails. Switching requires access to both storage services; the host's Google ADC is used only for administrative copying and test cleanup, never mounted in the app. Database records and history stay in the same database. Old Compose services are stopped to avoid competing local stacks.

Keep the forwarding command running while using the app. Ctrl-C stops forwarding; `make microk8s-forward` opens it again. To stop the VM:

```sh
make down
```

Stopping preserves the VM and its volumes. Removing the VM or namespace can delete stored data. `make up` starts it again. Existing Compose data is separate; back up and migrate its database and originals before switching an older installation to MicroK8s.

## GitHub sign-in

[Register a GitHub OAuth app](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/creating-an-oauth-app) with homepage `http://localhost:8080` and authorization callback `http://localhost:8080/auth/github/callback`. Run `make secrets-edit` and set `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET` in the encrypted settings. Set `APP_BASE_URL` to the same origin. Preserve existing API settings.

Run `make up` after configuration changes. The helper decrypts the settings in memory and copies OAuth credentials into a Kubernetes Secret. Without both credentials, sign-in is unavailable and uploads, generation, and question management remain locked. Published practice stays available.

The app requests only the public GitHub profile. It uses a seven-day, HttpOnly session cookie; sessions and individual history live in PostgreSQL. GitHub access tokens are used during sign-in and are not retained. Anonymous practice does not create a session, save answers, or use browser storage to track progress. Signing in does not import earlier anonymous answers.

When upgrading an older single-user database, `AUTH_LEGACY_GITHUB_ID` can identify the numeric GitHub account ID that owns its existing materials and answers. Those records are assigned after that exact account signs in. Leaving it blank keeps the older records unassigned; a new user cannot claim them automatically. Back up the database before upgrading.

## Course materials

Sign in and open **Course library** to upload DOCX, text-based PDF, or UTF-8 TXT files, up to 20 MB. Review the extracted teaching text, generate draft questions, check their answers, and publish them into lesson practice. Homework and learner submissions are stored as reference material. Scanned PDFs require OCR before import.

The local generator creates sentence gaps and multiple-choice questions from source examples. Each draft retains a source quote. Select draft questions, review their answers, and use **Publish selected** to publish them together, including current edits. **Delete question** removes a draft or published question from the library and practice while preserving previously saved answers. Changed course documents can be uploaded as new revisions.

Practice starts with five built-in questions and draws up to ten random questions from that starter set and published course questions. It shows one question at a time after starting, with no preview of upcoming questions. Publishing makes questions and their feedback available to everyone; original documents, unpublished drafts, and personal history remain private to their owner.

Original files use Google Cloud Storage by default, with MinIO available through the explicit local storage mode. The chart also supports an existing S3-compatible service or persistent filesystem. Document metadata, reviewed text, drafts, and progress are stored in PostgreSQL. Back up both the database and original-file storage. The Docker image includes PDF extraction; host development requires `pdftotext` for PDF uploads.

### Optional API generation

The adapter uses the [OpenAI-compatible chat-completions format](https://developers.openai.com/api/reference/python/resources/chat/subresources/completions/methods/create) with JSON responses. Run `make secrets-edit` to configure a complete HTTPS endpoint in the encrypted settings:

- `EXERCISE_API_URL`: complete endpoint or API base URL ending in `/v1`, including any required query parameters.
- `EXERCISE_API_KEY`: optional bearer token.
- `EXERCISE_API_MODEL`: model name; required for base URLs. It can be omitted for a complete endpoint that selects its own model.
- `EXERCISE_API_EFFORT`: optional `reasoning_effort` value (`none`, `minimal`, `low`, `medium`, `high`, `xhigh`, or `max`). Leave blank to omit the field and use the provider default. Supported levels depend on the selected model.
- `EXERCISE_API_EFFORT_FORMAT`: leave blank or use `reasoning_effort` for standard Chat Completions. Set `reasoning` for compatible gateways that require the nested `reasoning.effort` field. The chosen effort level is unchanged.

`make up` and `make microk8s-deploy` decrypt these settings with SOPS in memory and synchronize the API variables to a dedicated Kubernetes Secret. Values are passed through stdin and are not printed or stored in Helm values. The app restarts when settings change. Stop forwarding before redeploying, then run `make microk8s-forward` again. Without a configured URL, AI generation is disabled. For other clusters, set `generation.existingSecret` to an existing Secret with these variable names as keys.

Choose **AI API** in the generation controls to send the reviewed teaching text to the configured provider. Uploading documents does not trigger API calls. The adapter locates exact source quotes in the teaching text and computes their line references. It then makes a separate LLM request to check Romanian grammar, spelling, diacritics, answer correctness, ambiguity, and explanations against the lesson context. Only candidates approved by that review are saved as drafts; skipped candidates are reported. If the review fails or returns incomplete decisions, nothing is saved. The loader stays visible during both steps, which use the configured provider, model, and effort and can take up to two minutes. AI generation is selected by default when configured. The explicitly labeled local method remains offline and does not receive this AI review. If every candidate fails, nothing is saved and the error identifies a failed rule. Provider errors remain redacted. Review remains necessary for language accuracy. API generation accepts up to 30 KB of reviewed text per request; split larger documents into lessons.

## Development

Requirements: Go 1.26+, Node 24+, SOPS, the Google Cloud CLI, and Docker with Compose.

```sh
make build
make up
```

`make build` compiles the frontend and Go binary. `make up` rebuilds and deploys the container image into the same local MicroK8s environment.

## Configuration

PostgreSQL settings come from environment variables. Use `DATABASE_URL`, or configure `PGHOST`, `PGPORT`, `PGDATABASE`, `PGUSER`, `PGPASSWORD`, and `PGSSLMODE`. Select a dedicated application database.

### Encrypted secrets

[SOPS](https://getsops.io/docs/) keeps independent settings in `config/secrets.local.enc.yaml` and `config/secrets.prod.enc.yaml`, encrypted with Google Cloud KMS. The encrypted files and `.sops.yaml`, which contains the KMS resource name, can be committed. Google credential files, legacy private keys, plaintext environment files, and private deployment values are excluded from Git and build inputs. The matching `config/secrets.local.example.yaml` and `config/secrets.prod.example.yaml` files document their fields.

| Selection | Encrypted file | PostgreSQL | MinIO |
| --- | --- | --- | --- |
| `APP_ENV=local` (default) | `config/secrets.local.enc.yaml` | Existing local credentials; host/database supplied by the local runtime | Local credentials included |
| `APP_ENV=prod` | `config/secrets.prod.enc.yaml` | Independent cloud host, port, database, username, password, and TLS mode | Excluded |

```sh
make APP_ENV=local secrets-edit
make APP_ENV=prod secrets-edit
make APP_ENV=prod secrets-check
```

Enter the existing cloud database connection in the production file: `PGHOST`, `PGPORT`, `PGDATABASE`, `PGUSER`, `PGPASSWORD`, and `PGSSLMODE`. Use the dedicated application database on that instance. Set production API settings, GitHub credentials and HTTPS `APP_BASE_URL`, and Google storage identity in the same file. The production file starts with empty credentials, port `5432`, and TLS mode `verify-full`; checking or executing it requires all database fields to be filled. Initialization creates no database or user and copies no local credentials.

For a fresh production configuration, run `make APP_ENV=prod secrets-init` once. It refuses to overwrite an existing encrypted file and leaves the local file untouched. Production settings never fall back to the local file. A deployment runner can load them using `make APP_ENV=prod secrets-exec CMD='your-deployment-command'`; the wrapper clears inherited connection URLs and credentials before loading the selected file. For a custom PostgreSQL CA, configure the deployment's TLS Secret as described in the chart documentation.

`make up`, `make up-local`, `make db`, `make backend`, and the MicroK8s helpers manage local infrastructure and reject `APP_ENV=prod` before making changes. The Compose stack also rejects the production environment. Selecting production secrets does not deploy production or retarget local PostgreSQL. Production Helm deployments continue to use their own namespace, referenced database Secret, and existing cloud database.

SOPS uses Google [Application Default Credentials](https://docs.cloud.google.com/docs/authentication/provide-credentials-adc). For local development, sign in through the Google Cloud CLI:

```sh
gcloud auth application-default login
```

Choose an existing symmetric `ENCRYPT_DECRYPT` Cloud KMS key. The deploying identity needs `roles/cloudkms.cryptoKeyEncrypterDecrypter` on that key. On a Google-hosted runner, use the attached workload identity; an external runner can use an authorized credential configuration through `GOOGLE_APPLICATION_CREDENTIALS`. Credential files stay outside the repository. A normal `gcloud auth login` session alone does not configure ADC.

To migrate an existing age-encrypted installation:

```sh
make secrets-use-gcp KMS_KEY='projects/PROJECT/locations/LOCATION/keyRings/RING/cryptoKeys/KEY'
```

This command re-encrypts the settings in memory with a new data key and only the selected Google KMS recipient. It verifies Google-authenticated decryption and unchanged values before replacing the encrypted file and the project's SOPS rule. Failed Google authentication, missing KMS permissions, or a failed verification leave the working file intact. The old age key is not used for decryption after a successful switch.

For a fresh installation without `.sops.yaml`, set `SOPS_GCP_KMS_IDS` to the full KMS key resource name before initialization. The helper creates a Google KMS rule and does not generate an age key. Key creation, API enablement, and IAM grants are managed in Google Cloud separately.

```sh
make secrets-init    # New installation: generate credentials and encrypt them
make secrets-import  # Existing MicroK8s installation: preserve its credentials and .env settings
make secrets-edit    # Open the encrypted settings in your editor through SOPS
make secrets-check   # Verify decryption and structure without displaying values
```

Use either initialization or import once. Import verifies encryption and decryption before removing the old `.env`. Normal startup does not read `.env`; a missing key or invalid ciphertext stops deployment before cluster changes. Existing PostgreSQL/MinIO credentials must match the encrypted settings. Changing database credentials also requires changing the database role password; the helper refuses to silently rotate credentials on an existing data volume.

To share decryption access, grant the collaborator the appropriate KMS role on the selected key. The encryption key stays in Google Cloud; no private age key needs to be distributed. Keep the KMS key available for as long as encrypted revisions need to be recovered.

Make commands accept `APP_ENV=local|prod`; direct Python commands use `ROMANA_ENV=local|prod`. `ROMANA_SECRETS_FILE` optionally overrides the file within the selected environment; its fields must match that environment. Local and production schemas cannot be mixed. The Google migration command updates the project's single creation rule; custom SOPS configurations with additional rules must be managed separately.

The project ID, bucket name, and service-account address are encrypted alongside the application settings. SOPS must retain the KMS key resource name in readable metadata so it can locate the decryption key; that locator includes its project ID.

The deployment process passes decrypted values to Kubernetes through stdin. Helm stores Secret references rather than credentials; the app receives only its required runtime variables and never receives Google KMS decryption credentials. SOPS protects files in version control; runtime access is still controlled by Kubernetes RBAC and the cluster's encryption-at-rest configuration.

The optional Compose stack and `make backend` use local MinIO. For Google storage with rotating Kubernetes identity tokens, use the MicroK8s workflow. Pass Compose settings through the wrapper:

```sh
make secrets-exec CMD='docker compose build app'
```

The wrapper loads the selected values into the child process environment and disables Compose's implicit `.env` loading. Local selection sets the existing Compose project name; production cannot use that local stack. `make db`, `make storage`, and `make backend` use it too. Compose volumes are separate from MicroK8s: existing volumes require their original credentials. Do not run a second stack on the same ports. Avoid commands that print the environment or rendered Compose configuration because their output includes credentials.

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

Browser tests require Google Chrome. They provision temporary sessions through the test database and remove their own accounts, records, and original-file objects through the MicroK8s helper. No test login endpoint is exposed by the app.

Health endpoints: `/healthz` and `/readyz`. For signed-in users, retrying an answer with the same attempt ID does not duplicate progress.

## Kubernetes

The [Helm chart](charts/romanian/README.md) connects to an existing PostgreSQL instance and supports configurable storage, TLS, Secret references, and node placement. Its optional MinIO dependency provisions object storage and configured buckets. The chart does not provision PostgreSQL, a logical database, or a node. The default Service is ClusterIP. Public Ingress requires GitHub credentials and an HTTPS application origin.

`make APP_ENV=prod chart` renders the production chart with NGINX ingress and cert-manager's ACME annotation. The hostname comes from encrypted `APP_BASE_URL`, keeping the ingress, certificate, and GitHub callback aligned. Production database settings and the Google service-account annotation also come from the production SOPS file. See [ingress configuration and verification](charts/romanian/README.md#nginx-ingress-and-automatic-tls). Rendering does not deploy the app.

### Local MicroK8s checks

On macOS, install Lima and run Docker before these commands:

```sh
make microk8s-setup
make secrets-exec CMD='docker compose build app'
make microk8s-deploy
make microk8s-test
make microk8s-forward
```

The setup creates a dedicated Ubuntu VM with 2 CPUs, 4 GiB RAM, and a 40 GiB virtual disk, then installs MicroK8s 1.35 with DNS and hostpath storage. Deployment imports the local app image and installs the Helm chart with Google storage by default; `ROMANA_STORAGE=minio` enables the bundled MinIO dependency. The helper supplies PostgreSQL and copies SOPS-encrypted database, storage, API, and GitHub settings to separate runtime Secrets in the application namespace.

The test creates a temporary account, uploads a synthetic lesson, generates and publishes a question, verifies anonymous and signed-in grading, then checks the original file and personal progress after restarting the app and PostgreSQL pods, plus MinIO when it is enabled. It removes its temporary account, lesson, and answer. Stop any existing forwarding process before testing.

With forwarding running, open http://localhost:8080. MinIO mode also forwards its console at http://localhost:9001. Ctrl-C stops forwarding. `make down` stops the VM while preserving its data; `make microk8s-setup` starts it again. `MICROK8S_VM`, `MICROK8S_NAMESPACE` (starting with `romana-test`), and `MICROK8S_PORT` can override the helper defaults.
