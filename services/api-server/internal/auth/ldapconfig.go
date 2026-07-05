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

	mailboxMapper, err := buildGroupMailboxMapper(cfg.MailboxMappings)
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
		MailboxMapper:     mailboxMapper,
		DeactivateMissing: cfg.DeactivateMissingUsers,
	}

	return connCfg, syncCfg, nil
}

// buildGroupMailboxMapper は config.MailboxMappingsConfig から directory.GroupMailboxMapper を
// 組み立てる。pattern.regex が設定されている場合はコンパイル・名前付きキャプチャグループの
// 検証を行い、不正なら起動時エラーとして返す。
func buildGroupMailboxMapper(cfg config.MailboxMappingsConfig) (directory.GroupMailboxMapper, error) {
	mappings := make([]directory.GroupMailboxMapping, 0, len(cfg.List))
	for _, entry := range cfg.List {
		mappings = append(mappings, directory.GroupMailboxMapping{
			Group:              entry.Group,
			MailboxEmail:       entry.Mailbox,
			MailboxDisplayName: entry.MailboxDisplayName,
			Role:               domain.AssignmentRole(entry.Role),
		})
	}

	mapper := directory.GroupMailboxMapper{Mappings: mappings}

	if cfg.Pattern.Regex != "" {
		pattern, err := directory.NewGroupMailboxPattern(cfg.Pattern.Regex, cfg.Pattern.MailboxDomain)
		if err != nil {
			return directory.GroupMailboxMapper{}, fmt.Errorf("directory.ldap.mailbox_mappings.pattern が不正です: %w", err)
		}
		mapper.Pattern = pattern
	}

	return mapper, nil
}
