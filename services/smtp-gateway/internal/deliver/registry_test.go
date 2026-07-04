package deliver

import (
	"strings"
	"testing"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/config"
)

// TestRegistry_ResolveByName は deliverer 名で解決できることを確認する。
func TestRegistry_ResolveByName(t *testing.T) {
	reg, err := NewRegistry(map[string]config.DelivererConfig{
		"sendgrid": {
			Host: "smtp.sendgrid.net", Port: 587, TLS: "starttls",
			Auth: config.DelivererAuthConfig{Username: "apikey", Password: "SG.x"},
		},
	}, "", 0)
	if err != nil {
		t.Fatal(err)
	}

	d, err := reg.Resolve("sendgrid")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name() != "sendgrid" || d.Addr() != "smtp.sendgrid.net:587" {
		t.Errorf("name=%s addr=%s", d.Name(), d.Addr())
	}
	if d.tls != TLSStartTLS || d.username != "apikey" {
		t.Errorf("tls=%s username=%s", d.tls, d.username)
	}
}

// TestRegistry_ResolveDefault は destination 空のとき deliverers.default が
// 使われることを確認する。
func TestRegistry_ResolveDefault(t *testing.T) {
	reg, err := NewRegistry(map[string]config.DelivererConfig{
		"default": {Host: "postfix", Port: 25},
	}, "ignored-reinject-host", 25)
	if err != nil {
		t.Fatal(err)
	}

	d, err := reg.Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name() != "default" || d.Addr() != "postfix:25" {
		t.Errorf("deliverers.default が優先されるべきです: name=%s addr=%s", d.Name(), d.Addr())
	}
}

// TestRegistry_ResolveLegacyReinject は deliverers.default 未定義のとき
// reinject.host:port にフォールバックすることを確認する（後方互換）。
func TestRegistry_ResolveLegacyReinject(t *testing.T) {
	reg, err := NewRegistry(nil, "mailpit", 1025)
	if err != nil {
		t.Fatal(err)
	}

	d, err := reg.Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if d.Addr() != "mailpit:1025" {
		t.Errorf("reinject フォールバックの addr = %s, want mailpit:1025", d.Addr())
	}
	if d.tls != TLSNone {
		t.Errorf("reinject フォールバックは平文 SMTP であるべきです: %s", d.tls)
	}
}

// TestRegistry_ResolveNoDefault はデフォルトが一切ない場合にエラーになることを確認する。
func TestRegistry_ResolveNoDefault(t *testing.T) {
	reg, err := NewRegistry(nil, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Resolve(""); err == nil {
		t.Fatal("デフォルト配送先なしで destination 空はエラーになるべきです")
	}
}

// TestRegistry_ResolveHostPort は名前にマッチしない destination が
// host:port として解釈されることを確認する（後方互換）。
func TestRegistry_ResolveHostPort(t *testing.T) {
	reg, err := NewRegistry(map[string]config.DelivererConfig{
		"sendgrid": {Host: "smtp.sendgrid.net", Port: 587, TLS: "starttls"},
	}, "", 0)
	if err != nil {
		t.Fatal(err)
	}

	// port あり
	d, err := reg.Resolve("mailpit:1025")
	if err != nil {
		t.Fatal(err)
	}
	if d.Addr() != "mailpit:1025" || d.tls != TLSNone {
		t.Errorf("addr=%s tls=%s", d.Addr(), d.tls)
	}

	// port なし → defaultPort（reinject.port 由来。0 なら 25）を補完
	d, err = reg.Resolve("mailhost")
	if err != nil {
		t.Fatal(err)
	}
	if d.Addr() != "mailhost:25" {
		t.Errorf("port 補完後の addr = %s, want mailhost:25", d.Addr())
	}
}

// TestRegistry_NamePrecedesHostPort は deliverer 名が host 解釈より優先されることを確認する。
func TestRegistry_NamePrecedesHostPort(t *testing.T) {
	reg, err := NewRegistry(map[string]config.DelivererConfig{
		"mailpit": {Host: "other-host", Port: 2525},
	}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	d, err := reg.Resolve("mailpit")
	if err != nil {
		t.Fatal(err)
	}
	if d.Addr() != "other-host:2525" {
		t.Errorf("deliverer 名の解決が優先されるべきです: addr=%s", d.Addr())
	}
}

// TestNewRegistry_Validation は不正な設定が起動時エラーになることを確認する。
func TestNewRegistry_Validation(t *testing.T) {
	tests := []struct {
		name    string
		cfgs    map[string]config.DelivererConfig
		wantErr string
	}{
		{
			name:    "コロンを含む名前",
			cfgs:    map[string]config.DelivererConfig{"bad:name": {Host: "h"}},
			wantErr: "名が不正",
		},
		{
			name:    "host 未設定",
			cfgs:    map[string]config.DelivererConfig{"nohost": {}},
			wantErr: "host が未設定",
		},
		{
			name:    "未対応の type",
			cfgs:    map[string]config.DelivererConfig{"api": {Type: "http-api", Host: "h"}},
			wantErr: "未対応の type",
		},
		{
			name:    "未対応の tls",
			cfgs:    map[string]config.DelivererConfig{"badtls": {Host: "h", TLS: "ssl3"}},
			wantErr: "未対応の tls",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRegistry(tt.cfgs, "", 0)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want contains %q", err, tt.wantErr)
			}
		})
	}
}

// TestNewRegistry_Defaults は type / port / tls の省略時デフォルトを確認する。
func TestNewRegistry_Defaults(t *testing.T) {
	reg, err := NewRegistry(map[string]config.DelivererConfig{
		"minimal": {Host: "mta.internal"},
	}, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	d, err := reg.Resolve("minimal")
	if err != nil {
		t.Fatal(err)
	}
	if d.Addr() != "mta.internal:25" || d.tls != TLSNone {
		t.Errorf("addr=%s tls=%s（デフォルトは :25 / none）", d.Addr(), d.tls)
	}
}
