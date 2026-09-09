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
	if docs["PersistentVolumeClaim"] != nil || pod(docs)["volumes"] != nil {
		t.Fatal("identity-based GCS must not mount disks/keys")
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
	}
	for name, setting := range tests {
		t.Run(name, func(t *testing.T) {
			if _, _, err := render(t, "--set", setting); err == nil {
				t.Fatal("invalid settings rendered successfully")
			}
		})
	}
}
