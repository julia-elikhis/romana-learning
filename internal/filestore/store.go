package filestore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"

	"cloud.google.com/go/storage"
)

// Store holds originals; object names are generated internally, never user paths.
type Store interface {
	Put(context.Context, string, []byte) error
	Open(context.Context, string) (io.ReadCloser, error)
	Close() error
}

var safeKey = regexp.MustCompile(`^[a-f0-9]{32}\.original$`)

type Filesystem struct{ root *os.Root }

func NewFilesystem(path string) (*Filesystem, error) {
	if err := os.MkdirAll(path, 0700); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	return &Filesystem{root}, nil
}
func (f *Filesystem) Put(ctx context.Context, key string, data []byte) error {
	if !safeKey.MatchString(key) {
		return errors.New("invalid object key")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	out, err := f.root.OpenFile(key, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, bytes.NewReader(data))
	closeErr := out.Close()
	if err != nil {
		_ = f.root.Remove(key)
		return err
	}
	return closeErr
}
func (f *Filesystem) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if !safeKey.MatchString(key) {
		return nil, errors.New("invalid object key")
	}
	return f.root.Open(key)
}
func (f *Filesystem) Close() error { return f.root.Close() }

type GCS struct {
	client         *storage.Client
	bucket, prefix string
}

func NewGCS(ctx context.Context, bucket, prefix string) (*GCS, error) {
	if bucket == "" {
		return nil, errors.New("COURSE_STORAGE_BUCKET is required")
	}
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, errors.New("could not initialize Google Cloud credentials")
	}
	if prefix != "" {
		prefix = strings.TrimSuffix(prefix, "/") + "/"
	}
	return &GCS{client, bucket, prefix}, nil
}
func (g *GCS) Put(ctx context.Context, key string, data []byte) error {
	if !safeKey.MatchString(key) {
		return errors.New("invalid object key")
	}
	writer := g.client.Bucket(g.bucket).Object(g.prefix + key).If(storage.Conditions{DoesNotExist: true}).NewWriter(ctx)
	writer.ContentType = "application/octet-stream"
	writer.ChunkSize = 1024 * 1024
	if _, err := writer.Write(data); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}
func (g *GCS) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if !safeKey.MatchString(key) {
		return nil, errors.New("invalid object key")
	}
	return g.client.Bucket(g.bucket).Object(g.prefix + key).NewReader(ctx)
}
func (g *GCS) Close() error { return g.client.Close() }

func FromEnv(ctx context.Context) (Store, error) {
	switch os.Getenv("COURSE_STORAGE_PROVIDER") {
	case "", "disabled":
		return nil, nil
	case "filesystem":
		path := os.Getenv("COURSE_STORAGE_PATH")
		if path == "" {
			return nil, errors.New("COURSE_STORAGE_PATH is required")
		}
		return NewFilesystem(path)
	case "s3":
		return NewS3(os.Getenv("COURSE_S3_ENDPOINT"), os.Getenv("COURSE_STORAGE_BUCKET"), os.Getenv("COURSE_STORAGE_PREFIX"), os.Getenv("COURSE_S3_REGION"), os.Getenv("COURSE_S3_ACCESS_KEY"), os.Getenv("COURSE_S3_SECRET_KEY"))
	case "gcs":
		return NewGCS(ctx, os.Getenv("COURSE_STORAGE_BUCKET"), os.Getenv("COURSE_STORAGE_PREFIX"))
	default:
		return nil, errors.New("unknown course storage provider")
	}
}
