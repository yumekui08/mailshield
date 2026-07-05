package auth

import (
	"testing"

	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

func TestBuildLDAPConnConfig_MailboxMappings_ExplicitList(t *testing.T) {
	cfg := &config.LDAPConfig{
		Host: "ldap.corp.local", BaseDN: "ou=Users,dc=corp,dc=local",
		MailboxMappings: config.MailboxMappingsConfig{
			List: []config.MailboxMappingEntry{
				{Group: "Sales-Team", Mailbox: "sales@example.com", MailboxDisplayName: "Sales", Role: "member"},
			},
		},
	}

	_, syncCfg, err := BuildLDAPConnConfig(cfg)
	if err != nil {
		t.Fatalf("BuildLDAPConnConfig() error = %v", err)
	}

	tuples := syncCfg.MailboxMapper.Resolve([]string{"Sales-Team"})
	if len(tuples) != 1 || tuples[0].MailboxEmail != "sales@example.com" || tuples[0].Role != domain.AssignmentRoleMember {
		t.Errorf("tuples = %+v", tuples)
	}
}

func TestBuildLDAPConnConfig_MailboxMappings_ValidPattern(t *testing.T) {
	cfg := &config.LDAPConfig{
		Host: "ldap.corp.local",
		MailboxMappings: config.MailboxMappingsConfig{
			Pattern: config.MailboxMappingPatternConfig{
				Regex:         `^mbx-(?P<mailbox>[\w.-]+)-(?P<role>member|owner|admin)$`,
				MailboxDomain: "example.com",
			},
		},
	}

	_, syncCfg, err := BuildLDAPConnConfig(cfg)
	if err != nil {
		t.Fatalf("BuildLDAPConnConfig() error = %v", err)
	}

	tuples := syncCfg.MailboxMapper.Resolve([]string{"mbx-sales-member"})
	if len(tuples) != 1 || tuples[0].MailboxEmail != "sales@example.com" {
		t.Errorf("tuples = %+v", tuples)
	}
}

// TestBuildLDAPConnConfig_MailboxMappings_InvalidPattern は名前付きキャプチャグループの
// 欠落や不正な正規表現が起動時エラー（BuildLDAPConnConfig の戻り値）として検出されることを確認する。
// main.go はこのエラーを os.Exit(1) につなげており、実行時に静かに機能しないままにはならない。
func TestBuildLDAPConnConfig_MailboxMappings_InvalidPattern(t *testing.T) {
	tests := []struct {
		name  string
		regex string
	}{
		{"名前付きキャプチャグループ無し", `^mbx-([\w.-]+)-(member|owner|admin)$`},
		{"不正な正規表現", `(unclosed`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.LDAPConfig{
				Host: "ldap.corp.local",
				MailboxMappings: config.MailboxMappingsConfig{
					Pattern: config.MailboxMappingPatternConfig{Regex: tt.regex},
				},
			}
			if _, _, err := BuildLDAPConnConfig(cfg); err == nil {
				t.Error("不正な pattern はエラーになるべき")
			}
		})
	}
}

func TestBuildLDAPConnConfig_MailboxMappings_EmptyIsOK(t *testing.T) {
	cfg := &config.LDAPConfig{Host: "ldap.corp.local"}
	_, syncCfg, err := BuildLDAPConnConfig(cfg)
	if err != nil {
		t.Fatalf("mailbox_mappings 未設定はエラーになるべきではない: %v", err)
	}
	if tuples := syncCfg.MailboxMapper.Resolve([]string{"anything"}); len(tuples) != 0 {
		t.Errorf("未設定時は常に空であるべき: %+v", tuples)
	}
}
