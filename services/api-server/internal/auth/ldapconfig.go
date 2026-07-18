package auth

import (
	"fmt"
	"time"

	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/directory"
	ldapsync "github.com/koizumib/mailshield/services/api-server/internal/directory/ldap"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

// BuildLDAPConnConfig は config.LDAPConfig から接続設定と同期設定を組み立てる。
// directory.source: ldap のとき、この変換結果は定期同期（cmd/server/main.go の Syncer）と
// bind 認証（LDAPBindProvider）の両方から共通で使われる（同じディレクトリ・同じマッピングを
// 参照するため、変換ロジックを1箇所に集約する）。
func BuildLDAPConnConfig(cfg *config.LDAPConfig) (ldapsync.ConnConfig, ldapsync.SyncConfig, error) {
	tlsMode := ldapsync.TLSMode(cfg.TLS)
	switch tlsMode {
	case "":
		tlsMode = ldapsync.TLSNone
	case ldapsync.TLSNone, ldapsync.TLSStartTLS, ldapsync.TLSLDAPS:
	default:
		return ldapsync.ConnConfig{}, ldapsync.SyncConfig{}, fmt.Errorf("directory.ldap.tls が不正です: %q（none | starttls | ldaps）", cfg.TLS)
	}

	connCfg := ldapsync.ConnConfig{
		Host:          cfg.Host,
		Port:          cfg.Port,
		TLS:           tlsMode,
		TLSSkipVerify: cfg.TLSSkipVerify,
		BindDN:        cfg.BindDN,
		BindPassword:  cfg.BindPassword,
		SearchTimeout: time.Duration(cfg.SearchTimeoutSeconds) * time.Second,
		PageSize:      uint32(cfg.PageSize),
	}

	mailboxResolution, err := buildMailboxResolution(cfg.MailboxProvisioning)
	if err != nil {
		return ldapsync.ConnConfig{}, ldapsync.SyncConfig{}, err
	}

	syncCfg := ldapsync.SyncConfig{
		BaseDN:     cfg.BaseDN,
		UserFilter: cfg.UserFilter,
		EmailAttr:  cfg.Attributes.Email,
		NameAttr:   cfg.Attributes.DisplayName,
		GroupsAttr: cfg.Attributes.Groups,
		RoleMapper: directory.GroupRoleMapper{
			AdminGroup:    cfg.GroupMappings.Admin,
			OperatorGroup: cfg.GroupMappings.Operator,
			ViewerGroup:   cfg.GroupMappings.Viewer,
		},
		MailboxResolution: mailboxResolution,
		DeactivateMissing: cfg.DeactivateMissingUsers,
	}

	return connCfg, syncCfg, nil
}

// buildMailboxResolution は config.MailboxProvisioningConfig を検証し、
// コンパイル済みの ldapsync.MailboxResolution を組み立てる。
// 同じロールに対する複数ルールを許容する（全ルールの解決結果が合算される）。
// 設定不正（未知の role/method・正規表現の構文エラー・必須フィールド欠落・
// dereference filter の {value} プレースホルダ欠落）はすべて起動時エラーとして返す。
func buildMailboxResolution(cfg config.MailboxProvisioningConfig) (*ldapsync.MailboxResolution, error) {
	if len(cfg.Rules) == 0 {
		return nil, nil
	}

	resolution := &ldapsync.MailboxResolution{}
	for i, rc := range cfg.Rules {
		role := domain.AssignmentRole(rc.Role)
		switch role {
		case domain.AssignmentRoleMember, domain.AssignmentRoleOwner, domain.AssignmentRoleApprover:
		default:
			return nil, fmt.Errorf("mailbox_provisioning.rules[%d]: role が不正です: %q（member | owner | approver）", i, rc.Role)
		}

		rr, err := buildRoleResolution(role, rc)
		if err != nil {
			return nil, fmt.Errorf("mailbox_provisioning.rules[%d] (role=%s): %w", i, rc.Role, err)
		}
		resolution.Roles = append(resolution.Roles, rr)
	}
	return resolution, nil
}

func buildRoleResolution(role domain.AssignmentRole, rc config.MailboxProvisioningRuleConfig) (ldapsync.RoleResolution, error) {
	// chain / lua / fixed のいずれか 1 つだけを指定する。
	set := 0
	if len(rc.Chain) > 0 {
		set++
	}
	if rc.Lua != "" {
		set++
	}
	if len(rc.Fixed) > 0 {
		set++
	}
	if set == 0 {
		return ldapsync.RoleResolution{}, fmt.Errorf("chain / lua / fixed のいずれか 1 つを指定してください")
	}
	if set > 1 {
		return ldapsync.RoleResolution{}, fmt.Errorf("chain / lua / fixed は同時に指定できません")
	}

	switch {
	case len(rc.Chain) > 0:
		return ldapsync.CompileChainRule(role, rc.Chain)
	case rc.Lua != "":
		return ldapsync.CompileLuaRule(role, rc.Lua)
	default:
		return ldapsync.FixedRule(role, rc.Fixed), nil
	}
}
