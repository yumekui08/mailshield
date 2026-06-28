// Package config は mailshield.yaml と環境変数から設定を読み込む。
// 環境変数は YAML の値を上書きする。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server                  ServerConfig
	Storage                 StorageConfig
	Database                DatabaseConfig
	Queue                   QueueConfig
	// Workers は全ルートで共有するワーカーのグローバル設定（Lua ディレクトリ等）。
	// ルートごとの有効・無効・順序は routes[].workers.inspect / transform で制御する。
	Workers                 WorkersGlobal
	Routes                  []RouteConfig
	Log                     LogConfig
	AttachmentDownload      AttachmentDownloadConfig      `mapstructure:"attachment_download"`
	Notification            NotificationConfig            `mapstructure:"notification"`
	QuarantineNotification  QuarantineNotificationConfig  `mapstructure:"quarantine_notification"`
	Approval                ApprovalConfig                `mapstructure:"approval"`
}

type WorkersGlobal struct {
	// WorkersDir は Lua ワーカースクリプトのルートディレクトリ。
	WorkersDir string `mapstructure:"workers_dir"`
	// WorkerConfigDir はワーカー固有設定ファイル（YAML）を置くディレクトリ。
	WorkerConfigDir string `mapstructure:"worker_config_dir"`
}

type ApprovalConfig struct {
	// ExpiryHours は承認依頼の有効期限（デフォルト 72 時間）。
	ExpiryHours int `mapstructure:"expiry_hours"`
	// GlobalApproverEmail は承認者が解決できなかった場合のフォールバック承認者メールアドレス。
	GlobalApproverEmail string `mapstructure:"global_approver_email"`
}

type NotificationConfig struct {
	SMTPHost    string `mapstructure:"smtp_host"`
	SMTPPort    int    `mapstructure:"smtp_port"`
	FromAddress string `mapstructure:"from_address"`
}

type QuarantineNotificationConfig struct {
	// Enabled を false にすると通知メールを送信しない。
	Enabled   bool   `mapstructure:"enabled"`
	// UIBaseURL は通知メール内の「確認はこちら」リンクのベース URL。
	UIBaseURL string `mapstructure:"ui_base_url"`
}

type AttachmentDownloadConfig struct {
	// Flows はメール方向とダウンロードモードのマッピング。
	// 最初にマッチしたルールが適用される。
	Flows []AttachmentDownloadFlow `mapstructure:"flows"`
}

type AttachmentDownloadFlow struct {
	// Match はメールの方向（inbound / outbound / internal）。
	Match string `mapstructure:"match"`
	// Mode はダウンロード認証方式（simple / otp / auth）。
	Mode string `mapstructure:"mode"`
	// AllowedRoles は auth モード時にダウンロードを許可するメールボックスロール。
	// 空の場合はすべてのロールを許可する。
	// 例: ["member", "owner", "admin"]
	AllowedRoles []string `mapstructure:"allowed_roles"`
}

// DownloadModeFor は指定した方向に対応するダウンロードモードとロールを返す。
// マッチするルールがない場合はデフォルト値（simple, 全ロール許可）を返す。
func (c *AttachmentDownloadConfig) DownloadModeFor(direction string) (mode string, allowedRoles []string) {
	for _, flow := range c.Flows {
		if flow.Match == direction {
			return flow.Mode, flow.AllowedRoles
		}
	}
	return "simple", nil
}

type LogConfig struct {
	// Level はログレベル（debug / info / warn / error）。
	Level string `mapstructure:"level"`
	// Format は出力フォーマット（json / text）。
	Format string `mapstructure:"format"`
	// Output は出力先（stdout / syslog）。
	Output string `mapstructure:"output"`
	// SyslogTag は syslog 出力時のタグ。
	SyslogTag string `mapstructure:"syslog_tag"`
}

type ServerConfig struct {
	// SMTP サーバー設定
	SMTPPort              int      `mapstructure:"smtp_port"`
	SMTPHostname          string   `mapstructure:"smtp_hostname"`
	MaxMessageSizeMB      int      `mapstructure:"max_message_size_mb"`
	MaxRecipients         int      `mapstructure:"smtp_max_recipients"`
	ReadTimeoutSeconds    int      `mapstructure:"smtp_read_timeout_seconds"`
	WriteTimeoutSeconds   int      `mapstructure:"smtp_write_timeout_seconds"`
	HandlerTimeoutSeconds int      `mapstructure:"handler_timeout_seconds"`
	// ヘルスチェック・シャットダウン
	HealthPort             int `mapstructure:"health_port"`
	ShutdownTimeoutSeconds int `mapstructure:"shutdown_timeout_seconds"`
	TrustedSources []string `mapstructure:"trusted_sources"`
}

type StorageConfig struct {
	// Backend はストレージバックエンドの種別（minio | s3 | filesystem）。
	Backend           string `mapstructure:"backend"`
	Endpoint          string `mapstructure:"endpoint"`
	AccessKey         string `mapstructure:"access_key"`
	SecretKey         string `mapstructure:"secret_key"`
	BucketEML         string `mapstructure:"bucket_eml"`
	BucketAttachments string `mapstructure:"bucket_attachments"`
	UseSSL            bool   `mapstructure:"use_ssl"`
	// LocalDir はfallbackモード（backend: filesystem）でのEML保存先ディレクトリ。
	LocalDir      string `mapstructure:"local_dir"`
	// PublicBaseURL は filesystem モードで GetPresignedURL が返す URL のベース。
	// 空の場合 GetPresignedURL はエラーを返す。
	PublicBaseURL string `mapstructure:"public_base_url"`
}

