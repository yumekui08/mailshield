// Package domain はドメイン型を定義する。
// このパッケージは外部ライブラリに依存しない。
package domain

import "time"

// Role はユーザーのロールを表す。
type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

// ProvisionedBy はユーザーの role・display_name の同期主体を表す。
// OIDC/LDAP/SCIM 経由でログイン・同期されたユーザーは、Web UI で手動作成・編集された
// ユーザー（manual）の role を上書きしない。手動設定を外部ディレクトリより優先するためである。
type ProvisionedBy string

const (
	ProvisionedByManual ProvisionedBy = "manual"
	ProvisionedByOIDC   ProvisionedBy = "oidc"
	ProvisionedByLDAP   ProvisionedBy = "ldap"
	ProvisionedBySCIM   ProvisionedBy = "scim"
)

// UserClaims はOIDCトークンから取得するユーザー情報を表す。
type UserClaims struct {
	Sub    string   `json:"sub"`
	Email  string   `json:"email"`
	Name   string   `json:"name"`
	Groups []string `json:"groups"`
}

// Session はRedisに保存するセッション情報を表す。
type Session struct {
	ID           string     `json:"id"`
	User         UserClaims `json:"user"`
	Role         Role       `json:"role"`
	AccessToken  string     `json:"access_token"`
	RefreshToken string     `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time  `json:"expires_at"`
}

// MessageStatus はメールの処理状態を表す。
type MessageStatus string

const (
	StatusReceived        MessageStatus = "received"
	StatusProcessing      MessageStatus = "processing"
	StatusDelivered       MessageStatus = "delivered"
	StatusQuarantined     MessageStatus = "quarantined"
	StatusRejected        MessageStatus = "rejected"
	StatusApprovalPending MessageStatus = "approval_pending"
	StatusExpired         MessageStatus = "expired"
)

// Message はメールのメタデータを表す。
type Message struct {
	ID               string        `json:"id"`
	EMLPath          string        `json:"eml_path"`
	FromAddress      string        `json:"from_address"`
	ToAddresses      []string      `json:"to_addresses"`
	Subject          string        `json:"subject"`
	SizeBytes        int64         `json:"size_bytes"`
	HasAttachment    bool          `json:"has_attachment"`
	RspamdScore      float64       `json:"rspamd_score"`
	SPFResult        string        `json:"spf_result"`
	DKIMResult       string        `json:"dkim_result"`
	DMARCResult      string        `json:"dmarc_result"`
	Status           MessageStatus `json:"status"`
	ProcessedEMLPath *string       `json:"processed_eml_path,omitempty"` // archive 完了後に記録
	ReceivedAt       time.Time     `json:"received_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
}

// InspectResult は検査ワーカーの結果を表す。
type InspectResult struct {
	ID         string         `json:"id"`
	WorkerName string         `json:"worker_name"`
	Score      int            `json:"score"`
	Detected   bool           `json:"detected"`
	Details    map[string]any `json:"details"`
	CreatedAt  time.Time      `json:"created_at"`
}

// MessageDetail はメッセージの詳細情報（検査結果を含む）を表す。
type MessageDetail struct {
	Message
	InspectResults []InspectResult `json:"inspect_results"`
}

// AssignmentRole はメールボックスへのユーザー割り当てロールを表す。
type AssignmentRole string

const (
	AssignmentRoleMember AssignmentRole = "member"
	AssignmentRoleOwner  AssignmentRole = "owner"
	AssignmentRoleAdmin  AssignmentRole = "admin"
)

// MailboxVisibilityFilter は viewer ロールのユーザーに対する隔離一覧の絞り込み条件を表す。
// nil の場合はフィルターなし（admin/operator は全件閲覧可）。
type MailboxVisibilityFilter struct {
	// InboundMailboxes は閲覧可能な受信先メールボックスのアドレス一覧。
	// to_addresses がこのいずれかに一致するメールが表示される。
	InboundMailboxes []string
	// OutboundMailboxes は閲覧可能な送信元メールボックスのアドレス一覧。
	// from_address がこのいずれかに一致するメールが表示される。
	OutboundMailboxes []string
}

