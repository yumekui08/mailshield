package domain

import (
	"context"
	"time"
)

// ApprovalRequest は承認依頼レコードを表す。
// ApproverID と MailboxEmails はどちらか一方のみ設定する:
//   - ApproverID     : システム全体のフォールバック承認者（approval.global_approver_email）。
//     メールボックスに承認者がいない場合のみ使う
//   - MailboxEmails  : メールボックス承認（主経路）。対象メールボックス（1..n）のいずれかに
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
