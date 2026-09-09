package charttest

import (
	"bytes"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func render(t *testing.T, overrides ...string) (map[string]map[string]any, string, error) {
	t.Helper()
	if _, err := exec.LookPath("helm"); err != nil {
		t.Skip("helm is required for chart tests")
	}
	chart, err := filepath.Abs("../../charts/romanian")
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"template", "check", chart}, overrides...)
	out, err := exec.Command("helm", args...).CombinedOutput()
	if err != nil {
		return nil, string(out), err
	}
	docs := map[string]map[string]any{}
	decoder := yaml.NewDecoder(bytes.NewReader(out))
	for {
		var doc map[string]any
		err := decoder.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if kind, ok := doc["kind"].(string); ok {
			docs[kind] = doc
		}
	}
	return docs, string(out), nil
}

func pod(docs map[string]map[string]any) map[string]any {
	return docs["Deployment"]["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)
}

func TestDefaultChartUsesExternalSecretOnly(t *testing.T) {
	docs, out, err := render(t)
	if err != nil {
		t.Fatal(out)
	}
	for _, kind := range []string{"Secret", "PersistentVolume", "PersistentVolumeClaim", "StatefulSet", "Ingress"} {
		if docs[kind] != nil {
			t.Fatalf("default unexpectedly provisions %s", kind)
		}
	}
	if pod(docs)["serviceAccountName"] != "check-romanian" {
		t.Fatal("service account not attached")
	}
	if docs["Service"]["spec"].(map[string]any)["type"] != "ClusterIP" {
		t.Fatal("unexpected public service")
	}
	if !strings.Contains(out, "key: DATABASE_URL") {
		t.Fatal("URL compatibility missing")
	}
}

func TestExistingNodeDiskAndDedicatedDatabase(t *testing.T) {
	docs, out, err := render(t, "-f", "../../charts/romanian/values-gcp-node.example.yaml")
	if err != nil {
		t.Fatal(out)
	}
	if docs["PersistentVolumeClaim"] != nil {
		t.Fatal("must reuse existing claim")
	}
	data := docs["ConfigMap"]["data"].(map[string]any)
	if data["PGDATABASE"] != "romanian" || data["PGSSLMODE"] != "verify-full" {
		t.Fatal("database selection/TLS missing")
	}
	if data["PGPASSWORD"] != nil || data["PGUSER"] != nil {
		t.Fatal("credentials belong in Secret references")
	}
	if pod(docs)["nodeSelector"].(map[string]any)["kubernetes.io/hostname"] != "YOUR_EXISTING_NODE" {
		t.Fatal("node placement lost")
	}
	if !strings.Contains(out, "claimName: romanian-course-files") || !strings.Contains(out, "secretName: romanian-postgres-ca") {
		t.Fatal("disk or CA wiring missing")
	}
}

func TestClaimCreationAndGCSProfiles(t *testing.T) {
	docs, out, err := render(t, "--set", "storage.provider=filesystem,storage.filesystem.persistence.create=true", "--set-string", "storage.filesystem.persistence.storageClass=")
	if err != nil {
		t.Fatal(out)
	}
	pvc := docs["PersistentVolumeClaim"]
	if pvc["spec"].(map[string]any)["storageClassName"] != "" {
		t.Fatal("explicit empty storageClass must be preserved")
	}
	if pvc["metadata"].(map[string]any)["annotations"].(map[string]any)["helm.sh/resource-policy"] != "keep" {
		t.Fatal("retention missing")
	}

	docs, out, err = render(t, "-f", "../../charts/romanian/values-gcp-gcs.example.yaml")
	if err != nil {
		t.Fatal(out)
	}
	if docs["PersistentVolumeClaim"] != nil || len(pod(docs)["volumes"].([]any)) != 1 {
		t.Fatal("identity-based GCS should mount only temporary extraction space")
	}
	if docs["ConfigMap"]["data"].(map[string]any)["COURSE_STORAGE_BUCKET"] != "YOUR_EXISTING_BUCKET" {
		t.Fatal("bucket config lost")
	}
	if !strings.Contains(out, "iam.gke.io/gcp-service-account") {
		t.Fatal("identity annotation missing")
	}

	docs, out, err = render(t, "--set", "storage.provider=gcs,storage.gcs.bucket=private-course-bucket,storage.gcs.credentialsSecret=google-key,serviceAccount.create=false,serviceAccount.name=existing-identity")
	if err != nil {
		t.Fatal(out)
	}
	if docs["ServiceAccount"] != nil || pod(docs)["serviceAccountName"] != "existing-identity" {
		t.Fatal("existing service account not reused")
	}
	if !strings.Contains(out, "secretName: google-key") || !strings.Contains(out, "GOOGLE_APPLICATION_CREDENTIALS") {
		t.Fatal("credential file wiring missing")
	}
}

func TestInvalidChartSettingsFail(t *testing.T) {
	tests := map[string]string{
		"missing database host":     "database.mode=parameters",
		"maintenance database":      "database.mode=parameters,database.host=db.internal,database.name=postgres",
		"ephemeral files":           "storage.provider=filesystem",
		"conflicting claims":        "storage.provider=filesystem,storage.filesystem.persistence.create=true,storage.filesystem.persistence.existingClaim=already-exists",
		"missing bucket":            "storage.provider=gcs,storage.gcs.settingsSecret=",
		"missing identity provider": "storage.gcs.workloadIdentity.enabled=true",
		"conflicting identity":      "storage.gcs.workloadIdentity.enabled=true,storage.gcs.workloadIdentity.provider=projects/123456/locations/global/workloadIdentityPools/test-pool/providers/kubernetes,storage.gcs.credentialsSecret=user-credentials",
		"invalid identity provider": "storage.gcs.workloadIdentity.provider=https://untrusted.example",
		"invalid provider":          "storage.provider=unknown",
		"invalid TLS":               "database.sslMode=bogus",
		"invalid database port":     "database.port=70000",
		"missing reused account":    "serviceAccount.create=false",
		"invalid Google account":    "serviceAccount.gcpServiceAccount=invalid",
		"unmanaged Google link":     "serviceAccount.create=false,serviceAccount.name=external,serviceAccount.gcpServiceAccount=learning@test-project.iam.gserviceaccount.com",
		"multiple writers":          "storage.provider=filesystem,storage.filesystem.persistence.create=true,replicaCount=2",
		"overlapping disk rollout":  "storage.provider=filesystem,storage.filesystem.persistence.create=true,strategy.type=RollingUpdate",
			"ingress without login":    "ingress.enabled=true",
		"public without OAuth":      "appMode=public,auth.baseURL=https://learn.example.com",
		"public over HTTP":          "appMode=public,auth.existingSecret=github-auth",
	}
	for name, setting := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := render(t, "--set", setting); err == nil {
				t.Fatal("invalid settings rendered successfully")
			}
		})
	}
}

