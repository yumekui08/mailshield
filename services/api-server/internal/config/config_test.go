package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "api-server.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("テスト用YAMLファイル書き込み失敗: %v", err)
	}
	return path
}

const validYAML = `
server:
  port: 8080
  shutdown_timeout_seconds: 30
database:
  driver: mariadb
  host: mariadb
  port: 3306
  name: mailshield
  user: mailshield
  password: secret
  max_open_conns: 10
  max_idle_conns: 5
  conn_max_lifetime_minutes: 5
redis:
  host: redis
  port: 6379
  db: 0
auth:
  sso_mode: optional
  providers:
    oidc:
      issuer: https://accounts.google.com
      client_id: my-client-id
      client_secret: my-client-secret
      redirect_uri: http://localhost:8080/api/v1/auth/callback
      scopes:
        - openid
        - email
        - profile
  group_mappings:
    admin: mailshield-admin
    operator: mailshield-operator
    viewer: mailshield-viewer
  session:
    ttl_minutes: 60
    cookie_name: mailshield_session
    cookie_secure: false
log:
  level: info
  format: json
`

func TestLoad_ValidYAML(t *testing.T) {
	path := writeYAML(t, validYAML)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("有効なYAMLの読み込み失敗: %v", err)
	}

	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port 期待: 8080, 実際: %d", cfg.Server.Port)
	}
	if cfg.Server.ShutdownTimeoutSeconds != 30 {
		t.Errorf("Server.ShutdownTimeoutSeconds 期待: 30, 実際: %d", cfg.Server.ShutdownTimeoutSeconds)
	}
	if cfg.Database.Driver != "mariadb" {
		t.Errorf("Database.Driver 期待: mariadb, 実際: %s", cfg.Database.Driver)
	}
	if cfg.Database.Host != "mariadb" {
		t.Errorf("Database.Host 期待: mariadb, 実際: %s", cfg.Database.Host)
	}
	if cfg.Database.Port != 3306 {
		t.Errorf("Database.Port 期待: 3306, 実際: %d", cfg.Database.Port)
	}
	if cfg.Database.Name != "mailshield" {
		t.Errorf("Database.Name 期待: mailshield, 実際: %s", cfg.Database.Name)
	}
	if cfg.Database.User != "mailshield" {
		t.Errorf("Database.User 期待: mailshield, 実際: %s", cfg.Database.User)
	}
	if cfg.Database.Password != "secret" {
		t.Errorf("Database.Password 期待: secret, 実際: %s", cfg.Database.Password)
	}
	if cfg.Redis.Host != "redis" {
		t.Errorf("Redis.Host 期待: redis, 実際: %s", cfg.Redis.Host)
	}
	if cfg.Redis.Port != 6379 {
		t.Errorf("Redis.Port 期待: 6379, 実際: %d", cfg.Redis.Port)
	}
	if cfg.Auth.Providers.OIDC.Issuer != "https://accounts.google.com" {
		t.Errorf("Auth.OIDC.Issuer 期待: https://accounts.google.com, 実際: %s", cfg.Auth.Providers.OIDC.Issuer)
	}
	if cfg.Auth.Providers.OIDC.ClientID != "my-client-id" {
		t.Errorf("Auth.OIDC.ClientID 期待: my-client-id, 実際: %s", cfg.Auth.Providers.OIDC.ClientID)
	}
	if cfg.Auth.Providers.OIDC.ClientSecret != "my-client-secret" {
		t.Errorf("Auth.OIDC.ClientSecret 期待: my-client-secret, 実際: %s", cfg.Auth.Providers.OIDC.ClientSecret)
	}
	if cfg.Auth.Session.TTLMinutes != 60 {
		t.Errorf("Auth.Session.TTLMinutes 期待: 60, 実際: %d", cfg.Auth.Session.TTLMinutes)
	}
	if cfg.Auth.Session.CookieName != "mailshield_session" {
		t.Errorf("Auth.Session.CookieName 期待: mailshield_session, 実際: %s", cfg.Auth.Session.CookieName)
	}
	if cfg.Auth.GroupMappings.Admin != "mailshield-admin" {
		t.Errorf("Auth.GroupMappings.Admin 期待: mailshield-admin, 実際: %s", cfg.Auth.GroupMappings.Admin)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("Log.Level 期待: info, 実際: %s", cfg.Log.Level)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/api-server.yaml")
	if err == nil {
		t.Error("存在しないファイルでエラーが期待されるが、エラーがなかった")
	}
}

