package storage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinIOConfig struct {
	Endpoint       string
	PublicEndpoint string
	AccessKey      string
	SecretKey      string
	Bucket         string
	Region         string
	UseSSL         bool
	AutoCreate     bool
}

type MinIOStore struct {
	client     *minio.Client
	presigner  *minio.Client
	bucket     string
	region     string
	autoCreate bool
}

func NewMinIOStore(config MinIOConfig) (*MinIOStore, error) {
	options := &minio.Options{
		Creds:  credentials.NewStaticV4(config.AccessKey, config.SecretKey, ""),
		Secure: config.UseSSL,
		Region: config.Region,
	}
	client, err := minio.New(config.Endpoint, options)
	if err != nil {
		return nil, fmt.Errorf("create object storage client: %w", err)
	}
	presigner, err := minio.New(config.PublicEndpoint, options)
	if err != nil {
		return nil, fmt.Errorf("create object storage presigner: %w", err)
	}
	return &MinIOStore{
		client: client, presigner: presigner, bucket: config.Bucket,
		region: config.Region, autoCreate: config.AutoCreate,
	}, nil
}

func (s *MinIOStore) EnsureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check object storage bucket: %w", err)
	}
	if exists {
		return nil
	}
	if !s.autoCreate {
		return fmt.Errorf("object storage bucket %q does not exist", s.bucket)
	}
	if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: s.region}); err != nil {
		return fmt.Errorf("create object storage bucket: %w", err)
	}
	return nil
}

func (s *MinIOStore) PresignPut(ctx context.Context, key, contentType string, expiry time.Duration) (PresignedUpload, error) {
	headers := http.Header{"Content-Type": []string{contentType}}
	url, err := s.presigner.PresignHeader(ctx, http.MethodPut, s.bucket, key, expiry, nil, headers)
	if err != nil {
		return PresignedUpload{}, fmt.Errorf("presign object upload: %w", err)
	}
	return PresignedUpload{URL: url.String(), Headers: headers}, nil
}

func (s *MinIOStore) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		response := minio.ToErrorResponse(err)
		if response.Code == "NoSuchKey" || response.Code == "NoSuchObject" || response.StatusCode == http.StatusNotFound {
			return ObjectInfo{}, ErrObjectNotFound
		}
		return ObjectInfo{}, fmt.Errorf("stat object: %w", err)
	}
	return ObjectInfo{
		Key: key, Size: info.Size, ContentType: info.ContentType,
		ETag: info.ETag, LastModified: info.LastModified,
	}, nil
}

func (s *MinIOStore) Delete(ctx context.Context, key string) error {
	err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	if err != nil && !errors.Is(err, ErrObjectNotFound) {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}
