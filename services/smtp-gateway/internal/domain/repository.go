package domain

import "context"

// MailRepository はDBへのアクセスを抽象化するインターフェースである。
// MariaDB・PostgreSQLの差異を隠蔽し、DIで実装を差し込む。
type MailRepository interface {
	SaveMessage(ctx context.Context, msg *Mail) error
	UpdateMessageStatus(ctx context.Context, messageID string, status MessageStatus) error
	SaveInspectResult(ctx context.Context, result *InspectResult, messageID string) error
	// SaveAttachment は分離した添付ファイルのメタデータを mail_attachments テーブルに保存する。
	SaveAttachment(ctx context.Context, att *MailAttachment) error
	// UpdateProcessedEMLPath は変換後 EML の MinIO パスを mail_messages テーブルに記録する。
	// archiveAsync が MinIO への保存完了後に呼び出す。
	UpdateProcessedEMLPath(ctx context.Context, messageID, path string) error
}
