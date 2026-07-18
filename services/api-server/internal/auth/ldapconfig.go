package auth

import (
	"fmt"
	"regexp"
	"strings"
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
	rr := ldapsync.RoleResolution{Role: role, Method: rc.Method}

	switch rc.Method {
	case ldapsync.MethodUserAttribute:
		if rc.SourceAttribute == "" {
			return rr, fmt.Errorf("method: user_attribute には source_attribute が必須です")
		}
		rr.SourceAttribute = rc.SourceAttribute
		rr.MailboxDomain = rc.MailboxDomain

		var err error
		if rr.SourceTransform, err = compileTransform("source_transform", rc.SourceTransform); err != nil {
			return rr, err
		}
		if rr.TargetTransform, err = compileTransform("target_transform", rc.TargetTransform); err != nil {
			return rr, err
		}

		hasDeref := rc.Dereference.BaseDN != "" || rc.Dereference.Filter != ""
		if hasDeref {
			if rc.Dereference.BaseDN == "" || rc.Dereference.Filter == "" {
				return rr, fmt.Errorf("dereference には base_dn と filter の両方が必要です")
			}
			if !strings.Contains(rc.Dereference.Filter, "{value}") {
				return rr, fmt.Errorf("dereference.filter には {value} プレースホルダが必要です: %q", rc.Dereference.Filter)
			}
			if rc.TargetAttribute == "" {
				return rr, fmt.Errorf("dereference を使う場合は target_attribute が必須です")
			}
			rr.Dereference = &ldapsync.DereferenceRule{BaseDN: rc.Dereference.BaseDN, Filter: rc.Dereference.Filter}
			rr.TargetAttribute = rc.TargetAttribute
		} else if rc.TargetAttribute != "" {
			return rr, fmt.Errorf("target_attribute は dereference と併用したときのみ有効です（dereference 無しでは source の値がそのまま使われます）")
		}

	case ldapsync.MethodGroupSearch:
		if rc.BaseDN == "" || rc.Filter == "" || rc.MemberAttr == "" || rc.TargetAttribute == "" {
			return rr, fmt.Errorf("method: group_search には base_dn・filter・member_attr・target_attribute がすべて必須です")
		}
		rr.BaseDN = rc.BaseDN
		rr.Filter = rc.Filter
		rr.MemberAttr = rc.MemberAttr
		rr.TargetAttribute = rc.TargetAttribute
		rr.MailboxDomain = rc.MailboxDomain

		var err error
		if rr.TargetTransform, err = compileTransform("target_transform", rc.TargetTransform); err != nil {
			return rr, err
		}

	case ldapsync.MethodFixed:
		emails := splitFixedValues(rc.FixedValue)
		if len(emails) == 0 {
			return rr, fmt.Errorf("method: fixed には fixed_value（カンマまたはセミコロン区切りのメールアドレス）が必須です")
		}
		rr.FixedUserEmails = emails

	case "":
		return rr, fmt.Errorf("method は必須です（user_attribute | group_search | fixed）")
	default:
		return rr, fmt.Errorf("未知の method です: %q（user_attribute | group_search | fixed）", rc.Method)
	}

	return rr, nil
}

// compileTransform は変換用正規表現をコンパイルする。空文字列なら nil（変換なし）。
func compileTransform(name, pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("%s のコンパイル失敗: %w", name, err)
	}
	return re, nil
}

// splitFixedValues はカンマ・セミコロン区切りの値リストをパースする。
func splitFixedValues(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' }) {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}
