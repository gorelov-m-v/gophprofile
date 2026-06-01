package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const maxDownloadSize = 50 << 20

type Object struct {
	Key         string
	Data        []byte
	ContentType string
	ETag        string
}

type Client interface {
	EnsureBucket(ctx context.Context) error
	Upload(ctx context.Context, key string, data []byte, contentType string) error
	Download(ctx context.Context, key string) (Object, error)
	Delete(ctx context.Context, key string) error
	DeleteMany(ctx context.Context, keys []string) error
	Ping(ctx context.Context) error
	PublicURL(key string) string
}

type S3 struct {
	client    *minio.Client
	bucket    string
	publicURL string
}

func NewS3(endpoint, accessKey, secretKey, bucket string, useSSL bool, publicURL string) (*S3, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("create s3 client: %w", err)
	}
	return &S3{client: client, bucket: bucket, publicURL: strings.TrimRight(publicURL, "/")}, nil
}

func (s *S3) EnsureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("check bucket: %w", err)
	}
	if exists {
		return nil
	}
	if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("make bucket: %w", err)
	}
	return nil
}

func (s *S3) Upload(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("put object %q: %w", key, err)
	}
	return nil
}

func (s *S3) Download(ctx context.Context, key string) (Object, error) {
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return Object{}, fmt.Errorf("stat object %q: %w", key, err)
	}
	if info.Size > maxDownloadSize {
		return Object{}, fmt.Errorf("object %q is too large", key)
	}
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return Object{}, fmt.Errorf("get object %q: %w", key, err)
	}
	defer obj.Close()

	data, err := io.ReadAll(io.LimitReader(obj, maxDownloadSize+1))
	if err != nil {
		return Object{}, fmt.Errorf("read object %q: %w", key, err)
	}
	if len(data) > maxDownloadSize {
		return Object{}, fmt.Errorf("object %q is too large", key)
	}
	return Object{
		Key:         key,
		Data:        data,
		ContentType: info.ContentType,
		ETag:        info.ETag,
	}, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	if key == "" {
		return nil
	}
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("remove object %q: %w", key, err)
	}
	return nil
}

func (s *S3) DeleteMany(ctx context.Context, keys []string) error {
	for _, key := range keys {
		if err := s.Delete(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

func (s *S3) Ping(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("bucket %q does not exist", s.bucket)
	}
	return nil
}

func (s *S3) PublicURL(key string) string {
	if key == "" || s.publicURL == "" {
		return ""
	}
	parsed, err := url.Parse(s.publicURL)
	if err != nil {
		return s.publicURL + "/" + strings.TrimLeft(key, "/")
	}
	parsed.Path = path.Join(parsed.Path, key)
	return parsed.String()
}