// ListQuery はメッセージ一覧取得のクエリパラメータを表す。
type ListQuery struct {
	Page          int
	PerPage       int
	Status        string
	From          string
	To            string
	Subject       string
	Since         *time.Time
	Until         *time.Time
	HasAttachment *bool
	Sort          string // received_at | subject | from_address | size_bytes
	Order         string // asc | desc
	// VisibilityFilter は viewer ロール用の可視性フィルター。nil なら全件対象。
	VisibilityFilter *MailboxVisibilityFilter
}

// PageMeta はページネーションのメタ情報を表す。
type PageMeta struct {
	Total      int `json:"total"`
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	TotalPages int `json:"total_pages"`
}

// PagedResult はページング付きのレスポンスを表す。
type PagedResult[T any] struct {
	Data []T      `json:"data"`
	Meta PageMeta `json:"meta"`
}

// StatsPeriod は特定期間のメール処理件数を表す。
type StatsPeriod struct {
	Delivered   int `json:"delivered"`
	Quarantined int `json:"quarantined"`
	Rejected    int `json:"rejected"`
	Total       int `json:"total"`
}

// Stats はダッシュボード表示用の集計統計を表す。
type Stats struct {
	Today StatsPeriod `json:"today"`
	Week  StatsPeriod `json:"week"`
}

// StatsTimeseriesPoint は日別のメール処理件数を表す。
// Date は UTC 基準の日付（YYYY-MM-DD）。
type StatsTimeseriesPoint struct {
	Date        string `json:"date"`
	Delivered   int    `json:"delivered"`
	Quarantined int    `json:"quarantined"`
	Rejected    int    `json:"rejected"`
	Total       int    `json:"total"`
}

// StorageBackend は添付ファイルの保存先を表す。
type StorageBackend string

const (
	StorageBackendS3  StorageBackend = "s3"
	StorageBackendSPO StorageBackend = "spo"
)

// DownloadMode は添付ファイルのダウンロード認証方式を表す。
type DownloadMode string

const (
	DownloadModeSimple DownloadMode = "simple" // 認証なし（トークンのみ）
	DownloadModeOTP    DownloadMode = "otp"    // メールアドレス OTP 認証
	DownloadModeAuth   DownloadMode = "auth"   // MailShield ログイン必須
)

// ApprovalStatus は承認依頼の状態を表す。
type ApprovalStatus string

const (
	ApprovalStatusPending  ApprovalStatus = "pending"
	ApprovalStatusApproved ApprovalStatus = "approved"
	ApprovalStatusRejected ApprovalStatus = "rejected"
	ApprovalStatusExpired  ApprovalStatus = "expired"
)

// ApprovalRequest は承認依頼レコードを表す。
type ApprovalRequest struct {
	ID               string         `json:"id"`
	MessageID        string         `json:"message_id"`
	ApproverID       string         `json:"approver_id"`
	Status           ApprovalStatus `json:"status"`
	Comment          *string        `json:"comment,omitempty"`
	NotificationSent bool           `json:"-"`
	ResultNotified   bool           `json:"-"`
	DecidedAt        *time.Time     `json:"decided_at,omitempty"`
	ExpiresAt        time.Time      `json:"expires_at"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

// ApprovalRequestDetail は承認依頼と紐づくメール情報をまとめた詳細表現。
type ApprovalRequestDetail struct {
	ApprovalRequest
	Message Message `json:"message"`
}

// Attachment は分離された添付ファイルのメタデータを表す。
type Attachment struct {
	ID             string         `json:"id"`
	MessageID      string         `json:"message_id"`
	DownloadToken  string         `json:"download_token"`
	Filename       string         `json:"filename"`
	ContentType    string         `json:"content_type"`
	SizeBytes      int64          `json:"size_bytes"`
	StorageBackend StorageBackend `json:"storage_backend"`
	StoragePath    string         `json:"-"` // クライアントに露出しない
	IsDisabled     bool           `json:"is_disabled"`
	DownloadMode   DownloadMode   `json:"download_mode"`
	CreatedAt      time.Time      `json:"created_at"`
}
