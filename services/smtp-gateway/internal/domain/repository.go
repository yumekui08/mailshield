package domain

import (
	"context"
	"time"
)

// ApprovalRequest は承認依頼レコードを表す。
// ApproverID と MailboxEmails はどちらか一方のみ設定する:
//   - ApproverID     : ユーザー個人を承認者に指定（users.approver_id 経由の解決）
//   - MailboxEmails  : メールボックス承認。対象メールボックス（1..n）のいずれかに
//     role=admin で割り当てられたユーザー全員が承認できる
type ApprovalRequest struct {
	ID            string
	MessageID     string
	ApproverID    string
	MailboxEmails []string
	ExpiresAt     time.Time
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
	// CountMailboxAdmins は指定メールボックスに role=admin で割り当てられた
	// 有効ユーザー数を返す。メールボックス承認者の解決に使用する。
	CountMailboxAdmins(ctx context.Context, mailboxEmail string) (int, error)
	// SaveApprovalRequest は承認依頼レコードを approval_requests テーブルに保存する。
	SaveApprovalRequest(ctx context.Context, req *ApprovalRequest) error
	// SaveDelayedRelease は遅延送信レコードを delayed_releases テーブルに保存する。
	SaveDelayedRelease(ctx context.Context, rel *DelayedRelease) error
}

// DelayedRelease は遅延送信（送信ディレイ）レコードを表す。
type DelayedRelease struct {
	ID        string
	MessageID string
	ReleaseAt time.Time
}
