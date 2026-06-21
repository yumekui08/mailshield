// Package config は api-server.yaml と環境変数から設定を読み込む。
// 環境変数は YAML の値を上書きする（viper の AutomaticEnv を使用）。
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config はサービス全体の設定を保持する。
type Config struct {
	Server             ServerConfig             `mapstructure:"server"`
	Database           DatabaseConfig           `mapstructure:"database"`
	Redis              RedisConfig              `mapstructure:"redis"`
	Auth               AuthConfig               `mapstructure:"auth"`
	Storage            StorageConfig            `mapstructure:"storage"`
	MailboxPolicy      MailboxPolicyConfig      `mapstructure:"mailbox_policy"`
	AttachmentDownload AttachmentDownloadConfig `mapstructure:"attachment_download"`
	Notification       NotificationConfig       `mapstructure:"notification"`
	Gateway            GatewayConfig            `mapstructure:"gateway"`
	Log                LogConfig                `mapstructure:"log"`
}

// GatewayConfig は smtp-gateway への内部接続設定を保持する。
type GatewayConfig struct {
	// URL は smtp-gateway のヘルスチェックポート（デフォルト: http://smtp-gateway:8080）。
	// シミュレーション API がここに POST /simulate をプロキシする。
	URL string `mapstructure:"url"`
}

// AttachmentDownloadConfig は添付ファイルダウンロードのアクセス制御設定を保持する。
type AttachmentDownloadConfig struct {
	// AuthMode は mode=auth のときに適用するロール制限を保持する。
	AuthMode AuthModeConfig `mapstructure:"auth_mode"`
}

// AuthModeConfig は mode=auth 時のダウンロード許可ロールを保持する。
type AuthModeConfig struct {
	// AllowedRoles はダウンロードを許可するメールボックスロール。
	// 空の場合はすべてのロール（member/owner/admin）を許可する。
	AllowedRoles []string `mapstructure:"allowed_roles"`
}

// AllowedRoleSet は許可ロールをセットとして返す。空の場合は全ロールを許可。
func (c *AuthModeConfig) AllowedRoleSet() map[string]bool {
	if len(c.AllowedRoles) == 0 {
		return map[string]bool{"member": true, "owner": true, "admin": true}
	}
	set := make(map[string]bool, len(c.AllowedRoles))
	for _, r := range c.AllowedRoles {
		set[r] = true
	}
	return set
}

// NotificationConfig はシステムが送信するメール（通知・OTP・隔離解放）の共通 SMTP 設定を保持する。
type NotificationConfig struct {
	// FromAddress はシステムメールの送信元アドレス。
	FromAddress  string `mapstructure:"from_address"`
	SMTPHost     string `mapstructure:"smtp_host"`
	SMTPPort     int    `mapstructure:"smtp_port"`
	StartTLS     bool   `mapstructure:"starttls"`
	AuthUser     string `mapstructure:"auth_user"`
	AuthPass     string `mapstructure:"auth_pass"`
	// ReinjectHost / ReinjectPort は隔離解放時に処理済み EML を再インジェクトする Postfix の接続先。
	// Postfix の content_filter なしのポートを指定する（デフォルト: postfix:10025）。
	ReinjectHost string `mapstructure:"reinject_host"`
	ReinjectPort int    `mapstructure:"reinject_port"`
}

// StorageConfig はオブジェクトストレージ（MinIO/S3）の接続設定を保持する。
type StorageConfig struct {
	Endpoint            string `mapstructure:"endpoint"`
	PublicEndpoint      string `mapstructure:"public_endpoint"`       // ブラウザからアクセスする際のFQDN（空の場合はEndpointを使用）
	AccessKey           string `mapstructure:"access_key"`
	SecretKey           string `mapstructure:"secret_key"`
	BucketEML           string `mapstructure:"bucket_eml"`
	BucketAttachments   string `mapstructure:"bucket_attachments"`
	UseSSL              bool   `mapstructure:"use_ssl"`
	PublicUseSSL        bool   `mapstructure:"public_use_ssl"`        // public_endpoint へのアクセスに SSL を使うか
	PresignedURLExpiryH int    `mapstructure:"presigned_url_expiry_hours"`
}

// MailboxPolicyConfig はメールボックスの可視性・解放権限ポリシーを保持する。
type MailboxPolicyConfig struct {
	InboundQuarantine  DirectionPolicyConfig `mapstructure:"inbound_quarantine"`
	OutboundQuarantine DirectionPolicyConfig `mapstructure:"outbound_quarantine"`
}

// DirectionPolicyConfig は送受信方向ごとの可視性・解放権限を保持する。
// VisibleTo / ReleaseBy の値は mailbox_assignments.role（member/owner/admin）を指定する。
type DirectionPolicyConfig struct {
	VisibleTo []string `mapstructure:"visible_to"`
	ReleaseBy []string `mapstructure:"release_by"`
}

