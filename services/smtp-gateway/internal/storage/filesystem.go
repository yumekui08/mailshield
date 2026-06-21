package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FilesystemStorage はローカルファイルシステムを使ったストレージ実装。
// MinIO/S3 が不要な単一ノード構成向け。
// EMLStorage, ArchiveStorage, AttachmentStorage を実装する。
type FilesystemStorage struct {
	baseDir       string
	publicBaseURL string // GetPresignedURL 用ベース URL（空の場合はエラーを返す）
}

// NewFilesystem はローカルファイルシステムストレージを初期化する。
// baseDir 配下に raw/processed/attachments サブディレクトリを作成して保存する。
func NewFilesystem(baseDir, publicBaseURL string) (*FilesystemStorage, error) {
	if baseDir == "" {
		return nil, errors.New("storage.local_dir が設定されていません")
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("ストレージディレクトリ作成失敗 (%s): %w", baseDir, err)
	}
	return &FilesystemStorage{baseDir: baseDir, publicBaseURL: publicBaseURL}, nil
}

// Save は EML をローカル FS に保存してパスを返す。
// キー形式: raw/{YYYY}/{MM}/{DD}/{message_id}.eml
func (s *FilesystemStorage) Save(_ context.Context, messageID string, eml []byte, receivedAt time.Time) (string, error) {
	rel := fmt.Sprintf("raw/%s/%s/%s/%s.eml",
		receivedAt.UTC().Format("2006"),
		receivedAt.UTC().Format("01"),
		receivedAt.UTC().Format("02"),
		messageID,
	)
	return s.write(rel, eml)
}

// Get は指定パスの EML を返す。
func (s *FilesystemStorage) Get(_ context.Context, path string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(s.baseDir, path))
	if err != nil {
		return nil, fmt.Errorf("EML 読み込み失敗 (%s): %w", path, err)
	}
	return data, nil
}

// ArchiveProcessed は処理済み EML を保存してパスを返す。
// キー形式: processed/{YYYY}/{MM}/{DD}/{message_id}.eml
func (s *FilesystemStorage) ArchiveProcessed(_ context.Context, messageID string, eml []byte, receivedAt time.Time) (string, error) {
	rel := fmt.Sprintf("processed/%s/%s/%s/%s.eml",
		receivedAt.UTC().Format("2006"),
		receivedAt.UTC().Format("01"),
		receivedAt.UTC().Format("02"),
		messageID,
	)
	return s.write(rel, eml)
}

// SaveAttachment は添付ファイルを保存してパスを返す。
// キー形式: attachments/{message_id}/{filename}
func (s *FilesystemStorage) SaveAttachment(_ context.Context, messageID, filename string, data []byte) (string, error) {
	rel := fmt.Sprintf("attachments/%s/%s", messageID, filename)
	return s.write(rel, data)
}

// DeleteAttachment は保存済み添付ファイルを削除する。ファイルが存在しない場合は成功とする。
func (s *FilesystemStorage) DeleteAttachment(_ context.Context, path string) error {
	full := filepath.Join(s.baseDir, path)
	if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("添付ファイル削除失敗 (%s): %w", path, err)
	}
	return nil
}

// GetPresignedURL は添付ファイルのダウンロード URL を返す。
// filesystem モードでは storage.public_base_url の設定が必要。
// 未設定の場合はエラーを返す。
func (s *FilesystemStorage) GetPresignedURL(_ context.Context, path string, _ int) (string, error) {
	if s.publicBaseURL == "" {
		return "", errors.New("filesystem ストレージモードで署名付き URL を生成するには storage.public_base_url の設定が必要です")
	}
	return s.publicBaseURL + "/internal/files/" + path, nil
}

func (s *FilesystemStorage) write(rel string, data []byte) (string, error) {
	full := filepath.Join(s.baseDir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", fmt.Errorf("ディレクトリ作成失敗: %w", err)
	}
	if err := os.WriteFile(full, data, 0o644); err != nil {
		return "", fmt.Errorf("ファイル書き込み失敗 (%s): %w", rel, err)
	}
	return rel, nil
}
