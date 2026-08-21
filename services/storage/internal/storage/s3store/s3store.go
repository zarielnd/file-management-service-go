package s3store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type Store struct {
	client     *s3.Client
	bucket     string
	basePrefix string
}

type Config struct {
	Endpoint     string
	Region       string
	Bucket       string
	AccessKey    string
	SecretKey    string
	UsePathStyle bool
	BasePrefix   string
}

func NewStore(ctx context.Context, cfg Config) (*Store, error) {
	loadOptions := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		),
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.UsePathStyle = cfg.UsePathStyle
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
	})

	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(cfg.Bucket),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to access bucket %s: %w", cfg.Bucket, err)
	}

	return &Store{
		client:     client,
		bucket:     cfg.Bucket,
		basePrefix: cfg.BasePrefix,
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

func (s *Store) PresignFetch(ctx context.Context, path string, expiry time.Duration) (string, error) {
	key := s.key(path)
	presignClient := s3.NewPresignClient(s.client)
	req, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("failed to presign s3://%s/%s: %w", s.bucket, key, err)
	}
	return req.URL, nil
}

func (s *Store) PresignStore(ctx context.Context, path string, expiry time.Duration) (string, error) {
	key := s.key(path)
	presignClient := s3.NewPresignClient(s.client)
	req, err := presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		ContentType: aws.String("application/octet-stream"),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("failed to presign PUT s3://%s/%s: %w", s.bucket, key, err)
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
