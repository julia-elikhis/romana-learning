# Existing Google Cloud infrastructure

This chart installs the app into an existing Kubernetes cluster. It uses your existing PostgreSQL instance and either an existing course-files PVC or a Cloud Storage bucket. It creates no VM, node pool, Postgres server, database, bucket, or IAM binding.

If the existing node is a standalone Compute Engine VM without Kubernetes, use Docker Compose/container deployment there instead. Helm requires Kubernetes; it does not install Kubernetes onto a VM.

## Choose a profile

- `values-gcp-node.example.yaml`: existing node, existing course-files PVC, and external Postgres.
- `values-gcp-gcs.example.yaml`: existing bucket with Google identity, and external Postgres. Set node placement if needed.
- `values.yaml`: all available options and a minimal profile using a connection URL Secret.

Copy a profile to a private deployment values file and replace its placeholders. Use the actual image registry/tag built from this source; placeholder images are not published. An ARM build from this Mac needs a matching node architecture; build/publish an AMD64 image or a multi-platform image for AMD64 nodes.

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

Alternatively, `database.mode: url` reads the complete URL from `database.existingSecret` / `database.urlKey`. The URL must select the dedicated database and its TLS settings. This preserves the initial setup. Credentials from parameters mode are not injected in URL mode. Rotating an environment-based Secret requires restarting the Deployment; updating the ConfigMap automatically changes the pod-template checksum.

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

The app runs as UID/GID 10001 with fsGroup 10001. Ensure the volume supports these permissions; this is especially important for pre-existing/local volumes. Single-writer volumes use one replica and a Recreate rollout. The app filesystem remains read-only except the course mount. See [Kubernetes persistent volume behavior](https://kubernetes.io/docs/concepts/storage/persistent-volumes/).

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

## Current scope

Database parameters are consumed by the Go server. File volumes, bucket settings, and Google identity references are deployment plumbing for the planned course library; **the upload APIs and GCS application adapter are not implemented yet**. Configuring a bucket does not upload existing notes or make the demo use them.

The app still has one local learner. The Service is ClusterIP and the schema only permits local mode; Ingress remains blocked until authentication exists. For a private test deployment:

```sh
kubectl -n romanian port-forward svc/romanian-romanian 8080:8080
```

No cluster or Google Cloud changes are performed by linting/rendering. Before live deployment, verify the exact node/PVC or bucket, dedicated database and role, network route, image architecture, and required Secrets.
