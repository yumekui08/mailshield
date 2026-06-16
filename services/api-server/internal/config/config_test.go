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
  providers:
    standalone:
      enabled: true
    oidc:
      enabled: true
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