func TestServiceAccountWorkloadIdentityLink(t *testing.T) {
	account := "learning@test-project.iam.gserviceaccount.com"
	docs, out, err := render(t, "--set-string", "serviceAccount.gcpServiceAccount="+account,
		"--set-string", `serviceAccount.annotations.iam\.gke\.io/gcp-service-account=`+account,
		"--set-string", "serviceAccount.name=learning-identity", "--set-string", "serviceAccount.annotations.owner=learning-team")
	if err != nil {
		t.Fatal(out)
	}
	metadata := docs["ServiceAccount"]["metadata"].(map[string]any)
	annotations := metadata["annotations"].(map[string]any)
	if annotations["iam.gke.io/gcp-service-account"] != account || annotations["owner"] != "learning-team" {
		t.Fatal("Google identity annotation or existing annotation missing")
	}
	if metadata["name"] != "learning-identity" || pod(docs)["serviceAccountName"] != metadata["name"] {
		t.Fatal("app must use the annotated Kubernetes service account")
	}
	if pod(docs)["automountServiceAccountToken"] != false {
		t.Fatal("linking Google identity must not enable automatic Kubernetes API token mounting")
	}
	_, _, err = render(t, "--set-string", "serviceAccount.gcpServiceAccount="+account,
		"--set-string", `serviceAccount.annotations.iam\.gke\.io/gcp-service-account=different@test-project.iam.gserviceaccount.com`)
	if err == nil {
		t.Fatal("conflicting identity assignments must fail instead of selecting one silently")
	}
}

