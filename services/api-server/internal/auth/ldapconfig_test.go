package auth

import (
	"strings"
	"testing"

	"github.com/koizumib/mailshield/services/api-server/internal/config"
	ldapsync "github.com/koizumib/mailshield/services/api-server/internal/directory/ldap"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

func TestBuildLDAPConnConfig_MailboxProvisioning_UserAttribute(t *testing.T) {
	cfg := &config.LDAPConfig{
		Host: "ldap.corp.local",
		MailboxProvisioning: config.MailboxProvisioningConfig{
			Rules: []config.MailboxProvisioningRuleConfig{
				{
					Role:            "member",
					Method:          ldapsync.MethodUserAttribute,
					SourceAttribute: "memberOf",
					SourceTransform: `^cn=(?P<value>[^,]+),.*$`,
					Dereference: config.MailboxDereferenceConfig{
						BaseDN: "ou=Groups,dc=corp,dc=local",
						Filter: "(cn={value})",
					},
					TargetAttribute: "mail",
				},
			},
		},
	}

	_, syncCfg, err := BuildLDAPConnConfig(cfg)
	if err != nil {
		t.Fatalf("BuildLDAPConnConfig() error = %v", err)
	}
	if syncCfg.MailboxResolution.Empty() {
		t.Fatal("MailboxResolution が構築されるべき")
	}
	rr := syncCfg.MailboxResolution.Roles[0]
	if rr.Role != domain.AssignmentRoleMember || rr.Method != ldapsync.MethodUserAttribute {
		t.Errorf("rr = %+v", rr)
	}
	if rr.SourceTransform == nil || rr.Dereference == nil || rr.TargetAttribute != "mail" {
		t.Errorf("パイプライン構成が不完全: %+v", rr)
	}
}

// TestBuildLDAPConnConfig_MailboxProvisioning_MultipleRulesSameRole は同じロールに
// 複数のルールを設定できることを確認する（個人メールボックス + 共有メールボックスの併用）。
func TestBuildLDAPConnConfig_MailboxProvisioning_MultipleRulesSameRole(t *testing.T) {
	cfg := &config.LDAPConfig{
		Host: "ldap.corp.local",
		MailboxProvisioning: config.MailboxProvisioningConfig{
			Rules: []config.MailboxProvisioningRuleConfig{
				// 個人メールボックス: 自分の mail 属性（変換・再検索なし）
				{Role: "owner", Method: ldapsync.MethodUserAttribute, SourceAttribute: "mail"},
				// 共有メールボックス: memberOf → 再検索
				{
					Role: "member", Method: ldapsync.MethodUserAttribute, SourceAttribute: "memberOf",
					Dereference:     config.MailboxDereferenceConfig{BaseDN: "ou=g", Filter: "(cn={value})"},
					TargetAttribute: "mail",
				},
				// 同じ member ロールに group_search の 2 ルール目
				{
					Role: "member", Method: ldapsync.MethodGroupSearch,
					BaseDN: "ou=g", Filter: "(mail=*)", MemberAttr: "member", TargetAttribute: "mail",
				},
			},
		},
	}

	_, syncCfg, err := BuildLDAPConnConfig(cfg)
	if err != nil {
		t.Fatalf("BuildLDAPConnConfig() error = %v", err)
	}
	if len(syncCfg.MailboxResolution.Roles) != 3 {
		t.Fatalf("Roles = %d 件, want 3（同一ロールの複数ルールが保持されるべき）", len(syncCfg.MailboxResolution.Roles))
	}
	// 個人メールボックスルール: 変換・再検索なしで自分の mail をそのまま使う
	self := syncCfg.MailboxResolution.Roles[0]
	if self.Role != domain.AssignmentRoleOwner || self.SourceAttribute != "mail" || self.Dereference != nil {
		t.Errorf("個人メールボックスルール = %+v", self)
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
			rc:      config.MailboxProvisioningRuleConfig{Role: "superuser", Method: ldapsync.MethodFixed, FixedValue: "a@b.c"},
			wantErr: "role が不正",
		},
		{
			name:    "role 未指定",
			rc:      config.MailboxProvisioningRuleConfig{Method: ldapsync.MethodFixed, FixedValue: "a@b.c"},
			wantErr: "role が不正",
		},
		{
			name:    "method 未指定",
			rc:      config.MailboxProvisioningRuleConfig{Role: "member"},
			wantErr: "method は必須",
		},
		{
			name:    "未知の method",
			rc:      config.MailboxProvisioningRuleConfig{Role: "member", Method: "magic"},
			wantErr: "未知の method",
		},
		{
			name:    "user_attribute で source_attribute 欠落",
			rc:      config.MailboxProvisioningRuleConfig{Role: "member", Method: ldapsync.MethodUserAttribute},
			wantErr: "source_attribute が必須",
		},
		{
			name: "不正な正規表現",
			rc: config.MailboxProvisioningRuleConfig{
				Role: "member", Method: ldapsync.MethodUserAttribute, SourceAttribute: "memberOf", SourceTransform: "(unclosed",
			},
			wantErr: "コンパイル失敗",
		},
		{
			name: "dereference filter に {value} プレースホルダ無し",
			rc: config.MailboxProvisioningRuleConfig{
				Role: "member", Method: ldapsync.MethodUserAttribute, SourceAttribute: "memberOf",
				Dereference:     config.MailboxDereferenceConfig{BaseDN: "ou=g", Filter: "(cn=static)"},
				TargetAttribute: "mail",
			},
			wantErr: "{value} プレースホルダが必要",
		},
		{
			name: "dereference 使用時に target_attribute 欠落",
			rc: config.MailboxProvisioningRuleConfig{
				Role: "member", Method: ldapsync.MethodUserAttribute, SourceAttribute: "memberOf",
				Dereference: config.MailboxDereferenceConfig{BaseDN: "ou=g", Filter: "(cn={value})"},
			},
			wantErr: "target_attribute が必須",
		},
		{
			name: "dereference 無しで target_attribute 指定",
			rc: config.MailboxProvisioningRuleConfig{
				Role: "member", Method: ldapsync.MethodUserAttribute, SourceAttribute: "memberOf", TargetAttribute: "mail",
			},
			wantErr: "dereference と併用したときのみ有効",
		},
		{
			name:    "group_search で必須フィールド欠落",
			rc:      config.MailboxProvisioningRuleConfig{Role: "owner", Method: ldapsync.MethodGroupSearch, BaseDN: "ou=g"},
			wantErr: "すべて必須",
		},
		{
			name:    "fixed で fixed_value 欠落",
			rc:      config.MailboxProvisioningRuleConfig{Role: "approver", Method: ldapsync.MethodFixed},
			wantErr: "fixed_value",
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

func TestBuildLDAPConnConfig_MailboxProvisioning_Fixed(t *testing.T) {
	cfg := &config.LDAPConfig{
		Host: "ldap.corp.local",
		MailboxProvisioning: config.MailboxProvisioningConfig{
			Rules: []config.MailboxProvisioningRuleConfig{
				{Role: "approver", Method: ldapsync.MethodFixed, FixedValue: "a@x.com, b@x.com; c@x.com"},
			},
		},
	}
	_, syncCfg, err := BuildLDAPConnConfig(cfg)
	if err != nil {
		t.Fatalf("BuildLDAPConnConfig() error = %v", err)
	}
	rr := syncCfg.MailboxResolution.Roles[0]
	if len(rr.FixedUserEmails) != 3 {
		t.Errorf("FixedUserEmails = %v, want 3 件（カンマ・セミコロン混在で分割されるべき）", rr.FixedUserEmails)
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
