# Existing Google Cloud infrastructure

This chart installs the app into an existing Kubernetes cluster. It uses an existing PostgreSQL instance and configurable course storage: an existing PVC, Google Cloud Storage, an S3-compatible service, or the optional MinIO dependency. Enabling MinIO provisions its storage and configured buckets. The chart creates no VM, node pool, Postgres server, logical database, or IAM binding.

If the existing node is a standalone Compute Engine VM without Kubernetes, use Docker Compose/container deployment there instead. Helm requires Kubernetes; it does not install Kubernetes onto a VM.

## Choose a profile

- `values-gcp-node.example.yaml`: existing node, existing course-files PVC, and external Postgres.
- `values-gcp-gcs.example.yaml`: existing bucket with Google identity, and external Postgres. Set node placement if needed.
- `values-microk8s.yaml`: Google storage with Kubernetes Workload Identity. The helper supplies encrypted configuration and local PostgreSQL.
- `values-minio.yaml`: add after the MicroK8s profile for bundled MinIO with hostpath storage.
- `values-prod.yaml`: production Secret references, external Postgres, and Google storage on GKE; the renderer supplies SOPS identifiers.
- `values-ingress-nginx.yaml`: public HTTPS through an existing NGINX controller and cert-manager's default ACME issuer.
- `values.yaml`: all available options and a minimal profile using a connection URL Secret.

Google storage is the default. Its bucket, prefix, and project are loaded from the Secret named by `storage.gcs.settingsSecret`. The local helper decrypts these identifiers from SOPS; production service-account addresses are supplied from encrypted settings at deployment time. Set `settingsSecret: ""` to use explicit `project`, `bucket`, and `prefix` values instead.

Copy a profile to a private deployment values file and replace its placeholders. Use the actual image registry/tag built from this source; placeholder images are not published. Build the container image for the target node architecture.

The pinned dependency archive is included. Run `helm dependency build charts/romanian` to restore it from `Chart.lock` if needed.

```sh
helm lint charts/romanian -f /path/to/private-values.yaml
helm template romanian charts/romanian -f /path/to/private-values.yaml
# Run only when the cluster, image, database, Secrets, and storage are ready:
helm upgrade --install romanian charts/romanian \
  --namespace romanian --create-namespace \
  -f /path/to/private-values.yaml --wait --timeout 5m
```

## Existing PostgreSQL instance, separate database

Create a dedicated logical database (for example `romanian`) and an application role on the existing instance through your normal database administration process. The role needs connection and schema/table permissions within this database; it does not need superuser or permission to create databases. Existing unrelated databases are outside the application's scope. The app initializes its tables in the selected database and does not execute `CREATE DATABASE`.

Preferred chart settings:

```yaml
database:
  mode: parameters
  host: postgres.internal.example
  port: 5432
  name: romanian
  existingSecret: romanian-database
  usernameKey: username
  passwordKey: password
  sslMode: verify-full
  tls:
    existingSecret: romanian-postgres-ca
    caKey: ca.crt
```

The credential Secret must exist in the release namespace and contain the configured keys. Provision it with your secret manager or a locally protected file; don't put plaintext credentials in chart values or shell history. The optional CA Secret must also be in that namespace. Omit it when the server certificate is trusted by the image's system roots. Set the host to a name covered by the certificate. `sslMode` is configurable for your actual connection path.

Alternatively, `database.mode: url` reads the complete URL from `database.existingSecret` / `database.urlKey`. The URL must select the dedicated database and its TLS settings. Credentials from parameters mode are not injected in URL mode. Rotating an environment-based Secret requires restarting the Deployment; updating the ConfigMap automatically changes the pod-template checksum.