func TestLoad_EnvOverridesYAML(t *testing.T) {
	path := writeYAML(t, validYAML)

	t.Setenv("DB_HOST", "override-mariadb")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_USER", "override-user")
	t.Setenv("DB_PASSWORD", "override-password")
	t.Setenv("REDIS_HOST", "override-redis")
	t.Setenv("OIDC_CLIENT_SECRET", "override-secret")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("環境変数上書きテストで読み込み失敗: %v", err)
	}

	if cfg.Database.Host != "override-mariadb" {
		t.Errorf("DB_HOST 上書き期待: override-mariadb, 実際: %s", cfg.Database.Host)
	}
	if cfg.Database.Port != 5432 {
		t.Errorf("DB_PORT 上書き期待: 5432, 実際: %d", cfg.Database.Port)
	}
	if cfg.Database.User != "override-user" {
		t.Errorf("DB_USER 上書き期待: override-user, 実際: %s", cfg.Database.User)
	}
	if cfg.Database.Password != "override-password" {
		t.Errorf("DB_PASSWORD 上書き期待: override-password, 実際: %s", cfg.Database.Password)
	}
	if cfg.Redis.Host != "override-redis" {
		t.Errorf("REDIS_HOST 上書き期待: override-redis, 実際: %s", cfg.Redis.Host)
	}
	if cfg.Auth.Providers.OIDC.ClientSecret != "override-secret" {
		t.Errorf("OIDC_CLIENT_SECRET 上書き期待: override-secret, 実際: %s", cfg.Auth.Providers.OIDC.ClientSecret)
	}
}

func TestValidateAuthDirectory_DefaultsOK(t *testing.T) {
	path := writeYAML(t, `
server:
  port: 8080
database:
  host: mariadb
`)
	if _, err := Load(path); err != nil {
		t.Fatalf("デフォルト設定（directory.source/auth.sso_mode 省略）は許容されるべき: %v", err)
	}
}

func TestValidateAuthDirectory_ScimRequiresSSO(t *testing.T) {
	path := writeYAML(t, `
server:
  port: 8080
database:
  host: mariadb
directory:
  source: scim
`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("directory.source: scim かつ auth.sso_mode 未設定（disabled）はエラーになるべき")
	}
}

func TestValidateAuthDirectory_ScimWithSSOOptionalOK(t *testing.T) {
	path := writeYAML(t, `
server:
  port: 8080
database:
  host: mariadb
directory:
  source: scim
auth:
  sso_mode: optional
  providers:
    oidc:
      issuer: https://idp.example.com
`)
	if _, err := Load(path); err != nil {
		t.Fatalf("directory.source: scim + sso_mode: optional は許容されるべき: %v", err)
	}
}

func TestValidateAuthDirectory_InvalidSource(t *testing.T) {
	path := writeYAML(t, `
server:
  port: 8080
database:
  host: mariadb
directory:
  source: invalid-value
`)
	if _, err := Load(path); err == nil {
		t.Fatal("directory.source が不正な値の場合エラーになるべき")
	}
}

func TestValidateAuthDirectory_InvalidSSOMode(t *testing.T) {
	path := writeYAML(t, `
server:
  port: 8080
database:
  host: mariadb
auth:
  sso_mode: invalid-value
`)
	if _, err := Load(path); err == nil {
		t.Fatal("auth.sso_mode が不正な値の場合エラーになるべき")
	}
}

