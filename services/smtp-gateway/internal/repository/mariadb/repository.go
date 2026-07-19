// Package mariadb は MailRepository インターフェースの MariaDB 実装を提供する。
package mariadb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql" // MariaDB/MySQL ドライバー
	"github.com/google/uuid"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

// Config は DB 接続プールの設定を保持する。
type Config struct {
	MaxOpenConns           int
	MaxIdleConns           int
	ConnMaxLifetimeMinutes int
	PingTimeoutSeconds     int
}

// Repository は MariaDB を使った MailRepository 実装である。
type Repository struct {
	db *sql.DB
}

// New は MariaDB 接続を確立して Repository を返す。
// プール設定が 0 の場合は安全なデフォルト値を使用する。
func New(dsn string, cfg Config) (*Repository, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("MariaDB 接続オープン失敗: %w", err)
	}

	maxOpen := cfg.MaxOpenConns
	if maxOpen == 0 {
		maxOpen = 10
	}
	maxIdle := cfg.MaxIdleConns
	if maxIdle == 0 {
		maxIdle = 5
	}
	maxLifetime := cfg.ConnMaxLifetimeMinutes
	if maxLifetime == 0 {
		maxLifetime = 5
	}

	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(time.Duration(maxLifetime) * time.Minute)

	pingTimeout := cfg.PingTimeoutSeconds
	if pingTimeout == 0 {
		pingTimeout = 5
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(pingTimeout)*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("MariaDB 疎通確認失敗: %w", err)
	}

	return &Repository{db: db}, nil
}

// Close はDB接続を閉じる。
func (r *Repository) Close() error {
	return r.db.Close()
}

// Ping はDB疎通を確認する。/readyz のレディネスチェックに使用する。
func (r *Repository) Ping(ctx context.Context) error {
	return r.db.PingContext(ctx)
}

// SaveMessage はメールメタデータを mail_messages テーブルに保存する。
func (r *Repository) SaveMessage(ctx context.Context, msg *domain.Mail) error {
	toJSON, err := json.Marshal(msg.ToAddresses)
	if err != nil {
		return fmt.Errorf("to_addresses JSON エンコード失敗: %w", err)
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO mail_messages
			(id, eml_path, from_address, to_addresses, subject,
			 size_bytes, has_attachment, rspamd_score,
			 spf_result, dkim_result, dmarc_result,
			 status, received_at, direction)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.MessageID,
		msg.EMLPath,
		msg.FromAddress,
		toJSON,
		msg.Subject,
		msg.SizeBytes,
		boolToInt(msg.HasAttachment),
		msg.RspamdScore,
		string(msg.AuthResults.SPF),
		string(msg.AuthResults.DKIM),
		string(msg.AuthResults.DMARC),
		string(domain.StatusReceived),
		msg.ReceivedAt.UTC(),
		string(msg.Direction),
	)
	if err != nil {
		return fmt.Errorf("mail_messages 保存失敗 (message_id=%s): %w",
			msg.MessageID, err)
	}
	return nil
}

// UpdateMessageStatus はメッセージの処理状態を更新する。
func (r *Repository) UpdateMessageStatus(ctx context.Context, messageID string, status domain.MessageStatus) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE mail_messages SET status = ? WHERE id = ?`,
		string(status), messageID,
	)
	if err != nil {
		return fmt.Errorf("status 更新失敗 (message_id=%s): %w", messageID, err)
	}
	return nil
}

// SaveInspectResult は検査ワーカーの結果を inspect_results テーブルに保存する。
func (r *Repository) SaveInspectResult(ctx context.Context, result *domain.InspectResult, messageID string) error {
	detailsJSON, err := json.Marshal(result.Details)
	if err != nil {
		return fmt.Errorf("details JSON エンコード失敗: %w", err)
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO inspect_results (id, message_id, worker_name, score, detected, details)
		VALUES (?, ?, ?, ?, ?, ?)`,
		uuid.New().String(),
		messageID,
		result.WorkerName,
		result.Score,
		boolToInt(result.Detected),
		detailsJSON,
	)
	if err != nil {
		return fmt.Errorf("inspect_results 保存失敗 (message_id=%s, worker=%s): %w",
			messageID, result.WorkerName, err)
	}
	return nil
}

