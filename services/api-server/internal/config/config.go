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
	Approval           ApprovalConfig           `mapstructure:"approval"`
	Directory          DirectoryConfig          `mapstructure:"directory"`
	Gateway            GatewayConfig            `mapstructure:"gateway"`
	Settings           SettingsConfig           `mapstructure:"settings"`
	Log                LogConfig                `mapstructure:"log"`
}

// DirectoryConfig は「ユーザー情報（role・display_name 等）の真実の源」を選択する設定を保持する。
// Source は auth.sso_mode とは独立した軸である:
//   - Source           : ユーザー情報の真実の源とローカルログイン手段の選択（none | ldap | scim）
//   - Auth.SSOMode : OIDC（SSO）をどこまで使うか（disabled | optional | required）
//
// Source ごとの「ローカルログイン」手段:
//   - none : standalone（メール + bcrypt パスワード）
//   - ldap : LDAP bind 認証（同じ directory.ldap 接続設定を認証にも流用する）
//   - scim : ローカルログイン手段なし（SCIM はパスワード検証の仕組みを持たないため、
//     auth.sso_mode を optional/required にすることが必須。Load() 時にバリデーションする）
type DirectoryConfig struct {
	// Source はユーザー情報源の選択（省略時 none）。
	Source string     `mapstructure:"source"`
	LDAP   LDAPConfig `mapstructure:"ldap"`
}

const (
	DirectorySourceNone = "none"
	DirectorySourceLDAP = "ldap"
	DirectorySourceSCIM = "scim"
)

// EffectiveSource は Source の省略時デフォルト（none）を補完して返す。
func (d DirectoryConfig) EffectiveSource() string {
	if d.Source == "" {
		return DirectorySourceNone
	}
	return d.Source
}

// LDAPConfig は LDAP ディレクトリの接続・検索・マッピング設定を保持する。
// directory.source: ldap のとき、この接続設定は定期同期（Syncer）と bind 認証
// （auth.LDAPBindProvider）の両方から共用される。
type LDAPConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
	// TLS は接続の暗号化方式（none | starttls | ldaps。省略時 none）。
	TLS           string `mapstructure:"tls"`
	TLSSkipVerify bool   `mapstructure:"tls_skip_verify"`
	// BindDN / BindPassword はディレクトリ検索用のサービスアカウント資格情報。
	BindDN       string `mapstructure:"bind_dn"`
	BindPassword string `mapstructure:"bind_password"`
	// BaseDN はユーザー検索の起点となる DN。
	BaseDN string `mapstructure:"base_dn"`
	// UserFilter はユーザーを絞り込む LDAP 検索フィルタ。
	UserFilter string `mapstructure:"user_filter"`
	// Attributes はユーザーエントリの属性名マッピング。
	Attributes LDAPAttributesConfig `mapstructure:"attributes"`
	// GroupMappings はグループの DN（Attributes.Groups で得られる値と同じ形式）と
	// MailShield ロールのマッピング。
	GroupMappings GroupMappingsConfig `mapstructure:"group_mappings"`
	// SyncIntervalMinutes は定期同期の間隔（デフォルト 60 分）。
	SyncIntervalMinutes int `mapstructure:"sync_interval_minutes"`
	// SearchTimeoutSeconds は 1 回の LDAP 検索のタイムアウト（デフォルト 30 秒）。
	SearchTimeoutSeconds int `mapstructure:"search_timeout_seconds"`
	// PageSize は LDAP ページング検索の 1 ページあたり件数（デフォルト 500）。
	// AD 等のサーバー側件数上限（既定 1000 件）を超える規模のディレクトリでも
	// 全件を確実に取得するために使用する。
	PageSize int `mapstructure:"page_size"`
	// DeactivateMissingUsers を true にすると、同期結果に含まれなくなった
	// provisioned_by=ldap のユーザーを is_active=0 にする（アクセス即時剥奪）。
	// LDAP 検索が 0 件を返した場合は誤って全ユーザーを無効化しないよう何もしない。
	DeactivateMissingUsers bool `mapstructure:"deactivate_missing_users"`
	// MailboxProvisioning はユーザー・グループのディレクトリ構造からメールボックス割り当て
	// （member/owner/admin）を自動反映するための設定。
	MailboxProvisioning MailboxProvisioningConfig `mapstructure:"mailbox_provisioning"`
}

