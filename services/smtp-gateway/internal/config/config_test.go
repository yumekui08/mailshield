package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/config"
)

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
	yaml := `
server:
  smtp_port: 10024
  reinject_host: postfix
  reinject_port: 10025
  trusted_sources:
    - 127.0.0.1
database:
  host: localhost
  port: 3306
  name: mailshield
  user: mailshield
  password: secret
routes:
  - name: inbound
    direction: inbound
    match:
      to: "@internal.test$"
    policy:
      rules_file: /etc/mailshield/policy.yaml
`
	f := filepath.Join(t.TempDir(), "mailshield.yaml")
	if err := os.WriteFile(f, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(f)
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

func TestLoad_FileNotFound(t *testing.T) {
	_, err := config.Load("/nonexistent/path/mailshield.yaml")
	if err == nil {
		t.Error("存在しないファイルはエラーを返すべき")
	}
}
