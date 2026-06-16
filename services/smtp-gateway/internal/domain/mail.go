// Package domain はドメイン型とインターフェースを定義する。
// このパッケージは外部ライブラリに依存しない。
package domain

import "time"

// Direction はメールの流通方向を表す。
// smtp-gateway の設定から決定される。
type Direction string

const (
	DirectionInbound  Direction = "inbound"  // 外部 → 内部
	DirectionOutbound Direction = "outbound" // 内部 → 外部
	DirectionInternal Direction = "internal" // 内部 → 内部
)

// DownloadMode は添付ファイルのダウンロード認証方式を表す。
type DownloadMode string

const (
	DownloadModeSimple DownloadMode = "simple" // 認証なし（トークンのみ）
	DownloadModeOTP    DownloadMode = "otp"    // メールアドレス OTP 認証
	DownloadModeAuth   DownloadMode = "auth"   // MailShield ログイン必須
)

// AuthResult は SPF/DKIM/DMARC の検証結果を表す。
type AuthResult string

const (
	AuthPass AuthResult = "pass"
	AuthFail AuthResult = "fail"
	AuthNone AuthResult = "none"
)

// MessageStatus はメールの処理状態を表す。
type MessageStatus string

const (
	StatusReceived        MessageStatus = "received"
	StatusProcessing      MessageStatus = "processing"
	StatusDelivered       MessageStatus = "delivered"
	StatusQuarantined     MessageStatus = "quarantined"
	StatusRejected        MessageStatus = "rejected"
	StatusApprovalPending MessageStatus = "approval_pending"
)

// AuthResults は認証結果のセットを表す。
type AuthResults struct {
	SPF   AuthResult
	DKIM  AuthResult
	DMARC AuthResult
}

// DefaultAuthResults はすべて "none" のデフォルト認証結果を返す。
func DefaultAuthResults() AuthResults {
	return AuthResults{
		SPF:   AuthNone,
		DKIM:  AuthNone,
		DMARC: AuthNone,
	}
}

// Mail はシステム内で流通するメールの内部表現である。
// EMLのバイト列とパース済みメタデータの両方を保持する。
type Mail struct {
	MessageID     string
	EMLPath       string    // MinIO上のパス（保存後に設定される）
	RawEML        []byte    // 原本EMLバイト列
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

// StorageBackend は添付ファイルの保存先を表す。
type StorageBackend string

const (
	StorageBackendS3  StorageBackend = "s3"
	StorageBackendSPO StorageBackend = "spo"
)

// MailAttachment は分離した添付ファイルのメタデータを表す。
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