// MailboxProvisioningConfig はメールボックス割り当ての自動反映設定を保持する。
// Rules はルールのリストで、同じロールに対して複数のルールを書ける
// （例: 「個人メールボックスは自分の mail 属性から owner」と「共有メールボックスは
// memberOf から member」を同時に設定する）。全ルールの解決結果が合算される。
type MailboxProvisioningConfig struct {
	Rules []MailboxProvisioningRuleConfig `mapstructure:"rules"`
}

// MailboxProvisioningRuleConfig は 1 ルール分の解決方式を保持する。
// Role は member / owner / admin のいずれか。
// Method に応じて対応するフィールド群だけが使われる:
//   - user_attribute : ユーザー起点。ユーザー自身の属性から解決する有界パイプライン
//     （source_attribute → source_transform? → dereference?(最大1回) → target_attribute → target_transform?）。
//     source_attribute に mail（自分のメールアドレス属性）を指定すれば
//     「各ユーザー自身のメールアドレスをメールボックスとして登録し本人を割り当てる」
//     個人メールボックスの自動作成になる
//   - group_search   : グループ起点。メールボックスを表すグループを一括検索し、
//     そのグループの member_attr（DN 一覧）を対象ユーザーとみなす
//   - fixed          : 決め打ち。fixed_value に列挙したメールアドレスのユーザーへ、
//     この同期ソースが管理する全メールボックスに対して当該ロールを付与する
type MailboxProvisioningRuleConfig struct {
	Role   string `mapstructure:"role"`
	Method string `mapstructure:"method"`

	// ─── method: user_attribute ───
	// SourceAttribute はユーザーエントリのどの属性を読むか（例: memberOf）。複数値なら 1 件ずつ処理する。
	SourceAttribute string `mapstructure:"source_attribute"`
	// SourceTransform は属性値に適用する正規表現（任意）。マッチしない値はスキップされる
	// （memberOf に含まれる無関係なグループを取り除くフィルタを兼ねる）。
	SourceTransform string `mapstructure:"source_transform"`
	// Dereference は前段の値を使った再検索（任意・最大1回）。
	Dereference MailboxDereferenceConfig `mapstructure:"dereference"`
	// TargetAttribute は dereference 結果のエントリから読む属性（dereference 使用時は必須）。
	TargetAttribute string `mapstructure:"target_attribute"`
	// TargetTransform は最終値に適用する正規表現（任意）。
	TargetTransform string `mapstructure:"target_transform"`
	// MailboxDomain が空でなく、最終値に "@" が含まれない場合、"値@MailboxDomain" を
	// メールボックスアドレスとして組み立てる。
	MailboxDomain string `mapstructure:"mailbox_domain"`

	// ─── method: group_search ───
	// BaseDN / Filter はメールボックスを表すグループの検索条件。
	BaseDN string `mapstructure:"base_dn"`
	Filter string `mapstructure:"filter"`
	// MemberAttr はグループエントリのメンバー（ユーザー DN 一覧）を表す属性名（例: member）。
	MemberAttr string `mapstructure:"member_attr"`
	// group_search でも TargetAttribute / TargetTransform / MailboxDomain を使い、
	// グループエントリ自身からメールボックスアドレスを取り出す（例: target_attribute: mail）。

	// ─── method: fixed ───
	// FixedValue はカンマまたはセミコロン区切りのユーザーメールアドレス一覧。
	FixedValue string `mapstructure:"fixed_value"`
}

// MailboxDereferenceConfig は user_attribute の再検索（1回まで）の設定。
type MailboxDereferenceConfig struct {
	BaseDN string `mapstructure:"base_dn"`
	// Filter は "{value}" プレースホルダを含む LDAP フィルタ。
	// プレースホルダには前段の値が LDAP エスケープされて埋め込まれる（エスケープは無効化できない）。
	Filter string `mapstructure:"filter"`
}

