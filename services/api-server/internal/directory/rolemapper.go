package directory

import "github.com/koizumib/mailshield/services/api-server/internal/domain"

// GroupRoleMapper はグループ名の集合から MailShield ロールを解決する。
// OIDC の groups claim・LDAP の memberOf など、グループ所属でロールを表現する
// 複数のソースが同じマッピング方式を共有できるようにする（DRY）。
// 各ソースは自身の設定（グループ名）から個別に GroupRoleMapper を構築する
// （OIDC と LDAP でグループ名の形式が異なる ── 例: OIDC は "mailshield-admins"、
// LDAP は "CN=MailShield-Admins,OU=Groups,DC=corp,DC=local" ── ため、
// マッピングのロジックだけを共通化し、設定値はソースごとに独立させる）。
type GroupRoleMapper struct {
	AdminGroup    string
	OperatorGroup string
	ViewerGroup   string
}

// Resolve はグループ集合から最上位のロールを決定する。
// マッチするマッピングがなければ最低権限（viewer）を返す。
func (m GroupRoleMapper) Resolve(groups []string) domain.Role {
	groupSet := make(map[string]struct{}, len(groups))
	for _, g := range groups {
		groupSet[g] = struct{}{}
	}

	if m.AdminGroup != "" {
		if _, ok := groupSet[m.AdminGroup]; ok {
			return domain.RoleAdmin
		}
	}
	if m.OperatorGroup != "" {
		if _, ok := groupSet[m.OperatorGroup]; ok {
			return domain.RoleOperator
		}
	}
	if m.ViewerGroup != "" {
		if _, ok := groupSet[m.ViewerGroup]; ok {
			return domain.RoleViewer
		}
	}

	// デフォルトは最低権限
	return domain.RoleViewer
}
