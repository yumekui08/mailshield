package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/config"
)

// makeRouteDir は routesDir 配下にルートディレクトリと route.yaml を作成するヘルパー。
func makeRouteDir(t *testing.T, routesDir, dirName, routeYAML string) {
	t.Helper()
	d := filepath.Join(routesDir, dirName)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "route.yaml"), []byte(routeYAML), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadModeFor(t *testing.T) {
	cfg := &config.AttachmentDownloadConfig{
		Flows: []config.AttachmentDownloadFlow{
			{Match: "inbound", Mode: "otp"},
			{Match: "outbound", Mode: "auth", AllowedRoles: []string{"admin", "owner"}},
			{Match: "internal", Mode: "simple"},
		},
	}

	tests := []struct {
		direction    string
		wantMode     string
		wantRolesLen int
	}{
		{"inbound", "otp", 0},
		{"outbound", "auth", 2},
		{"internal", "simple", 0},
		{"unknown", "simple", 0},
		{"", "simple", 0},
	}

	for _, tt := range tests {
		t.Run(tt.direction, func(t *testing.T) {
			mode, roles := cfg.DownloadModeFor(tt.direction)
			if mode != tt.wantMode {
				t.Errorf("mode = %q, want %q", mode, tt.wantMode)
			}
			if len(roles) != tt.wantRolesLen {
				t.Errorf("len(roles) = %d, want %d", len(roles), tt.wantRolesLen)
			}
		})
	}
}

func TestDownloadModeFor_FirstMatchWins(t *testing.T) {
	cfg := &config.AttachmentDownloadConfig{
		Flows: []config.AttachmentDownloadFlow{
			{Match: "inbound", Mode: "otp"},
			{Match: "inbound", Mode: "auth"},
		},
	}
	mode, _ := cfg.DownloadModeFor("inbound")
	if mode != "otp" {
		t.Errorf("最初のマッチが適用されるべき: got %q, want %q", mode, "otp")
	}
}

