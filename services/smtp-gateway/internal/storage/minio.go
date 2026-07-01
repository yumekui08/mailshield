package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Storage は MinIO (S3互換) を使った EMLStorage / AttachmentStorage / ArchiveStorage 実装である。
type Storage struct {
	client            *s3.Client
	presignClient     *s3.PresignClient
	bucketEML         string // 原本・処理済み EML を格納する単一バケット
	bucketAttachments string
}

// New は MinIO クライアントを初期化して Storage を返す。
func New(endpoint, accessKey, secretKey, bucketEML, bucketAttachments, region string, useSSL bool) (*Storage, error) {
	if region == "" {
		region = "us-east-1"
	}
	scheme := "http"
	if useSSL {
		scheme = "https"
	}
	baseEndpoint := fmt.Sprintf("%s://%s", scheme, endpoint)

	client := s3.New(s3.Options{
		Region:       region,
		Credentials:  aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
		BaseEndpoint: aws.String(baseEndpoint),
		UsePathStyle: true,
	})

	return &Storage{
		client:            client,
		presignClient:     s3.NewPresignClient(client),
		bucketEML:         bucketEML,
		bucketAttachments: bucketAttachments,
	}, nil
}

func (s *Storage) Save(ctx context.Context, messageID string, eml []byte, receivedAt time.Time) (string, error) {
	key := buildKey("raw", messageID, receivedAt)
	if err := s.putEML(ctx, s.bucketEML, key, eml); err != nil {
		return "", err
	}
	return key, nil
}

// Get は指定されたキーから EML を取得する。
func (s *Storage) Get(ctx context.Context, path string) ([]byte, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucketEML),
		Key:    aws.String(path),
	})
	if err != nil {
		return nil, fmt.Errorf("MinIO からの取得失敗 (path=%s): %w", path, err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("MinIO レスポンス読み取り失敗: %w", err)
	}
	return data, nil
}

// receivedAt を使うことで goroutine の遅延によるパス日付ずれを防ぐ
func (s *Storage) ArchiveProcessed(ctx context.Context, messageID string, eml []byte, receivedAt time.Time) (string, error) {
	key := buildKey("processed", messageID, receivedAt)
	if err := s.putEML(ctx, s.bucketEML, key, eml); err != nil {
		return "", err
	}
	return key, nil
}

func (s *Storage) SaveAttachment(ctx context.Context, messageID, filename string, data []byte) (string, error) {
	key := fmt.Sprintf("%s/%s", messageID, filename)

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucketAttachments),
		Key:           aws.String(key),
		Body:          bytes.NewReader(data),
		ContentLength: aws.Int64(int64(len(data))),
		ContentType:   aws.String("application/octet-stream"),
	})
	if err != nil {
		return "", fmt.Errorf("添付ファイル保存失敗 (key=%s): %w", key, err)
	}
	return key, nil
}

func (s *Storage) DeleteAttachment(ctx context.Context, path string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucketAttachments),
		Key:    aws.String(path),
	})
	if err != nil {
		return fmt.Errorf("添付ファイル削除失敗 (key=%s): %w", path, err)
	}
	return nil
}

func (s *Storage) GetPresignedURL(ctx context.Context, path string, expiryHours int) (string, error) {
	req, err := s.presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucketAttachments),
		Key:    aws.String(path),
	}, s3.WithPresignExpires(time.Duration(expiryHours)*time.Hour))
	if err != nil {
		return "", fmt.Errorf("署名付きURL生成失敗 (path=%s): %w", path, err)
	}
	return req.URL, nil
}

func (s *Storage) putEML(ctx context.Context, bucket, key string, data []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Body:          bytes.NewReader(data),
		Key:           aws.String(key),
		ContentLength: aws.Int64(int64(len(data))),
		ContentType:   aws.String("message/rfc822"),
	})
	if err != nil {
		return fmt.Errorf("MinIO への保存失敗 (bucket=%s, key=%s): %w", bucket, key, err)
	}
	return nil
}

func buildKey(kind, messageID string, t time.Time) string {
	d := t.UTC()
	return fmt.Sprintf("%s/%04d/%02d/%02d/%s.eml",
		kind, d.Year(), int(d.Month()), d.Day(), messageID)
}