func TestGitHubCredentialsAndPublicIngress(t *testing.T) {
	docs, out, err := render(t, "--set", "appMode=public,auth.existingSecret=github-auth,auth.baseURL=https://learn.example.com,ingress.enabled=true")
	if err != nil {
		t.Fatal(out)
	}
	data := docs["ConfigMap"]["data"].(map[string]any)
	if data["APP_BASE_URL"] != "https://learn.example.com" || data["APP_MODE"] != "public" || docs["Ingress"] == nil {
		t.Fatal("public authentication configuration is missing")
	}
	if docs["Secret"] != nil || data["GITHUB_CLIENT_SECRET"] != nil || data["GITHUB_CLIENT_ID"] != nil || data["AUTH_LEGACY_GITHUB_ID"] != nil {
		t.Fatal("private authentication settings must use an existing Secret")
	}
	container := pod(docs)["containers"].([]any)[0].(map[string]any)
	found := map[string]bool{}
	for _, value := range container["env"].([]any) {
		entry := value.(map[string]any)
		name := entry["name"].(string)
		if name != "GITHUB_CLIENT_ID" && name != "GITHUB_CLIENT_SECRET" && name != "AUTH_LEGACY_GITHUB_ID" {
			continue
		}
		ref := entry["valueFrom"].(map[string]any)["secretKeyRef"].(map[string]any)
		if ref["name"] != "github-auth" || ref["key"] != name {
			t.Fatal("OAuth setting must reference the configured Secret")
		}
		if name != "AUTH_LEGACY_GITHUB_ID" && ref["optional"] == true {
			t.Fatal("OAuth credentials must be required when a Secret is configured")
		}
		found[name] = true
	}
	if len(found) != 3 {
		t.Fatal("OAuth Secret references missing")
	}
}

func TestNGINXIngressUsesOAuthHostAndACMETLS(t *testing.T) {
	docs, out, err := render(t, "-f", "../../charts/romanian/values-ingress-nginx.yaml",
		"--set-string", "auth.baseURL=https://learn.example.com", "--set", "service.port=8090")
	if err != nil {
		t.Fatal(out)
	}
	ingress := docs["Ingress"]
	annotations := ingress["metadata"].(map[string]any)["annotations"].(map[string]any)
	if annotations["kubernetes.io/tls-acme"] != "true" || annotations["nginx.ingress.kubernetes.io/ssl-redirect"] != "true" {
		t.Fatal("automatic certificates and HTTPS redirect must be enabled")
	}
	if annotations["nginx.ingress.kubernetes.io/proxy-body-size"] != "25m" ||
		annotations["nginx.ingress.kubernetes.io/proxy-read-timeout"] != "180" ||
		annotations["nginx.ingress.kubernetes.io/proxy-send-timeout"] != "180" {
		t.Fatal("ingress must allow course uploads and the two-stage generation deadline")
	}
	spec := ingress["spec"].(map[string]any)
	rule := spec["rules"].([]any)[0].(map[string]any)
	tls := spec["tls"].([]any)[0].(map[string]any)
	if spec["ingressClassName"] != "nginx" || rule["host"] != "learn.example.com" ||
		tls["hosts"].([]any)[0] != rule["host"] || tls["secretName"] != "romana-learning-tls" {
		t.Fatal("ingress routing and certificate must use the OAuth hostname")
	}
	path := rule["http"].(map[string]any)["paths"].([]any)[0].(map[string]any)
	backend := path["backend"].(map[string]any)["service"].(map[string]any)
	service := docs["Service"]
	port := service["spec"].(map[string]any)["ports"].([]any)[0].(map[string]any)
	if path["path"] != "/" || path["pathType"] != "Prefix" ||
		backend["name"] != service["metadata"].(map[string]any)["name"] ||
		backend["port"].(map[string]any)["name"] != port["name"] || port["port"] != 8090 {
		t.Fatal("ingress must route all paths to the application's configured Service port")
	}
	if docs["Certificate"] != nil || docs["ClusterIssuer"] != nil || docs["Secret"] != nil {
		t.Fatal("existing cert-manager must manage certificates and issuer credentials")
	}
}

