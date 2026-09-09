package filestore

import (
	"context"
	"io"
	"testing"
)

func TestFilesystemOriginalsAreImmutableAndConfined(t *testing.T) {
	store, err := NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	key := "0123456789abcdef0123456789abcdef.original"
	if err = store.Put(ctx, key, []byte("private lesson")); err != nil {
		t.Fatal(err)
	}
	if store.Put(ctx, key, []byte("replacement")) == nil {
		t.Fatal("overwrote original")
	}
	r, err := store.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(r)
	r.Close()
	if string(got) != "private lesson" {
		t.Fatal("original changed")
	}
	for _, bad := range []string{"../escape", "/tmp/escape", "lesson.docx", "0123456789abcdef0123456789abcdef.original/../escape"} {
		if store.Put(ctx, bad, nil) == nil {
			t.Fatal("unsafe key accepted")
		}
		if r, err = store.Open(ctx, bad); err == nil {
			r.Close()
			t.Fatal("unsafe read accepted")
		}
	}
}
