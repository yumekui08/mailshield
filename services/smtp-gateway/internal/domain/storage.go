package domain

import (
	"context"
	"time"
)

// EMLStorage はEMLファイルのオブジェクトストレージを抽象化するインターフェースである。
// MinIO・外部S3の差異を隠蔽し、DIで実装を差し込む。
type EMLStorage interface {
	// Save は EML を保存しオブジェクトキーを返す。receivedAt にはメール受信時刻を渡す。
	Save(ctx context.Context, messageID string, eml []byte, receivedAt time.Time) (path string, err error)
	Get(ctx context.Context, path string) ([]byte, error)
}

// ArchiveStorage は変換済み EML のアーカイブを抽象化するインターフェースである。
// 隔離メールは DB ステータスで管理するため、別メソッドは不要。
type ArchiveStorage interface {
	// ArchiveProcessed は変換後の EML を保存し、保存先のオブジェクトキーを返す。
	// キー形式: processed/{YYYY}/{MM}/{DD}/{message_id}.eml
	// receivedAt にはメール受信時刻を渡す（time.Now() を使うと日付ずれが発生する）。
	ArchiveProcessed(ctx context.Context, messageID string, eml []byte, receivedAt time.Time) (string, error)
}

// AttachmentStorage は添付ファイルの保存と署名付きURL発行を抽象化するインターフェースである。
// フェーズ2では MinIO の署名付き URL を返す。
// フェーズ3では Web UI の一時キー URL に差し替える。
type AttachmentStorage interface {
	// SaveAttachment は添付ファイルを保存してオブジェクトキーを返す。
	// キー形式: attachments/{message_id}/{filename}
	SaveAttachment(ctx context.Context, messageID, filename string, data []byte) (path string, err error)
	// GetPresignedURL は指定パスへのダウンロード用署名付き URL を返す。
	GetPresignedURL(ctx context.Context, path string, expiryHours int) (url string, err error)
}
