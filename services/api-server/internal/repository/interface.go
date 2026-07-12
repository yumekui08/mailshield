// Package repository はRepositoryインターフェースを定義する。
package repository

import (
	"context"
	"time"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

// Repository はDBへのアクセスを抽象化するインターフェースである。
type Repository interface {
	// ListMessages はメッセージ一覧と総件数を返す。
	ListMessages(ctx context.Context, q domain.ListQuery) ([]domain.Message, int, error)
	// GetMessage はメッセージの詳細情報（検査結果を含む）を返す。
	GetMessage(ctx context.Context, id string) (*domain.MessageDetail, error)
	// ListQuarantine は隔離メッセージ一覧と総件数を返す（status=quarantined固定）。
	ListQuarantine(ctx context.Context, q domain.ListQuery) ([]domain.Message, int, error)
	// GetQuarantine は隔離メッセージの詳細情報を返す。
	GetQuarantine(ctx context.Context, id string) (*domain.MessageDetail, error)
	// UpdateMessageStatus はメッセージの処理状態を更新する。
	UpdateMessageStatus(ctx context.Context, id string, status domain.MessageStatus) error
	// BulkUpdateMessageStatus は複数メッセージの処理状態を一括更新する。status=quarantined のものだけ対象。
	BulkUpdateMessageStatus(ctx context.Context, ids []string, status domain.MessageStatus) error

	// FindUserByEmail はメールアドレスでユーザーを検索する。
	FindUserByEmail(ctx context.Context, email string) (*User, error)
	// CreateUser はユーザーを登録する。
	CreateUser(ctx context.Context, user *User) error
	// UpsertFederatedUser は外部ディレクトリ（OIDC/LDAP/SCIM）からのログイン・同期時に
	// ユーザーを作成または更新する。role の上書き可否は権威の優先順位で決まる:
	// manual（手動） > ldap/scim（ディレクトリ同期） > oidc（groups claim。フォールバック）。
	// 既存行が上位または同格の権威で管理されている場合、下位の source からの role 上書きは行わない。
	// is_active は変更しない（admin が無効化したユーザーをログインだけで再有効化しない）。
	UpsertFederatedUser(ctx context.Context, email, displayName string, role domain.Role, source domain.ProvisionedBy) (*User, error)
	// DeactivateMissingLDAPUsers は provisioned_by=ldap のユーザーのうち、
	// presentEmails に含まれないものを is_active=0 にし、無効化した件数を返す。
	// presentEmails が空の場合は何もしない（LDAP 検索の誤検知で全ユーザーを
	// 無効化することを防ぐ、最後の防衛線として repository 層でもガードする）。
	DeactivateMissingLDAPUsers(ctx context.Context, presentEmails []string) (int, error)
	// CountUsers はユーザー数を返す。
	CountUsers(ctx context.Context) (int, error)
	// ListUsers はユーザー一覧を返す。
	ListUsers(ctx context.Context) ([]User, error)
	// UpdateUserPassword はユーザーのパスワードハッシュを更新する。
	UpdateUserPassword(ctx context.Context, userID, passwordHash string) error
	// UpdateUserRole はユーザーのロールを更新する。
	UpdateUserRole(ctx context.Context, userID string, role domain.Role) error
	// DeleteUser はユーザーを論理削除する（is_active=0）。
	DeleteUser(ctx context.Context, userID string) error

	// CreateMailbox はメールボックスを登録する。
	CreateMailbox(ctx context.Context, mailbox *Mailbox) error
	// ListMailboxes はメールボックス一覧を返す。
	ListMailboxes(ctx context.Context) ([]Mailbox, error)
	// GetMailbox は指定メールボックスを返す。見つからない場合は nil, nil を返す。
	GetMailbox(ctx context.Context, id string) (*Mailbox, error)
	// UpdateMailbox はメールボックスの表示名と有効フラグを更新する。
	UpdateMailbox(ctx context.Context, id, displayName string, isActive bool) error
	// DeleteMailbox はメールボックスを削除する（割り当ても CASCADE 削除）。
	DeleteMailbox(ctx context.Context, id string) error

	// ListAssignments はメールボックスの割り当て一覧を返す。
	ListAssignments(ctx context.Context, mailboxID string) ([]MailboxAssignment, error)
	// AddAssignment はメールボックスにユーザーを割り当てる。重複は無視する。
	AddAssignment(ctx context.Context, assignment *MailboxAssignment) error
	// RemoveAssignment はメールボックスからユーザーの割り当てを削除する。
	RemoveAssignment(ctx context.Context, mailboxID, userID string, role domain.AssignmentRole) error

	// SyncMailboxAssignmentsForUser は 1 ユーザー分の LDAP/SCIM 由来メールボックス割り当てを
	// desired の内容に一致させる。
	//   - desired に含まれるメールボックスが存在しなければ作成する（provisioned_by=source）
	//   - 既存が provisioned_by=manual のメールボックス・割り当ては一切変更しない
	//   - 同一ユーザーの provisioned_by=source な割り当てのうち desired に無いものは削除する
	//
	// スコープはこのユーザーの行だけなので、JIT ログイン時にも定期同期のループ内からも
	// 安全に呼べる（他ユーザーの割り当てには一切影響しない）。
	SyncMailboxAssignmentsForUser(ctx context.Context, userID string, source domain.ProvisionedBy, desired []MailboxAssignmentRequest) error

	// GetMailboxAddressesForUser は指定ロールを持つユーザーのメールボックスアドレス一覧を返す。
	// 隔離一覧の可視性フィルターに使用する。
	GetMailboxAddressesForUser(ctx context.Context, userID string, roles []domain.AssignmentRole) ([]string, error)

	// GetStats はダッシュボード表示用の集計統計を返す。
	// filter が nil の場合は全体の統計を返す（admin/operator 向け）。
	// filter が指定された場合はメールボックス可視性で絞り込んだ統計を返す（viewer 向け）。
	GetStats(ctx context.Context, filter *domain.MailboxVisibilityFilter) (*domain.Stats, error)

	// GetStatsTimeseries は直近 days 日分（当日含む・UTC 日付単位）の日別処理件数を
	// 古い日付から順に返す。メールが1通もない日も件数 0 で埋めて必ず days 要素を返す。
	// filter の扱いは GetStats と同じ。
	GetStatsTimeseries(ctx context.Context, days int, filter *domain.MailboxVisibilityFilter) ([]domain.StatsTimeseriesPoint, error)

	// ListAttachmentsByMessage はメッセージに紐づく添付ファイル一覧を返す（削除済み除く）。
	ListAttachmentsByMessage(ctx context.Context, messageID string) ([]domain.Attachment, error)
	// ListAttachmentsByToken は download_token に紐づく添付ファイル一覧を返す（削除済み除く）。
	ListAttachmentsByToken(ctx context.Context, downloadToken string) ([]domain.Attachment, error)
	// GetAttachmentByToken は download_token とファイル名で添付ファイルを取得する。
	GetAttachmentByToken(ctx context.Context, downloadToken, filename string) (*domain.Attachment, error)
	// ListAttachmentsByTokenPublic は download_token のみで添付ファイル一覧を返す（認証不要）。
	ListAttachmentsByTokenPublic(ctx context.Context, downloadToken string) ([]domain.Attachment, error)
	// GetAttachmentByTokenPublic は download_token とファイル名で添付ファイルを取得する（認証不要）。
	GetAttachmentByTokenPublic(ctx context.Context, downloadToken, filename string) (*domain.Attachment, error)
	// GetAttachmentToAddressesByToken は download_token に紐づく元メッセージの to_addresses を返す。
	// mode=auth のメールボックスロール確認に使用する。
	GetAttachmentToAddressesByToken(ctx context.Context, downloadToken string) ([]string, error)
	// DisableAttachment は添付ファイルのダウンロードを無効化する。
	DisableAttachment(ctx context.Context, id string, disabled bool) error
	// DeleteAttachment は添付ファイルをソフトデリートする。
	DeleteAttachment(ctx context.Context, id string) error

	// CreateAuditLog は監査ログを1件記録する。
	CreateAuditLog(ctx context.Context, log *domain.AuditLog) error
	// ListAuditLogs は監査ログを絞り込み・ページネーションして返す。
	ListAuditLogs(ctx context.Context, q domain.AuditLogQuery) ([]domain.AuditLog, int, error)

	// CreateAPIKey は API キーを登録する。keyHash は SHA-256 ハッシュ値。
	CreateAPIKey(ctx context.Context, key *domain.APIKey, keyHash string) error
	// ListAPIKeys は API キー一覧を返す（revoked 含む）。
	ListAPIKeys(ctx context.Context) ([]domain.APIKey, error)
	// FindAPIKeyByHash は key_hash で API キーを検索する。見つからない場合は nil, nil。
	FindAPIKeyByHash(ctx context.Context, keyHash string) (*domain.APIKey, error)
	// RevokeAPIKey は API キーを即時失効させる。
	RevokeAPIKey(ctx context.Context, id string) error
	// UpdateAPIKeyLastUsed は last_used_at を現在時刻に更新する。
	UpdateAPIKeyLastUsed(ctx context.Context, id string) error

	// ─── 承認フロー ──────────────────────────────────────────────────────────

	// ListApprovalRequests は指定ユーザーが承認できる承認依頼一覧を返す（status=pending のみ）。
	// 「自分が承認者（approver_id）」と「自分が role=admin の対象メールボックスを持つ依頼」
	// の両方を含む。
	ListApprovalRequests(ctx context.Context, userID string) ([]domain.ApprovalRequest, error)
	// IsMailboxAdmin は userID が指定メールボックスに role=admin で割り当てられているかを返す。
	IsMailboxAdmin(ctx context.Context, userID, mailboxEmail string) (bool, error)
	// ListMailboxAdminEmails は指定メールボックスに role=admin で割り当てられた
	// 有効ユーザーのメールアドレス一覧を返す（承認依頼通知の宛先解決）。
	ListMailboxAdminEmails(ctx context.Context, mailboxEmail string) ([]string, error)

	// ─── 承認依頼通知（宛先ごとの送信状態管理） ────────────────────────────
	// 通知は宛先ごとに approval_notifications で管理し、一部の宛先だけ送信に
	// 失敗した場合は失敗した宛先のみ再送する（attempts が上限に達したら諦める）。

	// EnsureApprovalNotifications は依頼の通知宛先行を冪等に作成する。
	EnsureApprovalNotifications(ctx context.Context, approvalID string, recipients []string) error
	// ListPendingNotificationRecipients は未送信かつ試行回数が maxAttempts 未満の宛先を返す。
	ListPendingNotificationRecipients(ctx context.Context, approvalID string, maxAttempts int) ([]string, error)
	// MarkApprovalNotificationResult は宛先ごとの送信結果を記録する。
	MarkApprovalNotificationResult(ctx context.Context, approvalID, recipient string, sent bool, sendErr string) error
	// CountRemainingNotifications は再送対象として残っている宛先数を返す。
	CountRemainingNotifications(ctx context.Context, approvalID string, maxAttempts int) (int, error)
	// ListAllApprovalRequests は全ての承認依頼を返す（admin 向け）。
	ListAllApprovalRequests(ctx context.Context) ([]domain.ApprovalRequest, error)
	// GetApprovalRequest は指定 ID の承認依頼を返す。
	GetApprovalRequest(ctx context.Context, id string) (*domain.ApprovalRequest, error)
	// UpdateApprovalStatus は承認依頼の状態と決定日時・コメントを無条件に更新する
	//（ClaimApprovalRequest 失敗時のロールバック用）。
	UpdateApprovalStatus(ctx context.Context, id string, status domain.ApprovalStatus, comment *string) error
	// ClaimApprovalRequest は status=pending の依頼を原子的に status へ更新する。
	// 更新できた場合 true を返す。false は他の承認者が先に決定済み（またはすでに期限切れ）。
	// 複数承認者（メールボックス admin）の同時決定による二重配送を防ぐ。
	ClaimApprovalRequest(ctx context.Context, id string, status domain.ApprovalStatus, comment *string) (bool, error)
	// MarkApprovalNotificationSent は notification_sent フラグを true にする。
	MarkApprovalNotificationSent(ctx context.Context, id string) error
	// MarkApprovalResultNotified は result_notified フラグを true にする。
	MarkApprovalResultNotified(ctx context.Context, id string) error
	// ListPendingUnnotified は notification_sent=false かつ status=pending の依頼を返す。
	ListPendingUnnotified(ctx context.Context) ([]domain.ApprovalRequest, error)
	// ListResultUnnotified は result_notified=false かつ status が approved/rejected の依頼を返す。
	ListResultUnnotified(ctx context.Context) ([]domain.ApprovalRequest, error)
	// ExpireApprovals は expires_at を過ぎた pending 依頼を expired に更新し、
	// 対象の message_id 一覧を返す（呼び出し元が MinIO 削除・ステータス更新を行う）。
	ExpireApprovals(ctx context.Context) ([]string, error)

	// ─── ユーザー承認者設定 ──────────────────────────────────────────────────

	// GetUser は指定 ID のユーザーを返す。
	GetUser(ctx context.Context, id string) (*User, error)
	// UpdateUserApprover はユーザーの承認者を設定する（nil で解除）。
	UpdateUserApprover(ctx context.Context, userID string, approverID *string) error
	// FindUserByEmailInternal はメールアドレスでユーザーを検索する（承認通知送信先解決用）。
	FindUserByEmailInternal(ctx context.Context, email string) (*User, error)
}

// Mailbox はメールボックス情報を保持する。
type Mailbox struct {
	ID            string
	EmailAddress  string
	DisplayName   string
	IsActive      bool
	ProvisionedBy domain.ProvisionedBy
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// MailboxAssignmentRequest は SyncMailboxAssignmentsForUser への入力 1 件を表す。
// MailboxEmail に一致するメールボックスが存在しない場合、MailboxDisplayName で新規作成する
// （表示名省略時は MailboxEmail をそのまま使う。既存の CreateMailbox と同じ挙動）。
type MailboxAssignmentRequest struct {
	MailboxEmail       string
	MailboxDisplayName string
	Role               domain.AssignmentRole
}

// MailboxAssignment はメールボックスとユーザーの割り当て情報を保持する。
type MailboxAssignment struct {
	ID            string
	MailboxID     string
	UserID        string
	Role          domain.AssignmentRole
	ProvisionedBy domain.ProvisionedBy
	// 表示用（JOIN して取得）
	UserEmail       string
	UserDisplayName string
	CreatedAt       time.Time
}

// User はユーザー情報を保持する（スタンドアロン認証・OIDC/LDAP/SCIM 経由の両方）。
type User struct {
	ID            string
	Email         string
	DisplayName   string
	PasswordHash  string
	Role          domain.Role
	IsActive      bool
	ApproverID    *string
	ProvisionedBy domain.ProvisionedBy
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
