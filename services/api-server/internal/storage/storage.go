// Package storage は EML・添付ファイルダウンロード用の MinIO (S3互換) クライアントを提供する。
package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// EMLStorage は EML の取得・署名付きダウンロード URL 生成を抽象化するインターフェースである。
type EMLStorage interface {
	GetPresignedURL(ctx context.Context, path string, expiryHours int) (string, error)
	// GetEML は指定されたオブジェクトキーから EML のバイト列を取得する。
	// 隔離解放時の再インジェクトで使用する。
	GetEML(ctx context.Context, path string) ([]byte, error)
}

// AttachmentStorage は添付ファイルをプロキシダウンロードするインターフェースである。
type AttachmentStorage interface {
	// GetAttachment は指定されたオブジェクトキーのデータと Content-Type を返す。
	GetAttachment(ctx context.Context, path string) (io.ReadCloser, string, error)
}

// MinIOStorage は MinIO (S3互換) を使った EMLStorage / AttachmentStorage 実装である。
type MinIOStorage struct {
	presignClient     *s3.PresignClient
	client            *s3.Client
	bucketEML         string
	bucketAttachments string
}

// New は MinIO クライアントを初期化して MinIOStorage を返す。
//
// presigned URL の署名には SigV4 が使われ、署名計算に Host ヘッダーが含まれる。
// そのため、presign クライアントのベースエンドポイントは
// ブラウザが実際にアクセスするエンドポイント（public_endpoint）に合わせる必要がある。
// public_endpoint が空の場合は endpoint をそのまま使用する。
func New(endpoint, publicEndpoint, accessKey, secretKey, bucketEML, bucketAttachments string, useSSL, publicUseSSL bool) (*MinIOStorage, error) {
	scheme := "http"
	if useSSL {
		scheme = "https"
	}
	internalBase := fmt.Sprintf("%s://%s", scheme, endpoint)
	internalClient := s3.New(s3.Options{
		Region:       "us-east-1",
		Credentials:  aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
		BaseEndpoint: aws.String(internalBase),
		UsePathStyle: true,
	})

	presignEndpoint := endpoint
	presignScheme := scheme
	if publicEndpoint != "" {
		presignEndpoint = publicEndpoint
		presignScheme = "http"
		if publicUseSSL {
			presignScheme = "https"
		}
	}
	presignBase := fmt.Sprintf("%s://%s", presignScheme, presignEndpoint)
	presignClient := s3.New(s3.Options{
		Region:       "us-east-1",
		Credentials:  aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
		BaseEndpoint: aws.String(presignBase),
		UsePathStyle: true,
	})

	return &MinIOStorage{
		presignClient:     s3.NewPresignClient(presignClient),
		client:            internalClient,
		bucketEML:         bucketEML,
		bucketAttachments: bucketAttachments,
	}, nil
}

// GetPresignedURL は指定されたオブジェクトキーへの署名付きダウンロード URL を生成する。
func (s *MinIOStorage) GetPresignedURL(ctx context.Context, path string, expiryHours int) (string, error) {
	req, err := s.presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucketEML),
		Key:    aws.String(path),
	}, s3.WithPresignExpires(time.Duration(expiryHours)*time.Hour))
	if err != nil {
		return "", fmt.Errorf("署名付きURL生成失敗 (path=%s): %w", path, err)
	}
	return req.URL, nil
}

// GetEML は EML バケットから指定パスの EML バイト列を取得する。
func (s *MinIOStorage) GetEML(ctx context.Context, path string) ([]byte, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucketEML),
		Key:    aws.String(path),
	})
	if err != nil {
		return nil, fmt.Errorf("EML 取得失敗 (path=%s): %w", path, err)
	}
	defer out.Body.Close()
	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("EML 読み取り失敗 (path=%s): %w", path, err)
	}
	return data, nil
}

// GetAttachment は添付ファイルバケットから指定パスのデータを取得する。
// 呼び出し元は返された io.ReadCloser を必ず Close すること。
func (s *MinIOStorage) GetAttachment(ctx context.Context, path string) (io.ReadCloser, string, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucketAttachments),
		Key:    aws.String(path),
	})
	if err != nil {
		return nil, "", fmt.Errorf("添付ファイル取得失敗 (path=%s): %w", path, err)
	}
	ct := "application/octet-stream"
	if out.ContentType != nil && *out.ContentType != "" {
		ct = *out.ContentType
	}
	return out.Body, ct, nil
}