func TestIngressRejectsBrokenOAuthAndTLSRouting(t *testing.T) {
	settings := map[string]string{
		"different OAuth host": "ingress.host=other.example.com",
		"different TLS host":   "ingress.tls[0].hosts[0]=other.example.com",
		"missing TLS secret":   "ingress.tls[0].secretName=",
		"origin with path":     "auth.baseURL=https://learn.example.com/practice",
		"origin with query":    "auth.baseURL=https://learn.example.com?callback=bad",
		"origin with user":     "auth.baseURL=https://user@learn.example.com",
		"origin with port":     "auth.baseURL=https://learn.example.com:8443",
	}
	for name, setting := range settings {
		t.Run(name, func(t *testing.T) {
			_, _, err := render(t, "-f", "../../charts/romanian/values-ingress-nginx.yaml",
				"--set-string", "auth.baseURL=https://learn.example.com", "--set-string", setting)
			if err == nil {
				t.Fatal("broken ingress settings rendered successfully")
			}
		})
	}
	_, _, err := render(t, "--set", "appMode=public,auth.existingSecret=github-auth,auth.baseURL=https://learn.example.com,ingress.enabled=true",
		"--set-string", `ingress.annotations.kubernetes\.io/tls-acme=true`)
	if err == nil {
		t.Fatal("ACME without a TLS certificate Secret must fail")
	}
}

func TestGenerationCredentialsOnlyUseExistingSecret(t *testing.T) {
	docs, out, err := render(t, "--set", "generation.existingSecret=private-generator")
	if err != nil {
		t.Fatal(out)
	}
	if docs["Secret"] != nil {
		t.Fatal("chart must not create credentials")
	}
	data := docs["ConfigMap"]["data"].(map[string]any)
	if data["EXERCISE_API_URL"] != nil || data["EXERCISE_API_KEY"] != nil || data["EXERCISE_API_EFFORT"] != nil {
		t.Fatal("secrets leaked into configmap")
	}
	if !strings.Contains(out, "key: EXERCISE_API_URL") || !strings.Contains(out, "name: private-generator") {
		t.Fatal("generation secret not wired")
	}
	container := pod(docs)["containers"].([]any)[0].(map[string]any)
	foundEffort := false
	foundFormat := false
	for _, item := range container["env"].([]any) {
		entry := item.(map[string]any)
		if entry["name"] == "EXERCISE_API_EFFORT" {
			ref := entry["valueFrom"].(map[string]any)["secretKeyRef"].(map[string]any)
			if ref["name"] != "private-generator" || ref["key"] != "EXERCISE_API_EFFORT" || ref["optional"] != true {
				t.Fatal("Effort must use an optional key from the configured Secret")
			}
			foundEffort = true
		}
		if entry["name"] == "EXERCISE_API_EFFORT_FORMAT" {
			ref := entry["valueFrom"].(map[string]any)["secretKeyRef"].(map[string]any)
			if ref["name"] != "private-generator" || ref["key"] != "EXERCISE_API_EFFORT_FORMAT" || ref["optional"] != true {
				t.Fatal("Effort format must use an optional Secret key")
			}
			foundFormat = true
		}
	}
	if !foundEffort || !foundFormat {
		t.Fatal("Effort setting is missing from the backend environment")
	}
	if !strings.Contains(out, "mountPath: /tmp") {
		t.Fatal("PDF extraction needs writable temporary space")
	}
}

