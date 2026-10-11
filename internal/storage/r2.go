// Package storage wraps the object store (Cloudflare R2) that holds files
// too large for the database, e.g. farm photos. Metadata for each stored
// object lives in storage.file; only the bytes live here.
package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ObjectStore is the seam handlers depend on, so tests can swap in a fake
// instead of talking to R2.
type ObjectStore interface {
	Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error
	PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error)
}

type R2Config struct {
	AccountID       string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
}

// R2ConfigFromEnv reports ok=false when any variable is missing, so the
// server can still boot (with uploads disabled) in environments that
// haven't been given R2 credentials.
func R2ConfigFromEnv() (R2Config, bool) {
	cfg := R2Config{
		AccountID:       strings.TrimSpace(os.Getenv("R2_ACCOUNT_ID")),
		AccessKeyID:     strings.TrimSpace(os.Getenv("R2_ACCESS_KEY_ID")),
		SecretAccessKey: strings.TrimSpace(os.Getenv("R2_SECRET_ACCESS_KEY")),
		Bucket:          strings.TrimSpace(os.Getenv("R2_BUCKET")),
	}
	ok := cfg.AccountID != "" && cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" && cfg.Bucket != ""
	return cfg, ok
}

type R2Store struct {
	client *minio.Client
	bucket string
}

func NewR2(cfg R2Config) (*R2Store, error) {
	client, err := minio.New(fmt.Sprintf("%s.r2.cloudflarestorage.com", cfg.AccountID), &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: true,
		// Setting the region up front stops minio from calling
		// GetBucketLocation, which an Object Read & Write token isn't
		// allowed to do.
		Region:       "auto",
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		return nil, err
	}
	return &R2Store{client: client, bucket: cfg.Bucket}, nil
}

func (s *R2Store) Put(ctx context.Context, key string, body io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, body, size, minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (s *R2Store) PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	u, err := s.client.PresignedGetObject(ctx, s.bucket, key, expiry, url.Values{})
	if err != nil {
		return "", err
	}
	return u.String(), nil
}