func TestDownloadModeFor_EmptyFlows(t *testing.T) {
	cfg := &config.AttachmentDownloadConfig{}
	mode, roles := cfg.DownloadModeFor("inbound")
	if mode != "simple" {
		t.Errorf("フロー未設定はデフォルト simple を返すべき: got %q", mode)
	}
	if roles != nil {
		t.Errorf("フロー未設定はロールが nil であるべき: got %v", roles)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	mainYAML := `
server:
  smtp_port: 10024
  trusted_sources:
    - 127.0.0.1
database:
  host: localhost
  port: 3306
  name: mailshield
  user: mailshield
  password: secret
`
	f := filepath.Join(dir, "mailshield.yaml")
	if err := os.WriteFile(f, []byte(mainYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	routesDir := filepath.Join(dir, "routes.d")
	makeRouteDir(t, routesDir, "10-inbound", `
name: inbound
direction: inbound
match:
  to: "@internal.test$"
policy:
  rules_file: /etc/mailshield/policy.yaml
`)

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.SMTPPort != 10024 {
		t.Errorf("SMTPPort = %d, want 10024", cfg.Server.SMTPPort)
	}
	if cfg.Database.Host != "localhost" {
		t.Errorf("Database.Host = %q, want localhost", cfg.Database.Host)
	}
	if len(cfg.Routes) != 1 || cfg.Routes[0].Direction != "inbound" {
		t.Errorf("Routes[0].Direction = %q, want inbound", cfg.Routes[0].Direction)
	}
	if len(cfg.Server.TrustedSources) != 1 || cfg.Server.TrustedSources[0] != "127.0.0.1" {
		t.Errorf("TrustedSources = %v, want [127.0.0.1]", cfg.Server.TrustedSources)
	}
}

func TestLoad_RoutesOrdering(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mailshield.yaml"), []byte("server:\n  smtp_port: 10024\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	routesDir := filepath.Join(dir, "routes.d")
	// 意図的に逆順で作成してもディレクトリ名のアルファベット順に読まれることを確認する
	for _, tc := range []struct{ dir, name string }{
		{"20-outbound", "outbound"},
		{"00-bounce", "bounce"},
		{"10-inbound", "inbound"},
	} {
		makeRouteDir(t, routesDir, tc.dir,
			"name: "+tc.name+"\ndirection: outbound\npolicy:\n  rules_file: /dev/null\n")
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := []string{"bounce", "inbound", "outbound"}
	if len(cfg.Routes) != 3 {
		t.Fatalf("len(Routes) = %d, want 3", len(cfg.Routes))
	}
	for i, w := range want {
		if cfg.Routes[i].Name != w {
			t.Errorf("Routes[%d].Name = %q, want %q", i, cfg.Routes[i].Name, w)
		}
	}
}

func TestLoad_PolicyAutoResolve(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mailshield.yaml"), []byte("server:\n  smtp_port: 10024\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	routesDir := filepath.Join(dir, "routes.d")
	// route.yaml に policy: を書かずに policy.yaml / policy.lua を同ディレクトリに置く
	routeDir := filepath.Join(routesDir, "10-inbound")
	if err := os.MkdirAll(routeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(routeDir, "route.yaml"),
		[]byte("name: inbound\ndirection: inbound\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(routeDir, "policy.yaml"),
		[]byte("rules:\n  - name: default\n    condition: \"true\"\n    action: deliver\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(routeDir, "policy.lua"),
		[]byte("-- custom lua\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Routes) != 1 {
		t.Fatalf("len(Routes) = %d, want 1", len(cfg.Routes))
	}
	wantRules := filepath.Join(routeDir, "policy.yaml")
	if cfg.Routes[0].Policy.RulesFile != wantRules {
		t.Errorf("RulesFile = %q, want %q", cfg.Routes[0].Policy.RulesFile, wantRules)
	}
	wantLua := filepath.Join(routeDir, "policy.lua")
	if cfg.Routes[0].Policy.LuaFile != wantLua {
		t.Errorf("LuaFile = %q, want %q", cfg.Routes[0].Policy.LuaFile, wantLua)
	}
}

func TestLoad_PolicyAutoResolveNoFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mailshield.yaml"), []byte("server:\n  smtp_port: 10024\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// policy.yaml が存在しない場合は RulesFile が空のままであることを確認
	makeRouteDir(t, filepath.Join(dir, "routes.d"), "10-inbound",
		"name: inbound\ndirection: inbound\n")

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Routes[0].Policy.RulesFile != "" {
		t.Errorf("policy.yaml が存在しない場合 RulesFile は空であるべき: got %q", cfg.Routes[0].Policy.RulesFile)
	}
}

func TestLoad_MailshieldDFragment(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mailshield.yaml"),
		[]byte("server:\n  smtp_port: 10024\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "routes.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	// mailshield.d/ にフラグメントを置いて値が反映されることを確認する
	fragDir := filepath.Join(dir, "mailshield.d")
	if err := os.MkdirAll(fragDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fragDir, "ldap.yaml"),
		[]byte("database:\n  host: fragment-db\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Database.Host != "fragment-db" {
		t.Errorf("Database.Host = %q, want fragment-db (from mailshield.d/ldap.yaml)", cfg.Database.Host)
	}
}

func TestLoad_MailshieldDNotExistIsOK(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mailshield.yaml"),
		[]byte("server:\n  smtp_port: 10024\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "routes.d"), 0o755); err != nil {
		t.Fatal(err)
	}
	// mailshield.d/ が存在しなくても正常に読み込める
	_, err := config.Load(dir)
	if err != nil {
		t.Errorf("mailshield.d/ が存在しなくてもエラーにならないべき: %v", err)
	}
}

func TestLoad_RoutesDirNotExistIsOK(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mailshield.yaml"),
		[]byte("server:\n  smtp_port: 10024\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// routes.d/ が存在しなくても正常に読み込める（フレッシュインストール対応）
	cfg, err := config.Load(dir)
	if err != nil {
		t.Errorf("routes.d/ が存在しなくてもエラーにならないべき: %v", err)
	}
	if len(cfg.Routes) != 0 {
		t.Errorf("routes.d/ がない場合 Routes は空であるべき: got %v", cfg.Routes)
	}
}

func TestLoad_RouteWithoutRouteYamlSkipped(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mailshield.yaml"),
		[]byte("server:\n  smtp_port: 10024\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	routesDir := filepath.Join(dir, "routes.d")
	// route.yaml のないディレクトリはスキップされる
	if err := os.MkdirAll(filepath.Join(routesDir, "99-empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	makeRouteDir(t, routesDir, "10-inbound", "name: inbound\ndirection: inbound\n")

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Routes) != 1 || cfg.Routes[0].Name != "inbound" {
		t.Errorf("route.yaml のないディレクトリがスキップされ inbound のみ残るべき: %v", cfg.Routes)
	}
}

func TestLoad_DefaultAndOverride(t *testing.T) {
	dir := t.TempDir()

	defaultYAML := `
server:
  smtp_port: 10024
  health_port: 8080
database:
  host: default-db
  port: 3306
`
	userYAML := `
database:
  host: override-db
  password: secret
`
	defaultFile := filepath.Join(dir, "mailshield.default.yaml")
	userFile := filepath.Join(dir, "mailshield.yaml")
	if err := os.WriteFile(defaultFile, []byte(defaultYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userFile, []byte(userYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "routes.d"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	// デフォルト値が引き継がれる
	if cfg.Server.SMTPPort != 10024 {
		t.Errorf("SMTPPort = %d, want 10024 (from default)", cfg.Server.SMTPPort)
	}
	if cfg.Server.HealthPort != 8080 {
		t.Errorf("HealthPort = %d, want 8080 (from default)", cfg.Server.HealthPort)
	}
	// ユーザー設定で上書きされる
	if cfg.Database.Host != "override-db" {
		t.Errorf("Database.Host = %q, want override-db", cfg.Database.Host)
	}
	if cfg.Database.Password != "secret" {
		t.Errorf("Database.Password = %q, want secret", cfg.Database.Password)
	}
	// デフォルトの他フィールドも引き継がれる
	if cfg.Database.Port != 3306 {
		t.Errorf("Database.Port = %d, want 3306 (from default)", cfg.Database.Port)
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := config.Load("/nonexistent/path")
	if err == nil {
		t.Error("存在しないディレクトリはエラーを返すべき")
	}
}
