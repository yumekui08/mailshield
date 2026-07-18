package ldap

import (
	"errors"
	"strings"
	"testing"

	"github.com/koizumib/mailshield/services/api-server/internal/directory"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

// countingSearcher は検索回数を記録するフェイク Searcher（キャッシュ検証用）。
type countingSearcher struct {
	searchFunc func(baseDN, filter string, attrs []string) ([]Entry, error)
	calls      []string // 実行された filter の履歴
}

func (c *countingSearcher) SearchUsers(baseDN, filter string, attrs []string) ([]Entry, error) {
	c.calls = append(c.calls, filter)
	if c.searchFunc != nil {
		return c.searchFunc(baseDN, filter, attrs)
	}
	return nil, nil
}

func (c *countingSearcher) Close() error { return nil }

func chainRule(t *testing.T, role domain.AssignmentRole, steps ...map[string]any) RoleResolution {
	t.Helper()
	rr, err := CompileChainRule(role, steps)
	if err != nil {
		t.Fatalf("CompileChainRule: %v", err)
	}
	return rr
}

func mailboxStrings(tuples []directory.MailboxAssignmentTuple) []string {
	out := make([]string, len(tuples))
	for i, t := range tuples {
		out[i] = string(t.Role) + ":" + t.MailboxEmail
	}
	return out
}

// ─── チェーン: 個人メールボックス（self mail → to_mailbox） ───────────────────

func TestChain_PersonalMailbox(t *testing.T) {
	mr := &MailboxResolution{Roles: []RoleResolution{
		chainRule(t, domain.AssignmentRoleOwner,
			map[string]any{"self": "mail"},
			map[string]any{"to_mailbox": map[string]any{}},
		),
	}}
	entry := Entry{DN: "uid=tanaka,ou=Users,dc=x", Attributes: map[string][]string{
		"mail": {"tanaka@internal.dev"},
	}}
	got, _ := mr.ResolveForUser(nil, entry, NewDerefCache())
	if len(got) != 1 || got[0].MailboxEmail != "tanaka@internal.dev" || got[0].Role != domain.AssignmentRoleOwner {
		t.Errorf("個人メールボックス解決が不正: %+v", got)
	}
}

// ─── チェーン: グループメンバー（memberOf → regex で cn 抽出 → domain 補完） ────

func TestChain_GroupMemberFromMemberOf(t *testing.T) {
	mr := &MailboxResolution{Roles: []RoleResolution{
		chainRule(t, domain.AssignmentRoleMember,
			map[string]any{"self": "memberOf"},
			map[string]any{"regex": `^cn=(?P<value>[^,]+),ou=Groups`},
			map[string]any{"to_mailbox": map[string]any{"domain": "internal.dev"}},
		),
	}}
	entry := Entry{DN: "uid=tanaka,ou=Users,dc=x", Attributes: map[string][]string{
		"memberOf": {
			"cn=sales,ou=Groups,dc=internal,dc=dev",
			"cn=staff,ou=Groups,dc=internal,dc=dev",
			"cn=mailshield-viewers,ou=AppRoles,dc=internal,dc=dev", // ou=Groups でないので除外
		},
	}}
	t0, _ := mr.ResolveForUser(nil, entry, NewDerefCache())
	got := mailboxStrings(t0)
	want := []string{"member:sales@internal.dev", "member:staff@internal.dev"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("グループメンバー解決が不正: got=%v want=%v", got, want)
	}
}

// ─── チェーン: グループ管理者（dn → search owner={value} → attr cn → domain） ──

func TestChain_GroupOwnerViaSearch(t *testing.T) {
	searcher := &countingSearcher{searchFunc: func(_, filter string, _ []string) ([]Entry, error) {
		if strings.Contains(filter, "uid=sato") {
			return []Entry{{DN: "cn=sales,ou=Groups", Attributes: map[string][]string{"cn": {"sales"}}}}, nil
		}
		return nil, nil
	}}
	mr := &MailboxResolution{Roles: []RoleResolution{
		chainRule(t, domain.AssignmentRoleApprover,
			map[string]any{"self": "dn"},
			map[string]any{"search": map[string]any{
				"base_dn": "ou=Groups,dc=x",
				"filter":  "(&(objectClass=groupOfNames)(owner={value}))",
				"attrs":   "cn",
			}},
			map[string]any{"attr": "cn"},
			map[string]any{"to_mailbox": map[string]any{"domain": "internal.dev"}},
		),
	}}
	entry := Entry{DN: "uid=sato,ou=Users,dc=x"}
	t0, _ := mr.ResolveForUser(searcher, entry, NewDerefCache())
	got := mailboxStrings(t0)
	if len(got) != 1 || got[0] != "approver:sales@internal.dev" {
		t.Errorf("グループ管理者解決が不正: %v", got)
	}
	if len(searcher.calls) != 1 || !strings.Contains(searcher.calls[0], "owner=uid=sato") {
		t.Errorf("owner フィルタが期待通りでない: %v", searcher.calls)
	}
}

// ─── search の {value} は LDAP エスケープされる ────────────────────────────────

func TestChain_SearchEscaping(t *testing.T) {
	var captured string
	searcher := &countingSearcher{searchFunc: func(_, filter string, _ []string) ([]Entry, error) {
		captured = filter
		return nil, nil
	}}
	mr := &MailboxResolution{Roles: []RoleResolution{
		chainRule(t, domain.AssignmentRoleMember,
			map[string]any{"self": "memberOf"},
			map[string]any{"search": map[string]any{"base_dn": "ou=g,dc=x", "filter": "(cn={value})"}},
			map[string]any{"attr": "mail"},
			map[string]any{"to_mailbox": map[string]any{}},
		),
	}}
	entry := Entry{Attributes: map[string][]string{"memberOf": {"ev*il)(uid=admin"}}}
	_, _ = mr.ResolveForUser(searcher, entry, NewDerefCache())
	if strings.Contains(captured, "*") || strings.Contains(captured, ")(") {
		t.Errorf("LDAP メタ文字がエスケープされていない: %q", captured)
	}
}

// ─── search キャッシュ: 同一 (base, filter) は 1 回だけ ──────────────────────

func TestChain_SearchCache(t *testing.T) {
	searcher := &countingSearcher{searchFunc: func(_, _ string, _ []string) ([]Entry, error) {
		return []Entry{{Attributes: map[string][]string{"mail": {"sales@x"}}}}, nil
	}}
	mr := &MailboxResolution{Roles: []RoleResolution{
		chainRule(t, domain.AssignmentRoleMember,
			map[string]any{"self": "memberOf"},
			map[string]any{"regex": `^cn=(?P<value>[^,]+),`},
			map[string]any{"search": map[string]any{"base_dn": "ou=g,dc=x", "filter": "(cn={value})"}},
			map[string]any{"attr": "mail"},
			map[string]any{"to_mailbox": map[string]any{}},
		),
	}}
	cache := NewDerefCache()
	for _, u := range []string{"a", "b", "c"} {
		entry := Entry{DN: "uid=" + u, Attributes: map[string][]string{"memberOf": {"cn=sales,ou=g,dc=x"}}}
		_, _ = mr.ResolveForUser(searcher, entry, cache)
	}
	if len(searcher.calls) != 1 {
		t.Errorf("同一グループの検索は 1 回のはず: %d 回", len(searcher.calls))
	}
}

// ─── search エラーはそのルールをスキップ（他ルール・同期は止めない） ───────────

func TestChain_SearchErrorSkips(t *testing.T) {
	searcher := &countingSearcher{searchFunc: func(_, _ string, _ []string) ([]Entry, error) {
		return nil, errors.New("boom")
	}}
	mr := &MailboxResolution{Roles: []RoleResolution{
		// エラーになるチェーン
		chainRule(t, domain.AssignmentRoleMember,
			map[string]any{"self": "memberOf"},
			map[string]any{"search": map[string]any{"base_dn": "ou=g,dc=x", "filter": "(cn={value})"}},
			map[string]any{"attr": "mail"},
			map[string]any{"to_mailbox": map[string]any{}},
		),
		// 正常な個人ルール
		chainRule(t, domain.AssignmentRoleOwner,
			map[string]any{"self": "mail"},
			map[string]any{"to_mailbox": map[string]any{}},
		),
	}}
	entry := Entry{Attributes: map[string][]string{"memberOf": {"cn=x,ou=g,dc=x"}, "mail": {"me@x"}}}
	t0, _ := mr.ResolveForUser(searcher, entry, NewDerefCache())
	got := mailboxStrings(t0)
	if len(got) != 1 || got[0] != "owner:me@x" {
		t.Errorf("エラールールはスキップし正常ルールは残るべき: %v", got)
	}
}

// ─── fixed: FixedRolesForEmail ────────────────────────────────────────────────

func TestFixedRolesForEmail(t *testing.T) {
	mr := &MailboxResolution{Roles: []RoleResolution{
		FixedRule(domain.AssignmentRoleApprover, []string{"admin@internal.dev", "backup@internal.dev"}),
	}}
	if roles := mr.FixedRolesForEmail("admin@internal.dev"); len(roles) != 1 || roles[0] != domain.AssignmentRoleApprover {
		t.Errorf("fixed 承認者が解決されない: %v", roles)
	}
	if roles := mr.FixedRolesForEmail("other@internal.dev"); len(roles) != 0 {
		t.Errorf("対象外に fixed が付与された: %v", roles)
	}
	// fixed は ResolveForUser では解決しない
	if got, _ := mr.ResolveForUser(nil, Entry{Attributes: map[string][]string{"mail": {"admin@internal.dev"}}}, NewDerefCache()); len(got) != 0 {
		t.Errorf("fixed は ResolveForUser で解決してはいけない: %v", got)
	}
}