func TestValidateAuthDirectory_RequiredNeedsOIDCIssuer(t *testing.T) {
	path := writeYAML(t, `
server:
  port: 8080
database:
  host: mariadb
auth:
  sso_mode: required
`)
	if _, err := Load(path); err == nil {
		t.Fatal("sso_mode: required で oidc.issuer 未設定はエラーになるべき")
	}
}

func TestValidateAuthDirectory_RequiredWithOIDCIssuerOK(t *testing.T) {
	path := writeYAML(t, `
server:
  port: 8080
database:
  host: mariadb
auth:
  sso_mode: required
  providers:
    oidc:
      issuer: https://idp.example.com
`)
	if _, err := Load(path); err != nil {
		t.Fatalf("sso_mode: required + oidc.issuer 設定済みは許容されるべき: %v", err)
	}
}

func TestAuthConfig_EffectiveDefaults(t *testing.T) {
	var a AuthConfig
	if a.EffectiveSSOMode() != SSOModeDisabled {
		t.Errorf("EffectiveSSOMode() 省略時 = %q, want disabled", a.EffectiveSSOMode())
	}
	if !a.LocalLoginAllowed() {
		t.Error("デフォルトでは LocalLoginAllowed() は true であるべき")
	}
	if a.SSOAllowed() {
		t.Error("デフォルトでは SSOAllowed() は false であるべき")
	}

	a.SSOMode = SSOModeRequired
	if a.LocalLoginAllowed() {
		t.Error("required では LocalLoginAllowed() は false であるべき")
	}
	if !a.SSOAllowed() {
		t.Error("required では SSOAllowed() は true であるべき")
	}
}

func TestDirectoryConfig_EffectiveSource(t *testing.T) {
	var d DirectoryConfig
	if d.EffectiveSource() != DirectorySourceNone {
		t.Errorf("EffectiveSource() 省略時 = %q, want none", d.EffectiveSource())
	}
	d.Source = DirectorySourceLDAP
	if d.EffectiveSource() != DirectorySourceLDAP {
		t.Errorf("EffectiveSource() = %q, want ldap", d.EffectiveSource())
	}
}

func TestLoad_MailboxProvisioning(t *testing.T) {
	path := writeYAML(t, `
server:
  port: 8080
database:
  host: mariadb
directory:
  source: ldap
  ldap:
    host: ldap.corp.local
    base_dn: "ou=Users,dc=corp,dc=local"
    mailbox_provisioning:
      roles:
        member:
          method: user_attribute
          source_attribute: memberOf
          source_transform: '^cn=(?P<value>[^,]+),.*$'
          dereference:
            base_dn: "ou=Groups,dc=corp,dc=local"
            filter: "(cn={value})"
          target_attribute: mail
        owner:
          method: group_search
          base_dn: "ou=Groups,dc=corp,dc=local"
          filter: "(mail=*)"
          member_attr: owner
          target_attribute: mail
        admin:
          method: fixed
          fixed_value: "admin@internal.dev; backup@internal.dev"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	roles := cfg.Directory.LDAP.MailboxProvisioning.Roles
	if len(roles) != 3 {
		t.Fatalf("Roles = %d 件, want 3", len(roles))
	}
	member := roles["member"]
	if member.Method != "user_attribute" || member.SourceAttribute != "memberOf" {
		t.Errorf("member = %+v", member)
	}
	if member.Dereference.BaseDN != "ou=Groups,dc=corp,dc=local" || member.Dereference.Filter != "(cn={value})" {
		t.Errorf("member.Dereference = %+v", member.Dereference)
	}
	if member.TargetAttribute != "mail" {
		t.Errorf("member.TargetAttribute = %q", member.TargetAttribute)
	}
	owner := roles["owner"]
	if owner.Method != "group_search" || owner.MemberAttr != "owner" || owner.Filter != "(mail=*)" {
		t.Errorf("owner = %+v", owner)
	}
	admin := roles["admin"]
	if admin.Method != "fixed" || admin.FixedValue != "admin@internal.dev; backup@internal.dev" {
		t.Errorf("admin = %+v", admin)
	}
}
