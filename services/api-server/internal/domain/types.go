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
	StatusDelayed         MessageStatus = "delayed"
	StatusExpired         MessageStatus = "expired"
)

// DelayedReleaseStatus は遅延送信レコードの状態を表す。
type DelayedReleaseStatus string

const (
	DelayedPending   DelayedReleaseStatus = "pending"
	DelayedReleased  DelayedReleaseStatus = "released"
	DelayedCancelled DelayedReleaseStatus = "cancelled"
)

// DelayedRelease は遅延送信（送信ディレイ）レコードを表す。
// Message は一覧・詳細表示のために結合したメール情報（一覧クエリで埋める）。
type DelayedRelease struct {
	ID        string               `json:"id"`
	MessageID string               `json:"message_id"`
	ReleaseAt time.Time            `json:"release_at"`
	Status    DelayedReleaseStatus `json:"status"`
	DecidedBy *string              `json:"decided_by,omitempty"`
	DecidedAt *time.Time           `json:"decided_at,omitempty"`
	CreatedAt time.Time            `json:"created_at"`
	// 一覧表示用のメール属性（結合）
	FromAddress   string   `json:"from_address"`
	ToAddresses   []string `json:"to_addresses"`
	Subject       string   `json:"subject"`
	SizeBytes     int64    `json:"size_bytes"`
	HasAttachment bool     `json:"has_attachment"`
}

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

// AssignmentRole はメールボックスへのユーザー割り当てロール（機能ロール）を表す。
//   - member   : 受信担当。受信隔離の閲覧・解放、添付ダウンロード
//   - owner    : 送信担当。送信隔離の閲覧・解放、送信ディレイの取消/即時送信
//   - approver : 承認担当。承認フローに回った自メールボックスのメールを承認/却下/再配送
//
// メールボックス自体の管理（割り当て編集）はシステム RBAC（operator/admin）が担う。
type AssignmentRole string

const (
	AssignmentRoleMember   AssignmentRole = "member"
	AssignmentRoleOwner    AssignmentRole = "owner"
	AssignmentRoleApprover AssignmentRole = "approver"
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
// ApproverID と MailboxEmails はどちらか一方のみ設定される:
//   - ApproverID    : システム全体のフォールバック承認者（approval.global_approver_email）。
//     メールボックスに承認者がいない場合のみ使う
//   - MailboxEmails : メールボックス承認（主経路）。対象メールボックス（1..n）のいずれかに
//     role=approver で割り当てられたユーザー全員が承認・却下できる（先に決定した人が有効）
type ApprovalRequest struct {
	ID               string         `json:"id"`
	MessageID        string         `json:"message_id"`
	ApproverID       *string        `json:"approver_id"`
	MailboxEmails    []string       `json:"mailbox_emails"`
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
	// TextBody / HTMLBody は EML から抽出した本文（Web UI 表示用）。
	// HTMLBody はサンドボックス iframe で描画すること（XSS 対策）。
	TextBody    string       `json:"text_body"`
	HTMLBody    string       `json:"html_body"`
	Attachments []Attachment `json:"attachments"`
}

// ApprovalRequestListItem は承認一覧の 1 行（依頼 + メール件名・送信元）を表す。
type ApprovalRequestListItem struct {
	ApprovalRequest
	Subject     string `json:"subject"`
	FromAddress string `json:"from_address"`
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

// ─── 設定エンティティ（ADR 008・設定 WebUI 化） ────────────────────────────

// WorkerKind はワーカーインスタンスの種別（検査 or 変換）を表す。
type WorkerKind string

const (
	WorkerKindInspect   WorkerKind = "inspect"
	WorkerKindTransform WorkerKind = "transform"
)

// WorkerInstance はワーカー型＋型固有設定＋名前を持つ再利用可能な部品。
// ルーティングから alias で参照される。
//
//   - Alias       : 条件 DSL・検査結果のキーに使う短い安定ハンドル（rename-safe）
//   - DisplayName : 画面表示用（日本語可・変更可）
//   - WorkerType  : コード側で登録されたワーカー型名（filesep-worker 等）
//   - Config      : 型固有設定（不透明な JSON。アプリ層で検証）
type WorkerInstance struct {
	ID                    string         `json:"id"`
	Alias                 string         `json:"alias"`
	DisplayName           string         `json:"display_name"`
	WorkerType            string         `json:"worker_type"`
	Kind                  WorkerKind     `json:"kind"`
	Config                map[string]any `json:"config"`
	DefaultTimeoutSeconds int            `json:"default_timeout_seconds"`
	IsEnabled             bool           `json:"is_enabled"`
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`
}

// ConfigVariable は設定内から ${VAR} で参照する共有値（非機密・環境依存）。
// シークレットはここに入れず OS 環境変数のままにすること。
type ConfigVariable struct {
	ID          string    `json:"id"`
	Key         string    `json:"key"`
	Value       string    `json:"value"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// WorkerBinding はルーティングがワーカーインスタンスを束ねる 1 要素。
// Alias でワーカーインスタンスを参照し、有効無効と（任意の）タイムアウト上書きを持つ。
type WorkerBinding struct {
	Alias          string `json:"alias"`
	Enabled        bool   `json:"enabled"`
	TimeoutSeconds *int   `json:"timeout_seconds,omitempty"` // nil ならインスタンス既定値
}

// Routing はメールがどの検査・変換・ポリシーを通るかを決める合成単位（ADR 008）。
//   - Priority   : 評価順（昇順・小さいほど先）。first-match で最初に match した 1 つだけを通す
//   - MatchExpr  : この経路に載せる条件式（catch-all は "true"）
//   - IsCatchAll : システムが保証する最終フォールバック（削除・並べ替え不可・match/priority 固定）
//   - Inspect    : 並列実行される検査インスタンスの束ね
//   - Transform  : 定義順に直列実行される変換インスタンスの束ね
//   - PolicyRef  : 適用するポリシー名
type Routing struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Priority   int             `json:"priority"`
	MatchExpr  string          `json:"match_expr"`
	Direction  string          `json:"direction"` // inbound | outbound | internal（mail.Direction 決定用）
	IsCatchAll bool            `json:"is_catchall"`
	IsEnabled  bool            `json:"is_enabled"`
	PolicyRef  string          `json:"policy_ref"`
	Inspect    []WorkerBinding `json:"inspect"`
	Transform  []WorkerBinding `json:"transform"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// ConfigVersion は検証済み設定スナップショット（canonical JSON）の 1 版（ADR 008 ③-2）。
type ConfigVersion struct {
	ID        string    `json:"id"`
	Checksum  string    `json:"checksum"`
	Source    string    `json:"source"` // "ui" | "file"
	Author    string    `json:"author"`
	Content   string    `json:"content"` // canonical スナップショット JSON
	CreatedAt time.Time `json:"created_at"`
}

// ConfigSnapshot は gateway がインメモリにパイプラインを構築するための canonical モデル。
// 変数・ワーカーインスタンス・ルーティングを 1 つに束ねる。api-server と smtp-gateway で
// この JSON 形が唯一の契約になる（両モジュールで同形を定義する）。
type ConfigSnapshot struct {
	Variables       []ConfigVariable `json:"variables"`
	WorkerInstances []WorkerInstance `json:"worker_instances"`
	Routings        []Routing        `json:"routings"`
}
