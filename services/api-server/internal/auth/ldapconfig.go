package auth

import (
	"fmt"
	"time"

	"github.com/koizumib/mailshield/services/api-server/internal/config"
	"github.com/koizumib/mailshield/services/api-server/internal/directory"
	ldapsync "github.com/koizumib/mailshield/services/api-server/internal/directory/ldap"
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
		DeactivateMissing: cfg.DeactivateMissingUsers,
	}

	return connCfg, syncCfg, nil
}
