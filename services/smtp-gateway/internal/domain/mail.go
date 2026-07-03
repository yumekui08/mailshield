// Package domain はドメイン型とインターフェースを定義する。外部ライブラリに依存しない。
package domain

import "time"

type Direction string

const (
	DirectionInbound  Direction = "inbound"  // 外部 → 内部
	DirectionOutbound Direction = "outbound" // 内部 → 外部
	DirectionInternal Direction = "internal" // 内部 → 内部
)

type DownloadMode string

const (
	DownloadModeSimple DownloadMode = "simple" // 認証なし（トークンのみ）
	DownloadModeOTP    DownloadMode = "otp"    // メールアドレス OTP 認証
	DownloadModeAuth   DownloadMode = "auth"   // MailShield ログイン必須
)

type AuthResult string

const (
	AuthPass AuthResult = "pass"
	AuthFail AuthResult = "fail"
	AuthNone AuthResult = "none"
)

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

// AuthResults は SPF/DKIM/DMARC の検証結果を保持する。
// JSON タグは mail.received イベント（docs/specs/queues.md）の
// ワイヤーフォーマット（小文字キー）に合わせている。
type AuthResults struct {
	SPF   AuthResult `json:"spf"`
	DKIM  AuthResult `json:"dkim"`
	DMARC AuthResult `json:"dmarc"`
}

// DefaultAuthResults はすべて "none" のデフォルト認証結果を返す。
func DefaultAuthResults() AuthResults {
	return AuthResults{
		SPF:   AuthNone,
		DKIM:  AuthNone,
		DMARC: AuthNone,
	}
}

type Mail struct {
	MessageID     string
	EMLPath       string // MinIO上のパス（保存後に設定される）
	RawEML        []byte // 原本EMLバイト列
	ReceivedAt    time.Time
	FromAddress   string
	ToAddresses   []string
	Subject       string
	SizeBytes     int64
	HasAttachment bool
	RspamdScore   float64
	AuthResults   AuthResults
	Direction     Direction // メールの流通方向（smtp-gateway の設定から決定）
}

type StorageBackend string

const (
	StorageBackendS3  StorageBackend = "s3"
	StorageBackendSPO StorageBackend = "spo"
)

type MailAttachment struct {
	ID             string
	MessageID      string
	DownloadToken  string
	Filename       string
	ContentType    string
	SizeBytes      int64
	StorageBackend StorageBackend
	StoragePath    string
	DownloadMode   DownloadMode // ダウンロード認証方式
}
