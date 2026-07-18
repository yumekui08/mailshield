package ldap

import (
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

// TestLive_ChainResolution は実 LDAP（環境変数 MAILSHIELD_TEST_LDAP=1 のときのみ）に対して
// チェーン解決を検証する。接続情報:
//
//	ldap://192.168.1.103:3389
//	bind: uid=dovecot-bind,ou=ServiceAccounts,dc=internal,dc=dev / SvcBindPass3!
func TestLive_ChainResolution(t *testing.T) {
	if os.Getenv("MAILSHIELD_TEST_LDAP") != "1" {
		t.Skip("MAILSHIELD_TEST_LDAP=1 のときのみ実行")
	}
	searcher, err := Dial(ConnConfig{
		Host:         "192.168.1.103",
		Port:         3389,
		BindDN:       "uid=dovecot-bind,ou=ServiceAccounts,dc=internal,dc=dev",
		BindPassword: "SvcBindPass3!",
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer searcher.Close()

	// tanaka のエントリを取得（mail / memberOf を持つ）
	entries, err := searcher.SearchUsers("ou=Users,dc=internal,dc=dev", "(uid=tanaka)", []string{"mail", "memberOf"})
	if err != nil || len(entries) != 1 {
		t.Fatalf("tanaka の取得失敗: err=%v n=%d", err, len(entries))
	}
	tanaka := entries[0]
	t.Logf("tanaka.DN=%s mail=%v memberOf=%v", tanaka.DN, tanaka.Attributes["mail"], tanaka.Attributes["memberOf"])

	mr := &MailboxResolution{Roles: []RoleResolution{
		// ① 個人メールボックス: mail → owner
		mustChain(t, domain.AssignmentRoleOwner,
			map[string]any{"self": "mail"},
			map[string]any{"to_mailbox": map[string]any{}},
		),
		// ② グループメンバー: memberOf の cn → member（ou=Groups のみ）
		mustChain(t, domain.AssignmentRoleMember,
			map[string]any{"self": "memberOf"},
			map[string]any{"regex": `^cn=(?P<value>[^,]+),ou=Groups`},
			map[string]any{"to_mailbox": map[string]any{"domain": "internal.dev"}},
		),
		// ③ グループ管理者: owner=自分 のグループ cn → approver
		mustChain(t, domain.AssignmentRoleApprover,
			map[string]any{"self": "dn"},
			map[string]any{"search": map[string]any{
				"base_dn": "ou=Groups,dc=internal,dc=dev",
				"filter":  "(&(objectClass=groupOfNames)(owner={value}))",
				"attrs":   "cn",
			}},
			map[string]any{"attr": "cn"},
			map[string]any{"to_mailbox": map[string]any{"domain": "internal.dev"}},
		),
	}}

	tuples, rerr := mr.ResolveForUser(searcher, tanaka, NewDerefCache())
	if rerr != nil {
		t.Fatalf("ResolveForUser: %v", rerr)
	}
	got := make([]string, 0, len(tuples))
	for _, tp := range tuples {
		got = append(got, string(tp.Role)+":"+tp.MailboxEmail)
	}
	sort.Strings(got)
	t.Logf("tanaka の解決結果: %v", got)

	// tanaka: mail=tanaka@internal.dev, memberOf に sales / staff を含む（LDAP データより）
	assertContains(t, got, "owner:tanaka@internal.dev")
	assertContains(t, got, "member:sales@internal.dev")
	assertContains(t, got, "member:staff@internal.dev")

	// sato はいくつかのグループの owner（管理者 → approver）
	satoEntries, _ := searcher.SearchUsers("ou=Users,dc=internal,dc=dev", "(uid=sato)", []string{"mail", "memberOf"})
	if len(satoEntries) == 1 {
		st, _ := mr.ResolveForUser(searcher, satoEntries[0], NewDerefCache())
		var approvers []string
		for _, tp := range st {
			if tp.Role == domain.AssignmentRoleApprover {
				approvers = append(approvers, tp.MailboxEmail)
			}
		}
		t.Logf("sato の approver メールボックス: %v", approvers)
		if len(approvers) == 0 {
			t.Errorf("sato はグループ owner のはずだが approver 解決が 0 件")
		}
	}
}

func mustChain(t *testing.T, role domain.AssignmentRole, steps ...map[string]any) RoleResolution {
	t.Helper()
	rr, err := CompileChainRule(role, steps)
	if err != nil {
		t.Fatal(err)
	}
	return rr
}

func assertContains(t *testing.T, got []string, want string) {
	t.Helper()
	for _, g := range got {
		if g == want {
			return
		}
	}
	t.Errorf("解決結果に %q が含まれない: %v", want, strings.Join(got, ", "))
}
