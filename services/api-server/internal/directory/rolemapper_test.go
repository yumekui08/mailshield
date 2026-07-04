package directory

import (
	"testing"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

func TestGroupRoleMapper_Resolve(t *testing.T) {
	mapper := GroupRoleMapper{
		AdminGroup:    "mailshield-admins",
		OperatorGroup: "mailshield-operators",
		ViewerGroup:   "mailshield-viewers",
	}

	tests := []struct {
		name   string
		groups []string
		want   domain.Role
	}{
		{"admin グループ所属", []string{"mailshield-admins", "other-group"}, domain.RoleAdmin},
		{"operator グループ所属", []string{"mailshield-operators"}, domain.RoleOperator},
		{"viewer グループ所属", []string{"mailshield-viewers"}, domain.RoleViewer},
		{"複数マッチは admin 優先", []string{"mailshield-viewers", "mailshield-admins"}, domain.RoleAdmin},
		{"マッチなしは viewer にフォールバック", []string{"unrelated-group"}, domain.RoleViewer},
		{"グループなしは viewer にフォールバック", nil, domain.RoleViewer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapper.Resolve(tt.groups); got != tt.want {
				t.Errorf("Resolve(%v) = %q, want %q", tt.groups, got, tt.want)
			}
		})
	}
}

func TestGroupRoleMapper_UnconfiguredGroupsIgnored(t *testing.T) {
	// admin_group が設定されていない場合、同名のグループが claim に含まれても admin にならない。
	mapper := GroupRoleMapper{ViewerGroup: "mailshield-viewers"}
	if got := mapper.Resolve([]string{"mailshield-admins"}); got != domain.RoleViewer {
		t.Errorf("Resolve() = %q, want viewer（未設定のマッピングは無視されるべき）", got)
	}
}
