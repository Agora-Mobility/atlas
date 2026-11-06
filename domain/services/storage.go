package services

import (
	"context"
	"errors"
	"time"
)

var (
	ErrStorageUnavailable = errors.New("file storage is not available")
	ErrObjectTooLarge     = errors.New("the stored object is larger than allowed")
)

type PresignedURL struct {
	URL       string
	Headers   map[string]string
	ExpiresAt time.Time
}

type StoredObject struct {
	SizeBytes   int64
	ContentType string
}

type IObjectStorage interface {
	PresignUpload(ctx context.Context, key, contentType string, sizeBytes int64) (*PresignedURL, error)
	PresignDownload(ctx context.Context, key string) (*PresignedURL, error)
	PresignDownloadFor(ctx context.Context, key string, ttl time.Duration) (*PresignedURL, error)
	Read(ctx context.Context, key string, maxBytes int64) ([]byte, error)
	Write(ctx context.Context, key, contentType string, content []byte) error
	Stat(ctx context.Context, key string) (*StoredObject, error)
	FirstBytes(ctx context.Context, key string, count int64) ([]byte, error)
	Delete(ctx context.Context, key string) error
}
