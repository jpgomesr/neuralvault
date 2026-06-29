// Package minio provides a MinIO-backed objectstorage.Client using the AWS SDK v2,
// which is S3-compatible and works with MinIO, AWS S3, and Cloudflare R2.
package minio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/jpgomesr/NeuralVault/internal/config"
)

// Client wraps the AWS S3 client pointed at a MinIO endpoint.
type Client struct {
	s3     *s3.Client
	bucket string
}

// New creates a Client, ensures the configured bucket exists, and verifies
// connectivity via HeadBucket before returning.
func New(ctx context.Context, cfg *config.Config) (*Client, error) {
	scheme := "http"
	if cfg.MinIO.UseSSL {
		scheme = "https"
	}
	endpoint := fmt.Sprintf("%s://%s", scheme, cfg.MinIO.Endpoint)

	s3Client := s3.New(s3.Options{
		BaseEndpoint: aws.String(endpoint),
		Credentials: aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(
			cfg.MinIO.AccessKey,
			cfg.MinIO.SecretKey,
			"",
		)),
		// MinIO requires path-style URLs; the region value is ignored by MinIO
		// but is required by the SDK.
		Region:       "us-east-1",
		UsePathStyle: true,
	})

	if err := ensureBucket(ctx, s3Client, cfg.MinIO.Bucket); err != nil {
		return nil, fmt.Errorf("ensuring bucket %q: %w", cfg.MinIO.Bucket, err)
	}

	slog.Info("minio connected", "endpoint", endpoint, "bucket", cfg.MinIO.Bucket)
	return &Client{s3: s3Client, bucket: cfg.MinIO.Bucket}, nil
}

// ensureBucket creates the bucket if it does not already exist.
func ensureBucket(ctx context.Context, client *s3.Client, bucket string) error {
	_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	})
	if err == nil {
		return nil // bucket already exists
	}

	var notFound *types.NotFound
	if !errors.As(err, &notFound) {
		return fmt.Errorf("checking bucket: %w", err)
	}

	if _, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	}); err != nil {
		return fmt.Errorf("creating bucket: %w", err)
	}
	return nil
}

// Upload streams r to the object at key.
func (c *Client) Upload(ctx context.Context, key string, r io.Reader, size int64) error {
	if _, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(key),
		Body:          r,
		ContentLength: aws.Int64(size),
	}); err != nil {
		return fmt.Errorf("uploading %q: %w", key, err)
	}
	return nil
}

// Download returns a ReadCloser for the object at key. The caller must close it.
func (c *Client) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	result, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("downloading %q: %w", key, err)
	}
	return result.Body, nil
}

// ListObjects returns the keys of all objects whose key starts with prefix.
func (c *Client) ListObjects(ctx context.Context, prefix string) ([]string, error) {
	paginator := s3.NewListObjectsV2Paginator(c.s3, &s3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
		Prefix: aws.String(prefix),
	})

	var keys []string
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing objects (prefix %q): %w", prefix, err)
		}
		for _, obj := range page.Contents {
			keys = append(keys, aws.ToString(obj.Key))
		}
	}
	return keys, nil
}

// Delete removes the object at key.
func (c *Client) Delete(ctx context.Context, key string) error {
	if _, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}); err != nil {
		return fmt.Errorf("deleting %q: %w", key, err)
	}
	return nil
}
