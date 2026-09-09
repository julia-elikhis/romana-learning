# Existing Google Cloud infrastructure

This chart installs the app into an existing Kubernetes cluster. It uses an existing PostgreSQL instance and configurable course storage: an existing PVC, Google Cloud Storage, an S3-compatible service, or the optional MinIO dependency. Enabling MinIO provisions its storage and configured buckets. The chart creates no VM, node pool, Postgres server, logical database, or IAM binding.

If the existing node is a standalone Compute Engine VM without Kubernetes, use Docker Compose/container deployment there instead. Helm requires Kubernetes; it does not install Kubernetes onto a VM.

## Choose a profile

- `values-gcp-node.example.yaml`: existing node, existing course-files PVC, and external Postgres.
- `values-gcp-gcs.example.yaml`: existing bucket with Google identity, and external Postgres. Set node placement if needed.
- `values-microk8s.yaml`: bundled MinIO with MicroK8s hostpath storage. The local test harness supplies PostgreSQL and Secrets.
- `values.yaml`: all available options and a minimal profile using a connection URL Secret.

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
    project: your-project
    bucket: your-existing-private-bucket
    prefix: romanian/courses/
serviceAccount:
  create: true
  annotations:
    iam.gke.io/gcp-service-account: romanian@your-project.iam.gserviceaccount.com
```

On GKE, configure Workload Identity on the existing cluster/node pool and authorize the Kubernetes principal or linked Google service account for the intended bucket. The annotation alone does not grant permission. IAM setup is external to Helm. GKE's metadata server supplies credentials without a mounted JSON key. A bucket prefix is organization, not an independent access boundary; enforce permissions through IAM and the application. See [Workload Identity](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/workload-identity) and [Storage authentication](https://docs.cloud.google.com/storage/docs/authentication).

For a non-GKE cluster, use its configured Google federation/ADC setup or reference an existing credential-file Secret through `storage.gcs.credentialsSecret` and `credentialsKey`. The chart mounts it read-only and sets `GOOGLE_APPLICATION_CREDENTIALS`. You can reuse an existing Kubernetes service account with `serviceAccount.create: false` and `serviceAccount.name`.

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

Provision `course-minio` with `rootUser` and `rootPassword` keys in the release namespace. The local test harness generates this Secret automatically using its own test name. With an empty S3 endpoint, the chart selects the bundled service. MinIO runs as a single instance, creates the private `courses` bucket, and stores objects on a PVC. If changing the bucket, update both `storage.s3.bucket` and `minio.buckets`. A bucket prefix does not provide an access boundary.

The standalone MinIO PVC is retained on Helm uninstall by default. Reusing it requires deliberate reattachment through `minio.persistence.existingClaim`; retention does not replace backups. Deleting the namespace or VM can delete storage. Choose an appropriate storage class when running outside MicroK8s.

The official MinIO Helm dependency is pinned to 5.4.0, with explicit server and client image releases in `values.yaml`. [MinIO's community repository](https://github.com/minio/minio) was archived in April 2026; these pins provide reproducible local testing, not ongoing upstream maintenance.

## Test with MicroK8s

See the [local MicroK8s commands](../../README.md#local-microk8s-checks). The harness creates an isolated test namespace, a persistent PostgreSQL fixture outside the app chart, and generated Secret credentials. It imports the local image directly into MicroK8s and deploys this chart with `values-microk8s.yaml`.

`make microk8s-test` checks uploads, exercise generation/publication, answer retries, and file/progress persistence across pod restarts. `make microk8s-forward` exposes the app at http://localhost:8080 and the MinIO console at http://localhost:9001. `make up` uses this MicroK8s setup and stops older Compose services.

## Exercise API credentials

Set `generation.existingSecret` to an existing Secret containing `EXERCISE_API_URL` and optional `EXERCISE_API_KEY`, `EXERCISE_API_MODEL`, `EXERCISE_API_EFFORT`, and `EXERCISE_API_EFFORT_FORMAT` keys. The URL is a complete HTTPS endpoint using the chat-completions format. Effort is optional; an absent or empty value uses the provider default. Effort format defaults to `reasoning_effort`; `reasoning` selects the nested field required by some compatible gateways. These values are injected only into the backend; the chart does not create or embed credentials. Restart the Deployment after rotating the Secret. The local MicroK8s helper synchronizes these variables from `.env` to a Secret and restarts the app when they change.

AI generation performs two sequential provider requests: generation and Romanian language review. Allow at least 130 seconds for request timeouts in any ingress controller or reverse proxy. The application gives the two stages a combined 120-second deadline.

## Access

Database parameters are consumed by the Go server. Storage settings select the course-file location and its credentials, Google identity, or volume mount. Configuring storage does not automatically import documents.

Published questions are public. Uploads, original downloads, generation, and question management require an authenticated owner. Each signed-in user has separate history; anonymous answers are never recorded.

Configure GitHub OAuth through a Secret in the release namespace:

```yaml
auth:
  existingSecret: github-auth
  baseURL: https://learn.example.com
appMode: public
```

The Secret contains `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET`. Register `https://learn.example.com/auth/github/callback` as the OAuth callback. An optional `AUTH_LEGACY_GITHUB_ID` key assigns pre-account materials/history only after the specified numeric GitHub identity signs in. The optional `APP_BASE_URL` Secret key overrides the chart origin; keep it consistent with `auth.baseURL` and the registered callback. Credentials are never rendered into a ConfigMap or Helm values.

Public mode requires configured GitHub authentication and HTTPS. Enable Ingress and configure its host/TLS for the same origin when ready. Local mode permits HTTP on localhost and keeps management locked if OAuth credentials are absent. The local helper synchronizes credentials from `.env`; other deployments must restart the app when rotating environment-based Secrets.

The default Service is ClusterIP. For a private test deployment:

```sh
kubectl -n romanian port-forward svc/romanian-romanian 8080:8080
```

No cluster or Google Cloud changes are performed by linting/rendering. Before live deployment, verify the exact node/PVC or bucket, dedicated database and role, network route, image architecture, and required Secrets.