// ServerConfig はHTTPサーバーの設定を保持する。
type ServerConfig struct {
	Port                   int    `mapstructure:"port"`
	ShutdownTimeoutSeconds int    `mapstructure:"shutdown_timeout_seconds"`
	FrontendURL            string `mapstructure:"frontend_url"` // コールバック後のリダイレクト先ベース
}

// DatabaseConfig はMariaDB接続の設定を保持する。
type DatabaseConfig struct {
	Driver                 string `mapstructure:"driver"`
	Host                   string `mapstructure:"host"`
	Port                   int    `mapstructure:"port"`
	Name                   string `mapstructure:"name"`
	User                   string `mapstructure:"user"`
	Password               string `mapstructure:"password"`
	MaxOpenConns           int    `mapstructure:"max_open_conns"`
	MaxIdleConns           int    `mapstructure:"max_idle_conns"`
	ConnMaxLifetimeMinutes int    `mapstructure:"conn_max_lifetime_minutes"`
}

// RedisConfig はRedis接続の設定を保持する。
type RedisConfig struct {
	// Backend はキャッシュバックエンドの種別（redis | mariadb）。
	// mariadb を選ぶと Redis 不要でセッション/OTP/パスワードリセットを MariaDB に保存する。
	Backend  string `mapstructure:"backend"`
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	DB       int    `mapstructure:"db"`
	Password string `mapstructure:"password"`
}

// AuthConfig は認証・認可の設定を保持する。
type AuthConfig struct {
	Providers     AuthProvidersConfig `mapstructure:"providers"`
	GroupMappings GroupMappingsConfig `mapstructure:"group_mappings"`
	Session       SessionConfig       `mapstructure:"session"`
}

// AuthProvidersConfig は有効化する認証プロバイダーの設定を保持する。
type AuthProvidersConfig struct {
	Standalone StandaloneConfig `mapstructure:"standalone"`
	OIDC       OIDCConfig       `mapstructure:"oidc"`
}

// StandaloneConfig はスタンドアロン認証（メール+パスワード）の設定を保持する。
type StandaloneConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

// OIDCConfig はOIDCプロバイダーの設定を保持する。
type OIDCConfig struct {
	Enabled        bool     `mapstructure:"enabled"`
	Issuer         string   `mapstructure:"issuer"`
	ExternalIssuer string   `mapstructure:"external_issuer"` // ブラウザ向けURLのベース（空の場合はIssuerと同じ）
	ClientID       string   `mapstructure:"client_id"`
	ClientSecret   string   `mapstructure:"client_secret"`
	RedirectURI    string   `mapstructure:"redirect_uri"`
	Scopes         []string `mapstructure:"scopes"`
}

// GroupMappingsConfig はOIDCグループ名とRoleのマッピングを保持する。
type GroupMappingsConfig struct {
	Admin    string `mapstructure:"admin"`
	Operator string `mapstructure:"operator"`
	Viewer   string `mapstructure:"viewer"`
}

// SessionConfig はセッション管理の設定を保持する。
type SessionConfig struct {
	TTLMinutes   int    `mapstructure:"ttl_minutes"`
	CookieName   string `mapstructure:"cookie_name"`
	CookieSecure bool   `mapstructure:"cookie_secure"`
}

// LogConfig はロギングの設定を保持する。
type LogConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// Load は設定ファイルと環境変数から Config を読み込む。
// 環境変数のキーはアンダースコア区切りの大文字（例: DB_HOST）。
func Load(configFile string) (*Config, error) {
	v := viper.New()

	v.SetConfigFile(configFile)
	v.SetConfigType("yaml")

	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	// 環境変数のマッピング
	bindEnvs := map[string]string{
		"database.host":     "DB_HOST",
		"database.port":     "DB_PORT",
		"database.name":     "DB_NAME",
		"database.user":     "DB_USER",
		"database.password": "DB_PASSWORD",
		"redis.host":        "REDIS_HOST",
		"redis.port":        "REDIS_PORT",
		"storage.endpoint":   "MINIO_ENDPOINT",
		"storage.access_key": "MINIO_ACCESS_KEY",
		"storage.secret_key": "MINIO_SECRET_KEY",
		"storage.use_ssl":    "MINIO_USE_SSL",
		"notification.auth_pass": "NOTIFICATION_AUTH_PASS",
		"auth.providers.oidc.client_secret": "OIDC_CLIENT_SECRET",
	}
	for yamlKey, envKey := range bindEnvs {
		if err := v.BindEnv(yamlKey, envKey); err != nil {
			return nil, fmt.Errorf("env バインド失敗 %s: %w", envKey, err)
		}
	}

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("設定ファイル読み込み失敗: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("設定のデシリアライズ失敗: %w", err)
	}

	return &cfg, nil
}