func TestMicroK8sProfileUsesBundledMinIO(t *testing.T) {
	docs, out, err := render(t, "-f", "../../charts/romanian/values-microk8s.yaml", "-f", "../../charts/romanian/values-minio.yaml")
	if err != nil {
		t.Fatal(out)
	}
	data := docs["ConfigMap"]["data"].(map[string]any)
	if data["COURSE_STORAGE_PROVIDER"] != "s3" || data["COURSE_S3_ENDPOINT"] != "http://check-minio:9000" || data["PGDATABASE"] != "romanian_test" {
		t.Fatal("MicroK8s storage/database wiring is incorrect")
	}
	if data["COURSE_S3_ACCESS_KEY"] != nil || data["COURSE_S3_SECRET_KEY"] != nil {
		t.Fatal("S3 credentials leaked into ConfigMap")
	}
	for _, expected := range []string{"chart: minio-5.4.0", "name: check-minio", "name: test-minio", "key: rootPassword", "readOnlyRootFilesystem: true"} {
		if !strings.Contains(out, expected) {
			t.Fatalf("Missing %s", expected)
		}
	}
	pvc := docs["PersistentVolumeClaim"]
	if pvc["spec"].(map[string]any)["storageClassName"] != "microk8s-hostpath" ||
		pvc["metadata"].(map[string]any)["annotations"].(map[string]any)["helm.sh/resource-policy"] != "keep" {
		t.Fatal("MinIO must use persistent MicroK8s storage retained on Helm uninstall")
	}
	if strings.Contains(out, "kind: Secret") || strings.Contains(out, "console123") {
		t.Fatal("Bundled MinIO must use existing credentials only")
	}
	if strings.Contains(out, "name: courses\n              mountPath: /data/courses") {
		t.Fatal("App must use object storage instead of a course folder")
	}
	for name, setting := range map[string]string{"missing credentials": "storage.provider=s3,storage.s3.endpoint=http://storage:9000", "missing endpoint": "storage.provider=s3,storage.s3.existingSecret=s3-credentials", "minio wrong provider": "minio.enabled=true,minio.existingSecret=credentials", "minio ephemeral": "minio.enabled=true,minio.existingSecret=credentials,minio.persistence.enabled=false,storage.provider=s3,storage.s3.existingSecret=credentials"} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := render(t, "--set", setting); err == nil {
				t.Fatal("Invalid object storage configuration rendered")
			}
		})
	}
}

func TestMicroK8sUsesProjectedWorkloadIdentity(t *testing.T) {
	provider := "projects/123456/locations/global/workloadIdentityPools/learning/providers/kubernetes"
	docs, out, err := render(t, "-f", "../../charts/romanian/values-microk8s.yaml", "--set-string", "storage.gcs.workloadIdentity.provider="+provider)
	if err != nil {
		t.Fatal(out)
	}
	data := docs["ConfigMap"]["data"].(map[string]any)
	if data["COURSE_STORAGE_PROVIDER"] != "gcs" || data["COURSE_STORAGE_BUCKET"] != nil || data["GOOGLE_CLOUD_PROJECT"] != nil {
		t.Fatal("Google storage must be default with deployment identifiers in Secret references")
	}
	if docs["Secret"] != nil || docs["PersistentVolumeClaim"] != nil || strings.Contains(out, "chart: minio") {
		t.Fatal("GCS profile must not render credentials or provision MinIO")
	}
	p := pod(docs)
	if p["automountServiceAccountToken"] != false || p["serviceAccountName"] != "check-romanian" {
		t.Fatal("unrestricted Kubernetes API token must not be automounted")
	}
	volumes := map[string]map[string]any{}
	for _, v := range p["volumes"].([]any) {
		volume := v.(map[string]any)
		volumes[volume["name"].(string)] = volume
	}
	projection := volumes["workload-token"]["projected"].(map[string]any)
	token := projection["sources"].([]any)[0].(map[string]any)["serviceAccountToken"].(map[string]any)
	if token["audience"] != "https://iam.googleapis.com/"+provider || token["expirationSeconds"] != 3600 || token["path"] != "token" {
		t.Fatal("token must expire and be scoped to the configured Google provider")
	}
	secret := volumes["workload-identity"]["secret"].(map[string]any)
	if secret["secretName"] != "google-workload-identity" {
		t.Fatal("federation configuration must reference its external Secret")
	}
	encoded, _ := json.Marshal(p)
	if !bytes.Contains(encoded, []byte("/var/run/secrets/google")) || !bytes.Contains(encoded, []byte("/etc/workload-identity")) ||
		data["GOOGLE_APPLICATION_CREDENTIALS"] != "/etc/workload-identity/credentials.json" {
		t.Fatal("projected token and ADC configuration must be accessible to the SDK")
	}
}
