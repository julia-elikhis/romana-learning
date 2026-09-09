package filestore

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

func TestS3PreservesImmutableObjectKeys(t *testing.T) {
	const key = "0123456789abcdef0123456789abcdef.original"
	var stored []byte
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path != "/courses/prefix/"+key {
			t.Error("Wrong bucket or object prefix")
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Error("Request is not signed")
		}
		switch r.Method {
		case "PUT":
			if r.Header.Get("If-None-Match") != "*" {
				t.Error("Missing conditional create")
			}
			if stored != nil {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(412)
				io.WriteString(w, `<Error><Code>PreconditionFailed</Code></Error>`)
				return
			}
			var reader io.Reader = r.Body
			if strings.Contains(r.Header.Get("Content-Encoding"), "aws-chunked") {
				reader = httputil.NewChunkedReader(r.Body)
			}
			stored, _ = io.ReadAll(reader)
			w.Header().Set("ETag", `"test"`)
		case "HEAD", "GET":
			if stored == nil {
				w.WriteHeader(404)
				return
			}
			w.Header().Set("ETag", `"test"`)
			w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
			w.Header().Set("Content-Type", "application/octet-stream")
			if r.Method == "GET" {
				w.Write(stored)
			}
		}
	}))
	defer server.Close()
	store, err := NewS3(server.URL, "courses", "prefix", "us-east-1", "test-access", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Put(context.Background(), key, []byte("lesson")); err != nil {
		t.Fatal(err)
	}
	if store.Put(context.Background(), key, []byte("replacement")) == nil {
		t.Fatal("Replaced an original")
	}
	if store.Put(context.Background(), "../escape", nil) == nil {
		t.Fatal("Unsafe key accepted")
	}
	if !bytes.Equal(stored, []byte("lesson")) {
		t.Fatal("Original changed")
	}
}
func TestS3ConfigurationDoesNotExposeCredentials(t *testing.T) {
	for _, endpoint := range []string{"https://user:private@example.com", "ftp://example.com", "https://example.com/prefix", "https://example.com?token=private"} {
		_, err := NewS3(endpoint, "courses", "", "", "access", "private")
		if err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("Invalid endpoint accepted or exposed")
		}
	}
}
func TestMinIOLiveRoundTrip(t *testing.T) {
	endpoint := os.Getenv("TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("Set TEST_S3_ENDPOINT to test local MinIO")
	}
	ctx := context.Background()
	prefix := "test-" + time.Now().UTC().Format("20060102T150405.000000000")
	store, err := NewS3(endpoint, "courses", prefix, "us-east-1", "romanian-local", "local-minio-development-only")
	if err != nil {
		t.Fatal(err)
	}
	key := "0123456789abcdef0123456789abcdef.original"
	defer store.client.RemoveObject(ctx, store.bucket, store.prefix+key, minio.RemoveObjectOptions{})
	// Upload more than the multipart threshold to verify the conditional single PUT path.
	payload := bytes.Repeat([]byte("Romanian lesson\n"), 500000)
	var wg sync.WaitGroup
	result := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); result <- store.Put(ctx, key, payload) }()
	}
	wg.Wait()
	close(result)
	successes := 0
	for err := range result {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("Expected one conditional-create winner, got %d", successes)
	}
	reader, err := store.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || !bytes.Equal(raw, payload) {
		t.Fatal("Stored original did not round trip")
	}
	if _, err = store.Open(ctx, "ffffffffffffffffffffffffffffffff.original"); err == nil {
		t.Fatal("Missing original was not detected before streaming")
	}
}
