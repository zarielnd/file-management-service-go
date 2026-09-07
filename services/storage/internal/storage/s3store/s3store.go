package s3store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	storageMiddleware "github.com/zarielnd/file-management-service-go/services/storage/internal/middleware"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws"
)

type Store struct {
	client                *s3.Client
	internalPresignClient *s3.PresignClient
	externalPresignClient *s3.PresignClient
	bucket                string
	archiveBucket         string
	basePrefix            string
}

type Config struct {
	Endpoint       string
	PublicEndpoint string
	Region         string
	Bucket         string
	ArchiveBucket  string
	AccessKey      string
	SecretKey      string
	UsePathStyle   bool
	BasePrefix     string
}

func NewStore(ctx context.Context, cfg Config) (*Store, error) {
	loadOptions := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				cfg.AccessKey,
				cfg.SecretKey,
				"",
			),
		),
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Internal client — actual HTTP calls to MinIO/S3.
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.UsePathStyle

		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(strings.TrimRight(cfg.Endpoint, "/"))
		}

		o.APIOptions = append(o.APIOptions, storageMiddleware.GCSCompatibleHeaders)
		// Add OpenTelemetry instrumentation.
		otelaws.AppendMiddlewares(&o.APIOptions)
	})

	// Verify main bucket.
	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(cfg.Bucket),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to access bucket %s: %w", cfg.Bucket, err)
	}

	if cfg.ArchiveBucket == "" {
		return nil, fmt.Errorf("archive bucket is required")
	}

	// Verify archive bucket.
	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(cfg.ArchiveBucket),
	})
	if err != nil {
		return nil, fmt.Errorf(
			"failed to access archive bucket %s: %w",
			cfg.ArchiveBucket,
			err,
		)
	}

	// External client — only used for browser-facing presigned URLs.
	externalS3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.UsePathStyle
		if cfg.PublicEndpoint != "" {
			o.BaseEndpoint = aws.String(strings.TrimRight(cfg.PublicEndpoint, "/"))
		} else if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(strings.TrimRight(cfg.Endpoint, "/"))
		}
		o.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		o.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
		o.APIOptions = append(o.APIOptions, storageMiddleware.GCSCompatibleHeaders)
	})

	return &Store{
		client:                client,
		internalPresignClient: s3.NewPresignClient(client),
		externalPresignClient: s3.NewPresignClient(externalS3Client),
		bucket:                cfg.Bucket,
		archiveBucket:         cfg.ArchiveBucket,
		basePrefix:            cfg.BasePrefix,
	}, nil
}

func (s *Store) key(path string) string {
	if s.basePrefix == "" {
		return path
	}
	return s.basePrefix + path
}

func (s *Store) Store(ctx context.Context, path string, reader io.Reader) (int64, error) {
	key := s.key(path)

	var written int64
	counter := &countingReader{Reader: reader, written: &written}

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   counter,
	})
	if err != nil {
		return 0, fmt.Errorf("failed to upload to s3://%s/%s: %w", s.bucket, key, err)
	}

	return written, nil
}

func (s *Store) Fetch(ctx context.Context, path string) (io.ReadCloser, error) {
	key := s.key(path)

	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var nsk *types.NoSuchKey
		if errors.As(err, &nsk) {
			return nil, fmt.Errorf("file not found")
		}
		return nil, fmt.Errorf("failed to fetch s3://%s/%s: %w", s.bucket, key, err)
	}

	return out.Body, nil
}

// PresignFetch — used by GetDownloadURLs (worker downloads files). Internal endpoint.
func (s *Store) PresignFetch(ctx context.Context, path string, expiry time.Duration) (string, error) {
	key := s.key(path)
	req, err := s.internalPresignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("failed to presign s3://%s/%s: %w", s.bucket, key, err)
	}
	return req.URL, nil
}

// PresignStore — used by ReserveUpload (file-server gets URL for client upload). Internal endpoint.
func (s *Store) PresignStore(ctx context.Context, path string, expiry time.Duration) (string, error) {
	key := s.key(path)
	req, err := s.internalPresignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		ContentType: aws.String("application/octet-stream"),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("failed to presign PUT s3://%s/%s: %w", s.bucket, key, err)
	}
	return req.URL, nil
}

// PresignArchiveStore — used by GetArchiveUploadURL (worker uploads archive). Internal endpoint.
func (s *Store) PresignArchiveStore(ctx context.Context, path string, contentType string, expiry time.Duration) (string, error) {
	key := s.key(path)
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	req, err := s.internalPresignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.archiveBucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("failed to presign archive PUT s3://%s/%s: %w", s.archiveBucket, key, err)
	}
	return req.URL, nil
}

// PresignArchiveFetch — used by GetArchiveDownloadURL (browser downloads archive). External endpoint.
func (s *Store) PresignArchiveFetch(ctx context.Context, path string, expiry time.Duration) (string, error) {
	key := s.key(path)
	req, err := s.externalPresignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.archiveBucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("failed to presign archive GET s3://%s/%s: %w", s.archiveBucket, key, err)
	}
	return req.URL, nil
}

type countingReader struct {
	io.Reader
	written *int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.Reader.Read(p)
	*c.written += int64(n)
	return n, err
}
