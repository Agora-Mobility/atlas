package r2

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/Agora-Mobility/atlas/domain/services"
)

const region = "auto"

type Settings struct {
	Endpoint    string
	AccessKeyID string
	SecretKey   string
	Bucket      string
	PresignTTL  time.Duration
}

type Storage struct {
	client  *s3.Client
	presign *s3.PresignClient
	bucket  string
	ttl     time.Duration
	now     func() time.Time
}

var _ services.IObjectStorage = (*Storage)(nil)

func New(settings Settings) *Storage {
	client := s3.New(s3.Options{
		Region:                     region,
		BaseEndpoint:               aws.String(settings.Endpoint),
		Credentials:                credentials.NewStaticCredentialsProvider(settings.AccessKeyID, settings.SecretKey, ""),
		UsePathStyle:               true,
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
	return &Storage{
		client:  client,
		presign: s3.NewPresignClient(client),
		bucket:  settings.Bucket,
		ttl:     settings.PresignTTL,
		now:     time.Now,
	}
}

func (s *Storage) PresignUpload(ctx context.Context, key, contentType string, sizeBytes int64) (*services.PresignedURL, error) {
	expiresAt := s.now().Add(s.ttl)
	request, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.bucket, Key: &key, ContentType: &contentType, ContentLength: &sizeBytes,
	}, s3.WithPresignExpires(s.ttl))
	if err != nil {
		return nil, fmt.Errorf("presign upload: %w", err)
	}
	return &services.PresignedURL{
		URL:       request.URL,
		Headers:   map[string]string{"Content-Type": contentType, "Content-Length": fmt.Sprint(sizeBytes)},
		ExpiresAt: expiresAt,
	}, nil
}

func (s *Storage) PresignDownload(ctx context.Context, key string) (*services.PresignedURL, error) {
	expiresAt := s.now().Add(s.ttl)
	request, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key}, s3.WithPresignExpires(s.ttl))
	if err != nil {
		return nil, fmt.Errorf("presign download: %w", err)
	}
	return &services.PresignedURL{URL: request.URL, ExpiresAt: expiresAt}, nil
}

func (s *Storage) Stat(ctx context.Context, key string) (*services.StoredObject, error) {
	head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: &s.bucket, Key: &key})
	if isNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("head object: %w", err)
	}
	return &services.StoredObject{SizeBytes: aws.ToInt64(head.ContentLength), ContentType: aws.ToString(head.ContentType)}, nil
}

func (s *Storage) FirstBytes(ctx context.Context, key string, count int64) ([]byte, error) {
	byteRange := fmt.Sprintf("bytes=0-%d", count-1)
	object, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key, Range: &byteRange})
	if err != nil {
		return nil, fmt.Errorf("read object: %w", err)
	}
	defer func() { _ = object.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(object.Body, count))
	if err != nil {
		return nil, fmt.Errorf("read object body: %w", err)
	}
	return data, nil
}

func (s *Storage) Delete(ctx context.Context, key string) error {
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: &key}); err != nil {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

func isNotFound(err error) bool {
	var response *smithyhttp.ResponseError
	return errors.As(err, &response) && response.HTTPStatusCode() == http.StatusNotFound
}

type Unavailable struct{}

var _ services.IObjectStorage = Unavailable{}

func (Unavailable) PresignUpload(context.Context, string, string, int64) (*services.PresignedURL, error) {
	return nil, services.ErrStorageUnavailable
}

func (Unavailable) PresignDownload(context.Context, string) (*services.PresignedURL, error) {
	return nil, services.ErrStorageUnavailable
}

func (Unavailable) Stat(context.Context, string) (*services.StoredObject, error) {
	return nil, services.ErrStorageUnavailable
}

func (Unavailable) FirstBytes(context.Context, string, int64) ([]byte, error) {
	return nil, services.ErrStorageUnavailable
}

func (Unavailable) Delete(context.Context, string) error {
	return services.ErrStorageUnavailable
}