// SaveAttachment は分離した添付ファイルのメタデータを mail_attachments テーブルに保存する。
func (r *Repository) SaveAttachment(ctx context.Context, att *domain.MailAttachment) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO mail_attachments
			(id, message_id, download_token, filename, content_type,
			 size_bytes, storage_backend, storage_path, download_mode)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		att.ID,
		att.MessageID,
		att.DownloadToken,
		att.Filename,
		att.ContentType,
		att.SizeBytes,
		string(att.StorageBackend),
		att.StoragePath,
		string(att.DownloadMode),
	)
	if err != nil {
		return fmt.Errorf("mail_attachments 保存失敗 (message_id=%s, filename=%s): %w",
			att.MessageID, att.Filename, err)
	}
	return nil
}

// UpdateProcessedEMLPath は変換後 EML の MinIO パスを mail_messages テーブルに記録する。
func (r *Repository) UpdateProcessedEMLPath(ctx context.Context, messageID, path string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE mail_messages SET processed_eml_path = ? WHERE id = ?`,
		path, messageID,
	)
	if err != nil {
		return fmt.Errorf("processed_eml_path 更新失敗 (message_id=%s): %w", messageID, err)
	}
	return nil
}

// FindUserIDByEmail はメールアドレスからユーザーIDを返す。
func (r *Repository) FindUserIDByEmail(ctx context.Context, email string) (string, error) {
	var userID string
	err := r.db.QueryRowContext(ctx,
		`SELECT id FROM users WHERE email = ? AND is_active = 1 LIMIT 1`,
		email,
	).Scan(&userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("ユーザーID解決失敗 (email=%s): %w", email, err)
	}
	return userID, nil
}

// SaveApprovalRequest は承認依頼レコードと対象メールボックス（0..n）を
// トランザクションで保存する。ApproverID は空文字を NULL として保存する。
func (r *Repository) SaveApprovalRequest(ctx context.Context, req *domain.ApprovalRequest) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("approval_requests トランザクション開始失敗: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // コミット後の Rollback は no-op

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO approval_requests (id, message_id, approver_id, expires_at)
		 VALUES (?, ?, ?, ?)`,
		req.ID, req.MessageID, nullIfEmpty(req.ApproverID), req.ExpiresAt.UTC(),
	); err != nil {
		return fmt.Errorf("approval_requests 保存失敗 (message_id=%s): %w", req.MessageID, err)
	}

	for _, mailbox := range req.MailboxEmails {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO approval_request_mailboxes (id, approval_request_id, mailbox_email)
			 VALUES (?, ?, ?)`,
			uuid.New().String(), req.ID, mailbox,
		); err != nil {
			return fmt.Errorf("approval_request_mailboxes 保存失敗 (message_id=%s, mailbox=%s): %w",
				req.MessageID, mailbox, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("approval_requests コミット失敗 (message_id=%s): %w", req.MessageID, err)
	}
	return nil
}

// SaveDelayedRelease は遅延送信レコードを delayed_releases テーブルに保存する。
func (r *Repository) SaveDelayedRelease(ctx context.Context, rel *domain.DelayedRelease) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO delayed_releases (id, message_id, release_at)
		 VALUES (?, ?, ?)`,
		rel.ID, rel.MessageID, rel.ReleaseAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("delayed_releases 保存失敗 (message_id=%s): %w", rel.MessageID, err)
	}
	return nil
}

// CountMailboxApprovers は指定メールボックスに role=approver で割り当てられた有効ユーザー数を返す。
func (r *Repository) CountMailboxApprovers(ctx context.Context, mailboxEmail string) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*)
		   FROM mailbox_assignments a
		   JOIN mailboxes m ON m.id = a.mailbox_id
		   JOIN users u     ON u.id = a.user_id
		  WHERE m.email_address = ? AND m.is_active = 1
		    AND a.role = 'approver' AND u.is_active = 1`,
		mailboxEmail,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("メールボックス承認者数取得失敗 (mailbox=%s): %w", mailboxEmail, err)
	}
	return count, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ReadActiveConfig はアクティブな設定スナップショット（checksum と content）を返す（ADR 008 ③-2b）。
// 未設定なら ("", "", nil)。gateway はこれをポーリングし、差分時にパイプラインを再構築する。
func (r *Repository) ReadActiveConfig(ctx context.Context) (checksum, content string, err error) {
	const q = `
		SELECT v.checksum, v.content
		FROM config_active a
		JOIN config_versions v ON v.id = a.version_id
		WHERE a.id = 1`
	err = r.db.QueryRowContext(ctx, q).Scan(&checksum, &content)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("アクティブ設定読み取り失敗: %w", err)
	}
	return checksum, content, nil
}
