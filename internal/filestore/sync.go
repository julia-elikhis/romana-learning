package filestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

type Original struct {
	Key  string `json:"key"`
	Hash string `json:"hash"`
}

// SyncOriginals copies immutable originals and verifies them against database hashes.
// It never removes source files or replaces conflicting destination objects.
func SyncOriginals(ctx context.Context, source, target Store, originals []Original) error {
	for _, original := range originals {
		if !safeKey.MatchString(original.Key) || len(original.Hash) != 64 {
			return errors.New("invalid original metadata")
		}
		data, err := readOriginal(ctx, source, original)
		if err != nil {
			return err
		}
		reader, err := target.Open(ctx, original.Key)
		if err == nil {
			existing, readErr := io.ReadAll(io.LimitReader(reader, 20*1024*1024+1))
			reader.Close()
			if readErr != nil || !matchesOriginal(existing, original) {
				return errors.New("destination original differs from the database hash")
			}
			continue
		}
		if err = target.Put(ctx, original.Key, data); err != nil {
			return errors.New("could not copy original without overwriting")
		}
		if _, err = readOriginal(ctx, target, original); err != nil {
			return err
		}
	}
	return nil
}

func readOriginal(ctx context.Context, store Store, original Original) ([]byte, error) {
	reader, err := store.Open(ctx, original.Key)
	if err != nil {
		return nil, errors.New("original is unavailable")
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, 20*1024*1024+1))
	if err != nil || !matchesOriginal(data, original) {
		return nil, errors.New("original does not match its database hash")
	}
	return data, nil
}

func matchesOriginal(data []byte, original Original) bool {
	digest := sha256.Sum256(data)
	return len(data) <= 20*1024*1024 && hex.EncodeToString(digest[:]) == original.Hash
}

// RemoveOriginal is used by the isolated test-account cleanup, not an HTTP route.
func RemoveOriginal(ctx context.Context, store Store, key string) error {
	if !safeKey.MatchString(key) {
		return errors.New("invalid original key")
	}
	switch s := store.(type) {
	case *GCS:
		err := s.client.Bucket(s.bucket).Object(s.prefix + key).Delete(ctx)
		if errors.Is(err, storage.ErrObjectNotExist) {
			return nil
		}
		return err
	case *S3:
		return s.remove(ctx, key)
	default:
		return errors.New("unsupported cleanup provider")
	}
}

// CheckAccess makes an authenticated bucket request, including for an empty library.
func CheckAccess(ctx context.Context, store Store) error {
	g, ok := store.(*GCS)
	if !ok {
		return errors.New("Google access check requires GCS")
	}
	_, err := g.client.Bucket(g.bucket).Objects(ctx, &storage.Query{Prefix: g.prefix}).Next()
	if errors.Is(err, iterator.Done) {
		return nil
	}
	return err
}
