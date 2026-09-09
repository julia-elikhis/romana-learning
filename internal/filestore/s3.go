package filestore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3 uses path-style addressing so private MinIO service names work without DNS aliases.
type S3 struct {
	client         *minio.Client
	bucket, prefix string
}

func NewS3(endpoint, bucket, prefix, region, accessKey, secretKey string) (*S3, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("COURSE_S3_ENDPOINT must be an HTTP(S) origin without credentials or a path")
	}
	if bucket == "" || accessKey == "" || secretKey == "" {
		return nil, errors.New("S3 storage requires a bucket and access credentials")
	}
	client, err := minio.New(u.Host, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""), Secure: u.Scheme == "https", Region: region, BucketLookup: minio.BucketLookupPath})
	if err != nil {
		return nil, errors.New("Could not initialize S3 storage")
	}
	if prefix != "" {
		prefix = strings.TrimSuffix(prefix, "/") + "/"
	}
	return &S3{client: client, bucket: bucket, prefix: prefix}, nil
}
func (s *S3) Put(ctx context.Context, key string, data []byte) error {
	if !safeKey.MatchString(key) {
		return errors.New("invalid object key")
	}
	options := minio.PutObjectOptions{ContentType: "application/octet-stream", DisableMultipart: true}
	options.SetMatchETagExcept("*")
	_, err := s.client.PutObject(ctx, s.bucket, s.prefix+key, bytes.NewReader(data), int64(len(data)), options)
	if err != nil {
		return errors.New("Could not write the original to S3 storage")
	}
	return nil
}
func (s *S3) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if !safeKey.MatchString(key) {
		return nil, errors.New("invalid object key")
	}
	object, err := s.client.GetObject(ctx, s.bucket, s.prefix+key, minio.GetObjectOptions{})
	if err != nil {
		return nil, errors.New("Could not read the original from S3 storage")
	}
	// GetObject is lazy: check existence before the download handler commits HTTP headers.
	if _, err = object.Stat(); err != nil {
		object.Close()
		return nil, errors.New("Original is unavailable in S3 storage")
	}
	return object, nil
}
func (s *S3) Close() error { return nil }

func (s *S3) remove(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, s.prefix+key, minio.RemoveObjectOptions{})
}