// LDAPAttributesConfig はユーザーエントリから読み取る属性名。
type LDAPAttributesConfig struct {
	Email       string `mapstructure:"email"`
	DisplayName string `mapstructure:"display_name"`
	// Groups はグループ所属を表す属性名（Active Directory の memberOf 等）。
	Groups string `mapstructure:"groups"`
}

// ApprovalConfig は承認フローの設定を保持する。
type ApprovalConfig struct {
	// ExpiryHours は承認依頼の有効期限（デフォルト 72 時間）。
	ExpiryHours int `mapstructure:"expiry_hours"`
	// GlobalApproverEmail は承認者が解決できなかった場合のフォールバック承認者メールアドレス。
	GlobalApproverEmail string `mapstructure:"global_approver_email"`
	// BaseURL は承認画面 URL の生成に使用するベース URL。
	BaseURL string `mapstructure:"base_url"`

	Notification ApprovalNotificationConfig `mapstructure:"notification"`
}

// ApprovalNotificationConfig は承認依頼・結果通知メールの設定を保持する。
type ApprovalNotificationConfig struct {
	FromAddress string `mapstructure:"from_address"`
	FromName    string `mapstructure:"from_name"`

	// 承認依頼通知（承認者向け）
	RequestEnabled         bool   `mapstructure:"request_enabled"`
	RequestSubjectTemplate string `mapstructure:"request_subject_template"`
	RequestBodyTemplate    string `mapstructure:"request_body_template"`

	// 承認結果通知（送信者向け・内部ユーザーのみ）
	ResultEnabled           bool   `mapstructure:"result_enabled"`
	ApprovedSubjectTemplate string `mapstructure:"approved_subject_template"`
	ApprovedBodyTemplate    string `mapstructure:"approved_body_template"`
	RejectedSubjectTemplate string `mapstructure:"rejected_subject_template"`
	RejectedBodyTemplate    string `mapstructure:"rejected_body_template"`
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
	FromAddress string `mapstructure:"from_address"`
	SMTPHost    string `mapstructure:"smtp_host"`
	SMTPPort    int    `mapstructure:"smtp_port"`
	StartTLS    bool   `mapstructure:"starttls"`
	AuthUser    string `mapstructure:"auth_user"`
	AuthPass    string `mapstructure:"auth_pass"`
	// ReinjectHost / ReinjectPort は隔離解放時に処理済み EML を再インジェクトする Postfix の接続先。
	// api-server.yaml に未設定の場合は settings.smtp_gateway_config_file の reinject 設定を継承する。
	ReinjectHost string `mapstructure:"reinject_host"`
	ReinjectPort int    `mapstructure:"reinject_port"`
}

// SettingsConfig は api-server が参照する外部ファイルパスの設定を保持する。
type SettingsConfig struct {
	// PolicyFile は smtp-gateway が読む policy.yaml のパス（GUI 編集用）。
	PolicyFile string `mapstructure:"policy_file"`
	// SmtpGatewayConfigFile は smtp-gateway の mailshield.yaml のパス。
	// notification.reinject_host/port が未設定の場合、このファイルから reinject 設定を継承する。
	SmtpGatewayConfigFile string `mapstructure:"smtp_gateway_config_file"`
}