The database endpoint must be reachable from the cluster through the existing network/firewall configuration. If the instance is Cloud SQL rather than self-managed Postgres, choose a supported connection route before deployment. This chart does not silently add a Cloud SQL Auth Proxy. An independently managed proxy can be used as the configured endpoint. See [Google's GKE/Postgres connection guide](https://docs.cloud.google.com/sql/docs/postgres/connect-kubernetes-engine).

## Course files on an existing disk

```yaml
storage:
  provider: filesystem
  filesystem:
    mountPath: /data/courses
    persistence:
      existingClaim: romanian-course-files
      create: false
nodeSelector:
  kubernetes.io/hostname: YOUR_EXISTING_NODE
```

The existing claim must be in the release namespace and backed by the disk you intend to use. A disk or directory on a node is not automatically a Kubernetes PVC: the cluster owner must expose it through a PV/CSI driver with correct node affinity. The chart intentionally does not mount an arbitrary host directory. For an existing disk, use `existingClaim`; setting `create: true` may dynamically allocate new storage and incur charges.

Optional claim creation is available using `create`, `size`, `storageClass`, and `accessModes`. A null class uses the cluster default; an empty string disables dynamic provisioning. Created claims are marked for retention on uninstall by default. They become orphaned and need deliberate reattachment/adoption on reinstall; retention is not a backup. Existing claims are never owned by this chart.

The app runs as UID/GID 10001 with fsGroup 10001. Ensure the volume supports these permissions; this is especially important for pre-existing/local volumes. Single-writer volumes use one replica and a Recreate rollout. The app filesystem remains read-only except the course mount and temporary extraction space. See [Kubernetes persistent volume behavior](https://kubernetes.io/docs/concepts/storage/persistent-volumes/).

## Course files in a Google Cloud Storage bucket

```yaml
storage:
  provider: gcs
  gcs:
    settingsSecret: ""
    project: your-project
    bucket: your-existing-private-bucket
    prefix: romanian/courses/
serviceAccount:
  create: true
  annotations:
    iam.gke.io/gcp-service-account: romanian@your-project.iam.gserviceaccount.com
```

`serviceAccount.gcpServiceAccount` renders `iam.gke.io/gcp-service-account` on the chart-managed Kubernetes ServiceAccount. The Deployment uses that same account through `serviceAccountName`. Other entries in `serviceAccount.annotations` are preserved; conflicting Google identity annotations are rejected. An externally managed Kubernetes account must be annotated through its owner.

A production deployment supplies the `iam.gke.io/gcp-service-account` annotation from `GCS_SERVICE_ACCOUNT` in production SOPS settings. Local settings omit that field, and local deployments receive no Google service-account annotation. The address stays in SOPS-encrypted settings in the repository. The helper supplies the selected environment’s account through Helm values on stdin. Kubernetes annotations and Helm release metadata contain the resolved address at deployment time.

On GKE, configure Workload Identity on the existing cluster/node pool and authorize the Kubernetes principal or linked Google service account for the intended bucket. The annotation alone does not grant permission. IAM setup is external to Helm. GKE's metadata server supplies credentials without a mounted JSON key. A bucket prefix is organization, not an independent access boundary; enforce permissions through IAM and the application. See [Workload Identity](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/workload-identity) and [Storage authentication](https://docs.cloud.google.com/storage/docs/authentication).

### Workload Identity on MicroK8s

Non-GKE Kubernetes uses a Google Workload Identity pool/provider with the cluster's public JWKS uploaded to Google. The cluster need not expose its API publicly. The app's projected Kubernetes token expires after one hour and rotates automatically; its audience is limited to the configured provider. For local Google storage, the Google SDK exchanges it for direct access as the federated Kubernetes principal. Production configurations with a Google service account can use impersonation. The app receives no private service-account key or user refresh token. See [Google's Kubernetes federation guide](https://docs.cloud.google.com/iam/docs/workload-identity-federation-with-kubernetes).

```yaml
storage:
  provider: gcs
  gcs:
    settingsSecret: google-storage
    workloadIdentity:
      enabled: true
      provider: projects/PROJECT_NUMBER/locations/global/workloadIdentityPools/POOL/providers/PROVIDER
      credentialsSecret: google-workload-identity
```

The settings Secret contains `GOOGLE_CLOUD_PROJECT`, `COURSE_STORAGE_BUCKET`, and `COURSE_STORAGE_PREFIX`. The identity Secret contains `credentials.json` with type `external_account`, Google STS as `token_url`, the provider as `audience`, `/var/run/secrets/google/token` as the credential source, and, only when impersonation is configured, the selected account's `generateAccessToken` URL. Provision this configuration through a trusted deployment runner. The local helper builds it from SOPS settings and uses versioned Secret references so Helm rollback preserves the old storage configuration. The projected token's audience is `https://iam.googleapis.com/` followed by the provider resource.

Run `make workload-identity-setup` once after `make microk8s-setup`. The setup account needs Workload Identity Pool Admin on the pool project and `storage.buckets.getIamPolicy` / `setIamPolicy` on the bucket. Impersonation additionally requires `iam.serviceAccounts.getIamPolicy` / `setIamPolicy` on the selected Google service account. Enable the IAM, IAM Service Account Credentials, and Security Token Service APIs in that project. The runtime app does not need these administrative permissions.

The helper permits only the exact Kubernetes namespace/service-account subject. For local Google storage it grants that principal `roles/storage.objectCreator` and `roles/storage.objectViewer` directly on the course bucket. When a Google service account is configured, it grants the subject `roles/iam.workloadIdentityUser` on that account and grants the bucket roles to the account instead. Existing permissions are preserved. Originals are immutable; the app does not need object deletion. Administrative test cleanup uses the host's Google identity separately.

`make workload-identity-check` verifies token exchange and a real bucket read/list request, including impersonation when configured. Deployment performs the same check before pausing a running app. Repeat identity setup after a deliberate cluster signing-key rotation to refresh Google's public JWKS; recreate no long-lived credentials. A different cluster, namespace, or account needs a matching provider and trust condition. The helper rejects an existing provider with different trust settings.

On GKE, leave `workloadIdentity.enabled: false` to use GKE's metadata server and configure the Kubernetes-to-Google service-account link through `serviceAccount.annotations.iam.gke.io/gcp-service-account`. You can reuse an existing Kubernetes account with `serviceAccount.create: false` and `serviceAccount.name`.

## Course files in MinIO or another S3-compatible service

For an existing service, set the endpoint and reference an existing credential Secret:

```yaml
storage:
  provider: s3
  s3:
    endpoint: https://objects.internal.example
    bucket: your-existing-private-bucket
    prefix: courses/
    region: us-east-1
    existingSecret: course-storage
    accessKeyKey: accessKey
    secretKeyKey: secretKey
```

The endpoint must be an HTTP(S) origin without a path or embedded credentials. The app uses path-style S3 requests and conditional writes that prevent overwriting originals. Create the bucket and grant read/write object access before deployment. Existing S3 and GCS buckets are not created by this chart.

To use the MinIO dependency instead:

```yaml
storage:
  provider: s3
  s3:
    existingSecret: course-minio
    accessKeyKey: rootUser
    secretKeyKey: rootPassword
minio:
  enabled: true
  existingSecret: course-minio
  persistence:
    storageClass: microk8s-hostpath
    size: 5Gi
```

Provision `course-minio` with `rootUser` and `rootPassword` keys in the release namespace. The local test harness loads credentials from SOPS-encrypted settings and creates this Secret using its own test name. With an empty S3 endpoint, the chart selects the bundled service. MinIO runs as a single instance, creates the private `courses` bucket, and stores objects on a PVC. If changing the bucket, update both `storage.s3.bucket` and `minio.buckets`. A bucket prefix does not provide an access boundary.

The standalone MinIO PVC is retained on Helm uninstall by default. Reusing it requires deliberate reattachment through `minio.persistence.existingClaim`; retention does not replace backups. Deleting the namespace or VM can delete storage. Choose an appropriate storage class when running outside MicroK8s.

The official MinIO Helm dependency is pinned to 5.4.0, with explicit server and client image releases in `values.yaml`. [MinIO's community repository](https://github.com/minio/minio) was archived in April 2026; these pins provide reproducible local testing, not ongoing upstream maintenance.

## Test with MicroK8s

See the [local MicroK8s commands](../../README.md#local-microk8s-checks). The harness creates an isolated test namespace, a persistent PostgreSQL fixture outside the app chart, and credentials supplied by SOPS-encrypted settings. It imports the local image directly into MicroK8s and deploys this chart with `values-microk8s.yaml`.

`make microk8s-test` checks uploads, exercise generation/publication, answer retries, and file/progress persistence across pod restarts. `make microk8s-forward` exposes the app at http://localhost:8080 and, in MinIO mode, its console at http://localhost:9001. `make up` uses this MicroK8s setup and stops older Compose services.

## Exercise API credentials

Set `generation.existingSecret` to an existing Secret containing `EXERCISE_API_URL` and optional `EXERCISE_API_KEY`, `EXERCISE_API_MODEL`, `EXERCISE_API_EFFORT`, and `EXERCISE_API_EFFORT_FORMAT` keys. The URL is a complete HTTPS endpoint using the chat-completions format. Effort is optional; an absent or empty value uses the provider default. Effort format defaults to `reasoning_effort`; `reasoning` selects the nested field required by some compatible gateways. These values are injected only into the backend; the chart does not create or embed credentials. Restart the Deployment after rotating the Secret. The local MicroK8s helper decrypts these variables from `config/secrets.local.enc.yaml` in memory, synchronizes them to a Secret, and restarts the app when they change. The local helper always requires `APP_ENV=local`. Production settings are held separately in `config/secrets.prod.enc.yaml`; select them with `APP_ENV=prod` in the deployment runner.

AI generation performs two sequential provider requests: generation and Romanian language review. Allow at least 130 seconds for request timeouts in any ingress controller or reverse proxy. The application gives the two stages a combined 120-second deadline.

## Access

Database parameters are consumed by the Go server. Storage settings select the course-file location and its credentials, Google identity, or volume mount. Helm alone does not copy documents. The local deployment helper copies and verifies database-referenced originals when switching between Google storage and MinIO, retaining the source copies.

Published questions are public. Uploads, original downloads, generation, and question management require an authenticated admin. Admins share the course library and can manage user roles. Each signed-in user has separate history; anonymous answers are never recorded.

Configure GitHub OAuth through a Secret in the release namespace:

```yaml
auth:
  existingSecret: github-auth
  baseURL: https://learn.example.com
appMode: public
```

The Secret contains `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET`. Register `https://learn.example.com/auth/github/callback` as the OAuth callback. An optional `AUTH_LEGACY_GITHUB_ID` key assigns pre-account materials/history only after the specified numeric GitHub identity signs in. The optional `APP_BASE_URL` Secret key overrides the chart origin; keep it consistent with `auth.baseURL` and the registered callback. Credentials are never rendered into a ConfigMap or Helm values.

Public mode requires configured GitHub authentication and HTTPS. Local mode permits HTTP on localhost and keeps management locked if OAuth credentials are absent. The local helper synchronizes credentials from SOPS-encrypted settings; other deployments must restart the app when rotating environment-based Secrets.

### NGINX ingress and automatic TLS

The `values-ingress-nginx.yaml` overlay selects the `nginx` IngressClass and enables `kubernetes.io/tls-acme: "true"`. The existing cert-manager installation must configure a default issuer for ingress-shim, for example `--default-issuer-name=letsencrypt-prod`, `--default-issuer-kind=ClusterIssuer`, and `--default-issuer-group=cert-manager.io`. Alternatively, set `ingress.annotations.cert-manager.io/cluster-issuer` to an existing issuer. The chart reuses the cluster's controller and issuer. See [cert-manager's ingress annotations](https://cert-manager.io/docs/usage/ingress/).

`ingress.host` defaults to the hostname in `auth.baseURL`. Each `ingress.tls` entry requires `secretName`; omitted `hosts` use that same hostname. The overlay requests the certificate in `romana-learning-tls`. cert-manager creates and renews the certificate and its Secret in the app namespace. Override that Secret name for separate apps in the same namespace. The chart rejects mismatched OAuth/ingress hosts and TLS settings that omit the app hostname.

The overlay enables HTTP-to-HTTPS redirects, a 25 MiB request-body allowance for the app's 20 MiB file limit, and 180-second upstream read/send timeouts for generation and Romanian review. See [NGINX ingress annotations](https://kubernetes.github.io/ingress-nginx/user-guide/nginx-configuration/annotations/).

Render production configuration directly from the selected SOPS settings:

```sh
make APP_ENV=prod chart
ROMANA_ENV=prod python3 scripts/helm_chart.py lint
# Optional image, node placement, Secret-reference, or ingress annotation overrides:
ROMANA_ENV=prod python3 scripts/helm_chart.py template \
  --release romana-learning --namespace romana-learning -f /path/to/private-values.yaml
```

The production renderer enables the NGINX overlay, reads the origin from `APP_BASE_URL`, maps the production database endpoint/name/TLS mode, and supplies the Google service-account annotation from `GCS_SERVICE_ACCOUNT`. Identifiers appear in rendered Kubernetes manifests; credentials and the exercise API endpoint remain in existing Secret references. SOPS-derived settings take precedence over additional values files. Rendering and linting do not install the app or create runtime Secrets. Local rendering keeps ingress disabled by default.

The default production release and namespace are both `romana-learning`. The production image is published to `elikhis/romana-learning` and pinned by digest in `values-prod.yaml`. Build for the target node architecture with `docker buildx build --platform linux/amd64 --tag elikhis/romana-learning:TAG --push .`, then update the production image tag/digest. The frontend build and Go compiler run on the builder's native architecture; Go cross-compiles the server for the target architecture.

Before deployment, provision the referenced production Secrets (`github-auth`, `romanian-database`, `romanian-generation`, and `google-storage`), supply the production image, and use the intended namespace. For the existing issuer's HTTP-01 solver, the public hostname must route port 80 and `/.well-known/acme-challenge/` to NGINX. If a CDN or access proxy fronts the hostname, configure its origin and challenge routing as well; its edge certificate does not establish that the Kubernetes certificate is ready.

After deployment, verify the Ingress backend has ready endpoints, the generated Certificate reports `Ready=True`, and the public `/readyz` endpoint succeeds over HTTPS. GitHub's registered callback must use the same `APP_BASE_URL` followed by `/auth/github/callback`.

The default Service is ClusterIP. For a private test deployment:

```sh
kubectl -n romanian port-forward svc/romanian-romanian 8080:8080
```

No cluster or Google Cloud changes are performed by linting/rendering. Before live deployment, verify the exact node/PVC or bucket, dedicated database and role, network route, image architecture, and required Secrets.

## SOPS integration

The chart uses existing Kubernetes Secrets. Secret values and decryption keys are never Helm values, templates, release metadata, or image inputs. The selected Google service-account address is identity configuration and appears in the Kubernetes annotation and Helm release metadata. The local deployment helper decrypts the encrypted settings with SOPS and sends Secret data to Kubernetes through stdin, without writing a plaintext deployment file. See the [secret commands and key setup](../../README.md#encrypted-secrets).

For a production cluster, load `config/secrets.prod.enc.yaml` with `make APP_ENV=prod secrets-exec` in a SOPS-capable deployment runner and provision the referenced Secrets in the production namespace before installing the chart. Map production `PGHOST`, `PGPORT`, `PGDATABASE`, and `PGSSLMODE` to `database.host`, `port`, `name`, and `sslMode`; map `PGUSER` / `PGPASSWORD` to the username/password keys of the referenced production database Secret. Use a production release and namespace; the local MicroK8s helper never installs production settings. Authorize that runner for the Cloud KMS key through Google Application Default Credentials or workload identity. Keep Google credential files outside Git. `ROMANA_SECRETS_FILE` overrides the source within the selected environment; the source must match that environment's schema. The local helper rejects production selection before any cluster changes. Runtime Secrets still require appropriate Kubernetes RBAC and encryption at rest.
