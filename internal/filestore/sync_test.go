package filestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"testing"
)

func TestSyncOriginalsPreservesAndVerifiesDocuments(t *testing.T) {
	for _, scenario := range []string{"empty destination", "same original", "conflict", "corrupt source", "missing source"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			source, err := NewFilesystem(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer source.Close()
			target, err := NewFilesystem(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer target.Close()
			key := "0123456789abcdef0123456789abcdef.original"
			data := []byte("Lecția: Îmi place să învăț limba română.")
			digest := sha256.Sum256(data)
			original := Original{Key: key, Hash: hex.EncodeToString(digest[:])}
			sourceData := data
			if scenario == "corrupt source" {
				sourceData = []byte("damaged original")
			}
			if scenario != "missing source" {
				if err := source.Put(ctx, key, sourceData); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "same original" {
				if err := target.Put(ctx, key, data); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "conflict" {
				if err := target.Put(ctx, key, []byte("keep this conflicting file")); err != nil {
					t.Fatal(err)
				}
			}
			err = SyncOriginals(ctx, source, target, []Original{original, original})
			wantError := scenario == "conflict" || scenario == "corrupt source" || scenario == "missing source"
			if (err != nil) != wantError {
				t.Fatalf("unexpected copy result: %v", err)
			}
			if scenario != "missing source" {
				r, err := source.Open(ctx, key)
				if err != nil {
					t.Fatal("source removed", err)
				}
				got, _ := io.ReadAll(r)
				r.Close()
				if string(got) != string(sourceData) {
					t.Fatal("source altered")
				}
			}
			if !wantError || scenario == "conflict" {
				r, err := target.Open(ctx, key)
				if err != nil {
					t.Fatal(err)
				}
				got, _ := io.ReadAll(r)
				r.Close()
				want := string(data)
				if scenario == "conflict" {
					want = "keep this conflicting file"
				}
				if string(got) != want {
					t.Fatal("destination overwritten or corrupted")
				}
			} else if r, err := target.Open(ctx, key); err == nil {
				r.Close()
				t.Fatal("invalid source was copied")
			}
		})
	}
}
