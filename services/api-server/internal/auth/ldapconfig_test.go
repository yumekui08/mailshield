package auth

import (
	"strings"
	"testing"

	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

// TestBuildLDAPConnConfig_MailboxProvisioning_Chain はチェーン方式が構築されることを確認する。
func TestBuildLDAPConnConfig_MailboxProvisioning_Chain(t *testing.T) {
	cfg := &config.LDAPConfig{
		Host: "ldap.corp.local",
		MailboxProvisioning: config.MailboxProvisioningConfig{
			Rules: []config.MailboxProvisioningRuleConfig{
				{Role: "owner", Chain: []map[string]any{
					{"self": "mail"},
					{"to_mailbox": map[string]any{}},
				}},
				{Role: "member", Chain: []map[string]any{
					{"self": "memberOf"},
					{"regex": `^cn=(?P<value>[^,]+),ou=Groups`},
					{"to_mailbox": map[string]any{"domain": "internal.dev"}},
				}},
			},
		},
	}
	_, syncCfg, err := BuildLDAPConnConfig(cfg)
	if err != nil {
		t.Fatalf("BuildLDAPConnConfig() error = %v", err)
	}
	if syncCfg.MailboxResolution.Empty() || len(syncCfg.MailboxResolution.Roles) != 2 {
		t.Fatalf("2 ルールが構築されるべき: %+v", syncCfg.MailboxResolution)
	}
	if syncCfg.MailboxResolution.Roles[0].Role != domain.AssignmentRoleOwner {
		t.Errorf("rule[0].Role = %q", syncCfg.MailboxResolution.Roles[0].Role)
	}
}

// TestBuildLDAPConnConfig_MailboxProvisioning_Fixed は fixed 方式を確認する。
func TestBuildLDAPConnConfig_MailboxProvisioning_Fixed(t *testing.T) {
	cfg := &config.LDAPConfig{
		Host: "ldap.corp.local",
		MailboxProvisioning: config.MailboxProvisioningConfig{
			Rules: []config.MailboxProvisioningRuleConfig{
				{Role: "approver", Fixed: []string{"a@x.com", "b@x.com"}},
			},
		},
	}
	_, syncCfg, err := BuildLDAPConnConfig(cfg)
	if err != nil {
		t.Fatalf("BuildLDAPConnConfig() error = %v", err)
	}
	if roles := syncCfg.MailboxResolution.FixedRolesForEmail("a@x.com"); len(roles) != 1 {
		t.Errorf("fixed 承認者が解決されない: %v", roles)
	}
}

func TestBuildLDAPConnConfig_MailboxProvisioning_Validation(t *testing.T) {
	tests := []struct {
		name    string
		rc      config.MailboxProvisioningRuleConfig
		wantErr string
	}{
		{
			name:    "不正なロール",
			rc:      config.MailboxProvisioningRuleConfig{Role: "superuser", Fixed: []string{"a@b.c"}},
			wantErr: "role が不正",
		},
		{
			name:    "chain / lua / fixed いずれも未指定",
			rc:      config.MailboxProvisioningRuleConfig{Role: "member"},
			wantErr: "いずれか 1 つを指定",
		},
		{
			name: "chain と fixed の同時指定",
			rc: config.MailboxProvisioningRuleConfig{
				Role: "member", Fixed: []string{"a@b.c"},
				Chain: []map[string]any{{"self": "mail"}, {"to_mailbox": map[string]any{}}},
			},
			wantErr: "同時に指定できません",
		},
		{
			name: "chain が to_mailbox で終わらない",
			rc: config.MailboxProvisioningRuleConfig{
				Role: "member", Chain: []map[string]any{{"self": "mail"}},
			},
			wantErr: "to_mailbox で終える",
		},
		{
			name: "chain が self / const で始まらない",
			rc: config.MailboxProvisioningRuleConfig{
				Role: "member", Chain: []map[string]any{{"regex": "x"}, {"to_mailbox": map[string]any{}}},
			},
			wantErr: "self または const で始める",
		},
		{
			name: "不正な正規表現",
			rc: config.MailboxProvisioningRuleConfig{
				Role: "member", Chain: []map[string]any{{"self": "memberOf"}, {"regex": "(unclosed"}, {"to_mailbox": map[string]any{}}},
			},
			wantErr: "正規表現のコンパイル失敗",
		},
		{
			name: "search filter に {value} プレースホルダ無し",
			rc: config.MailboxProvisioningRuleConfig{
				Role: "member", Chain: []map[string]any{
					{"self": "memberOf"},
					{"search": map[string]any{"base_dn": "ou=g", "filter": "(cn=static)"}},
					{"attr": "mail"},
					{"to_mailbox": map[string]any{}},
				},
			},
			wantErr: "{value} プレースホルダが必要",
		},
		{
			name: "未知のステップ種別",
			rc: config.MailboxProvisioningRuleConfig{
				Role: "member", Chain: []map[string]any{{"self": "mail"}, {"teleport": "x"}, {"to_mailbox": map[string]any{}}},
			},
			wantErr: "未知のステップ種別",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.LDAPConfig{
				Host: "ldap.corp.local",
				MailboxProvisioning: config.MailboxProvisioningConfig{
					Rules: []config.MailboxProvisioningRuleConfig{tt.rc},
				},
			}
			_, _, err := BuildLDAPConnConfig(cfg)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want contains %q", err, tt.wantErr)
			}
		})
	}
}

func TestBuildLDAPConnConfig_MailboxProvisioning_EmptyIsOK(t *testing.T) {
	cfg := &config.LDAPConfig{Host: "ldap.corp.local"}
	_, syncCfg, err := BuildLDAPConnConfig(cfg)
	if err != nil {
		t.Fatalf("mailbox_provisioning 未設定はエラーになるべきではない: %v", err)
	}
	if !syncCfg.MailboxResolution.Empty() {
		t.Error("未設定時は Empty() が true であるべき")
	}
}
