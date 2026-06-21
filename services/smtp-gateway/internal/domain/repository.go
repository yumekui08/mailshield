package domain

import (
	"context"
	"time"
)

// ApprovalRequest は承認依頼レコードを表す。
type ApprovalRequest struct {
	ID         string
	MessageID  string
	ApproverID string
	ExpiresAt  time.Time
}

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
	// FindApproverForSender は送信者メールアドレスから承認者のユーザーIDを解決する。
	// 送信者が users テーブルに存在し approver_id が設定されている場合にそれを返す。
	// 見つからない場合は ("", nil) を返す（呼び出し元が fallback を処理する）。
	FindApproverForSender(ctx context.Context, fromAddress string) (approverID string, err error)
	// FindUserIDByEmail はメールアドレスからユーザーIDを返す。承認者フォールバック解決に使用する。
	FindUserIDByEmail(ctx context.Context, email string) (userID string, err error)
	// SaveApprovalRequest は承認依頼レコードを approval_requests テーブルに保存する。
	SaveApprovalRequest(ctx context.Context, req *ApprovalRequest) error
}