type DatabaseConfig struct {
	Driver   string `mapstructure:"driver"`
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Name     string `mapstructure:"name"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	// 接続プール設定
	MaxOpenConns           int `mapstructure:"max_open_conns"`
	MaxIdleConns           int `mapstructure:"max_idle_conns"`
	ConnMaxLifetimeMinutes int `mapstructure:"conn_max_lifetime_minutes"`
}

type QueueConfig struct {
	Backend  string `mapstructure:"backend"`
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
}

// RouteConfig は1つのルート定義を保持する。
// MAIL FROM / RCPT TO に対して正規表現マッチを行い、最初にマッチしたルートが適用される。
type RouteConfig struct {
	Name      string           `mapstructure:"name"`
	// Direction はこのルートで処理するメールの方向（inbound / outbound / internal）。
	Direction string           `mapstructure:"direction"`
	Match     RouteMatchConfig `mapstructure:"match"`
	Workers   WorkersConfig    `mapstructure:"workers"`
	Policy    PolicyConfig     `mapstructure:"policy"`
}

// RouteMatchConfig はルートのマッチ条件を保持する。
// From / To は両方省略可能（省略すると全マッチ）。
// From と To を両方指定した場合は AND 条件になる。
type RouteMatchConfig struct {
	// From は MAIL FROM アドレスに対する正規表現。空の場合は全マッチ。
	From string `mapstructure:"from"`
	// To は RCPT TO アドレスに対する正規表現。空の場合は全マッチ。
	To string `mapstructure:"to"`
	// ToMatch は To 正規表現の評価方式。"any"（デフォルト）または "all"。
	// any: RCPT TO のいずれか1つがマッチすればルールを適用
	// all: RCPT TO の全員がマッチしたときのみルールを適用
	ToMatch string `mapstructure:"to_match"`
}

type WorkersConfig struct {
	// Inspect は検査ワーカーの有効・無効とタイムアウトの設定。
	// ワーカーの実装（Lua スクリプトや接続先）は workers.worker_config_dir 配下の YAML で設定する。
	Inspect   []InspectWorkerConfig  `mapstructure:"inspect"`
	Transform []TransformWorkerConfig `mapstructure:"transform"`
}

type InspectWorkerConfig struct {
	Name           string `mapstructure:"name"`
	Enabled        bool   `mapstructure:"enabled"`
	TimeoutSeconds int    `mapstructure:"timeout_seconds"`
}

type TransformWorkerConfig struct {
	Name    string `mapstructure:"name"`
	Enabled bool   `mapstructure:"enabled"`
	Order   int    `mapstructure:"order"`
}

type PolicyConfig struct {
	RulesFile string `mapstructure:"rules_file"`
	LuaFile   string `mapstructure:"lua_file"`
}

// Load は設定ファイルと環境変数から Config を読み込む。
//
// configFile（例: config/mailshield.yaml）と同じディレクトリに
// configFile.default.yaml（例: config/mailshield.default.yaml）が存在する場合、
// 先にデフォルト設定を読み込み、その後 configFile の値で上書きする。
// 環境変数は YAML より優先される。
func Load(configFile string) (*Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	bindEnvs := map[string]string{
		"database.host":          "DB_HOST",
		"database.port":          "DB_PORT",
		"database.name":          "DB_NAME",
		"database.user":          "DB_USER",
		"database.password":      "DB_PASSWORD",
		"storage.endpoint":       "MINIO_ENDPOINT",
		"storage.use_ssl":        "MINIO_USE_SSL",
		"queue.host":             "RABBITMQ_HOST",
		"queue.port":             "RABBITMQ_PORT",
		"queue.user":             "RABBITMQ_USER",
		"queue.password":         "RABBITMQ_PASSWORD",
		"notification.smtp_host": "MAILSHIELD_NOTIFICATION_SMTP_HOST",
		"notification.smtp_port": "MAILSHIELD_NOTIFICATION_SMTP_PORT",
	}
	for yamlKey, envKey := range bindEnvs {
		if err := v.BindEnv(yamlKey, envKey); err != nil {
			return nil, fmt.Errorf("env バインド失敗 %s: %w", envKey, err)
		}
	}
	if err := v.BindEnv("storage.access_key", "MINIO_ACCESS_KEY"); err != nil {
		return nil, fmt.Errorf("env バインド失敗 MINIO_ACCESS_KEY: %w", err)
	}
	if err := v.BindEnv("storage.secret_key", "MINIO_SECRET_KEY"); err != nil {
		return nil, fmt.Errorf("env バインド失敗 MINIO_SECRET_KEY: %w", err)
	}

	// <base>.default.yaml が存在すれば先にロードし、configFile で上書きする
	ext := filepath.Ext(configFile)
	defaultFile := strings.TrimSuffix(configFile, ext) + ".default" + ext
	if _, err := os.Stat(defaultFile); err == nil {
		v.SetConfigFile(defaultFile)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("デフォルト設定ファイル読み込み失敗 (%s): %w", defaultFile, err)
		}
		if _, err := os.Stat(configFile); err == nil {
			v.SetConfigFile(configFile)
			if err := v.MergeInConfig(); err != nil {
				return nil, fmt.Errorf("設定ファイルのマージ失敗 (%s): %w", configFile, err)
			}
		}
	} else {
		v.SetConfigFile(configFile)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("設定ファイル読み込み失敗: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("設定のデシリアライズ失敗: %w", err)
	}

	return &cfg, nil
}
