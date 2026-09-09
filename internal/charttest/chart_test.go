package charttest

import (
	"bytes"
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
		"missing database host":    "database.mode=parameters",
		"maintenance database":     "database.mode=parameters,database.host=db.internal,database.name=postgres",
		"ephemeral files":          "storage.provider=filesystem",
		"conflicting claims":       "storage.provider=filesystem,storage.filesystem.persistence.create=true,storage.filesystem.persistence.existingClaim=already-exists",
		"missing bucket":           "storage.provider=gcs",
		"invalid provider":         "storage.provider=unknown",
		"invalid TLS":              "database.sslMode=bogus",
		"invalid database port":    "database.port=70000",
		"missing reused account":   "serviceAccount.create=false",
		"multiple writers":         "storage.provider=filesystem,storage.filesystem.persistence.create=true,replicaCount=2",
		"overlapping disk rollout": "storage.provider=filesystem,storage.filesystem.persistence.create=true,strategy.type=RollingUpdate",
		"unauthenticated ingress":  "ingress.enabled=true",
		"public without OAuth":     "appMode=public,auth.baseURL=https://learn.example.com",
		"public over HTTP":         "appMode=public,auth.existingSecret=github-auth",
	}
	for name, setting := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := render(t, "--set", setting); err == nil {
				t.Fatal("invalid settings rendered successfully")
			}
		})
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
	docs, out, err := render(t, "-f", "../../charts/romanian/values-microk8s.yaml")
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