// StorageConfig はオブジェクトストレージ（MinIO/S3）の接続設定を保持する。
type StorageConfig struct {
	Endpoint            string `mapstructure:"endpoint"`
	PublicEndpoint      string `mapstructure:"public_endpoint"` // ブラウザからアクセスする際のFQDN（空の場合はEndpointを使用）
	AccessKey           string `mapstructure:"access_key"`
	SecretKey           string `mapstructure:"secret_key"`
	BucketEML           string `mapstructure:"bucket_eml"`
	BucketAttachments   string `mapstructure:"bucket_attachments"`
	UseSSL              bool   `mapstructure:"use_ssl"`
	PublicUseSSL        bool   `mapstructure:"public_use_ssl"` // public_endpoint へのアクセスに SSL を使うか
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
// AuthConfig は認証全体の設定を保持する。
//
// SSOMode は OIDC（SSO）の扱いを決める独立した軸である（disabled | optional | required）:
//   - disabled（省略時）: OIDC を使わない。directory.source が決めるローカルログイン手段のみ
//   - optional          : ローカルログイン手段 + OIDC の両方を提示する
//   - required          : OIDC のみ。ローカルログイン手段（standalone/LDAP bind）は無効化する
//
// directory.source（DirectoryConfig.Source）と組み合わせて、実際に有効なログイン手段が決まる。
type AuthConfig struct {
	SSOMode       string              `mapstructure:"sso_mode"`
	Providers     AuthProvidersConfig `mapstructure:"providers"`
	GroupMappings GroupMappingsConfig `mapstructure:"group_mappings"`
	Session       SessionConfig       `mapstructure:"session"`
	RateLimit     RateLimitConfig     `mapstructure:"rate_limit"`
}

// RateLimitConfig は認証系エンドポイント（ログイン・パスワードリセット・OTP 発行）の
// クライアント IP 単位レート制限の設定を保持する。
// ブルートフォース攻撃と通知メール送信の濫用を防ぐ。
type RateLimitConfig struct {
	// Enabled は省略時 true（明示的に false を指定した場合のみ無効化）。
	Enabled *bool `mapstructure:"enabled"`
	// MaxAttempts はウィンドウあたりの許容リクエスト数（省略時 10）。
	MaxAttempts int `mapstructure:"max_attempts"`
	// WindowSeconds はウィンドウ長（省略時 300 秒）。
	WindowSeconds int `mapstructure:"window_seconds"`
}

// EffectiveEnabled は Enabled の省略時デフォルト（true）を補完して返す。
func (r RateLimitConfig) EffectiveEnabled() bool {
	if r.Enabled == nil {
		return true
	}
	return *r.Enabled
}

// EffectiveMaxAttempts は MaxAttempts の省略時デフォルト（10）を補完して返す。
func (r RateLimitConfig) EffectiveMaxAttempts() int {
	if r.MaxAttempts <= 0 {
		return 10
	}
	return r.MaxAttempts
}

// EffectiveWindowSeconds は WindowSeconds の省略時デフォルト（300）を補完して返す。
func (r RateLimitConfig) EffectiveWindowSeconds() int {
	if r.WindowSeconds <= 0 {
		return 300
	}
	return r.WindowSeconds
}

const (
	SSOModeDisabled = "disabled"
	SSOModeOptional = "optional"
	SSOModeRequired = "required"
)

// EffectiveSSOMode は SSOMode の省略時デフォルト（disabled）を補完して返す。
func (a AuthConfig) EffectiveSSOMode() string {
	if a.SSOMode == "" {
		return SSOModeDisabled
	}
	return a.SSOMode
}

// LocalLoginAllowed はローカルログイン手段（standalone または LDAP bind）を
// 提示してよいかを返す（sso_mode: required でなければ true）。
func (a AuthConfig) LocalLoginAllowed() bool {
	return a.EffectiveSSOMode() != SSOModeRequired
}

// SSOAllowed は OIDC を提示してよいかを返す（sso_mode が disabled でなければ true）。
func (a AuthConfig) SSOAllowed() bool {
	return a.EffectiveSSOMode() != SSOModeDisabled
}

// AuthProvidersConfig は認証プロバイダーの接続設定を保持する。
// 各プロバイダーを実際に使うかどうかは Enabled フラグではなく、
// directory.source（ローカルログイン手段の選択）と auth.sso_mode（OIDC の扱い）から導出する。
type AuthProvidersConfig struct {
	OIDC OIDCConfig `mapstructure:"oidc"`
}

// OIDCConfig はOIDCプロバイダーの接続設定を保持する。
type OIDCConfig struct {
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
		"database.host":                     "DB_HOST",
		"database.port":                     "DB_PORT",
		"database.name":                     "DB_NAME",
		"database.user":                     "DB_USER",
		"database.password":                 "DB_PASSWORD",
		"redis.host":                        "REDIS_HOST",
		"redis.port":                        "REDIS_PORT",
		"storage.endpoint":                  "MINIO_ENDPOINT",
		"storage.access_key":                "MINIO_ACCESS_KEY",
		"storage.secret_key":                "MINIO_SECRET_KEY",
		"storage.use_ssl":                   "MINIO_USE_SSL",
		"notification.auth_pass":            "NOTIFICATION_AUTH_PASS",
		"auth.providers.oidc.client_secret": "OIDC_CLIENT_SECRET",
		"directory.ldap.bind_password":      "LDAP_BIND_PASSWORD",
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

	// notification.reinject_host が未設定の場合、mailshield.yaml の reinject 設定を継承する。
	if cfg.Notification.ReinjectHost == "" && cfg.Settings.SmtpGatewayConfigFile != "" {
		if err := inheritReinjectFromGateway(&cfg); err != nil {
			// 継承失敗は警告のみ（api-server.yaml 側で明示設定されていれば問題ない）
			fmt.Printf("warn: smtp-gateway 設定からの reinject 継承失敗: %v\n", err)
		}
	}

	if err := validateAuthDirectory(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// validateAuthDirectory は directory.source と auth.sso_mode の組み合わせを検証する。
// 起動時に検出できる設定不正はここで弾き、実行中に「誰もログインできない」状態や
// 静かなフォールバックを起こさないようにする。
func validateAuthDirectory(cfg *Config) error {
	switch cfg.Directory.EffectiveSource() {
	case DirectorySourceNone, DirectorySourceLDAP, DirectorySourceSCIM:
	default:
		return fmt.Errorf("directory.source が不正です: %q（none | ldap | scim）", cfg.Directory.Source)
	}

	switch cfg.Auth.EffectiveSSOMode() {
	case SSOModeDisabled, SSOModeOptional, SSOModeRequired:
	default:
		return fmt.Errorf("auth.sso_mode が不正です: %q（disabled | optional | required）", cfg.Auth.SSOMode)
	}

	// SCIM はパスワード検証の仕組みを持たないため、ローカルログイン手段が存在しない。
	// SSO（OIDC）を使わない設定はログイン手段が一つも無い詰みの構成になるため起動時に弾く。
	if cfg.Directory.EffectiveSource() == DirectorySourceSCIM && !cfg.Auth.SSOAllowed() {
		return fmt.Errorf("directory.source: scim を使う場合、auth.sso_mode を optional または required に設定してください（SCIM は認証手段を持たないため）")
	}

	// sso_mode: required でローカルログイン手段が無効化されるとき、OIDC の接続設定が
	// 無ければ誰もログインできない。
	if cfg.Auth.EffectiveSSOMode() == SSOModeRequired && cfg.Auth.Providers.OIDC.Issuer == "" {
		return fmt.Errorf("auth.sso_mode: required の場合、auth.providers.oidc.issuer 等の OIDC 接続設定が必要です")
	}

	return nil
}

// inheritReinjectFromGateway は mailshield.yaml の reinject.host/port を読み込んで
// cfg.Notification.ReinjectHost/Port に設定する。
// mailshield.yaml 側の reinject 設定が唯一の正（SSOT）となる。
func inheritReinjectFromGateway(cfg *Config) error {
	// mailshield.default.yaml → mailshield.yaml の順にマージして最終値を得る
	v := viper.New()
	v.SetConfigType("yaml")

	defaultFile := strings.TrimSuffix(cfg.Settings.SmtpGatewayConfigFile, ".yaml") + ".default.yaml"
	v.SetConfigFile(defaultFile)
	_ = v.ReadInConfig() // default がなければ無視（エラーは握りつぶす）

	mv := viper.New()
	mv.SetConfigFile(cfg.Settings.SmtpGatewayConfigFile)
	mv.SetConfigType("yaml")
	if err := mv.ReadInConfig(); err != nil {
		return fmt.Errorf("mailshield.yaml 読み込み失敗 (%s): %w", cfg.Settings.SmtpGatewayConfigFile, err)
	}
	_ = v.MergeConfigMap(mv.AllSettings())

	host := v.GetString("reinject.host")
	port := v.GetInt("reinject.port")
	if host == "" {
		return fmt.Errorf("mailshield.yaml に reinject.host が設定されていません")
	}
	cfg.Notification.ReinjectHost = host
	if port != 0 {
		cfg.Notification.ReinjectPort = port
	}
	return nil
}
