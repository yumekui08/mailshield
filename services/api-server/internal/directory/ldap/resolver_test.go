package ldap

import (
	"errors"
	"regexp"
	"strings"
	"testing"

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

// TestResolveUserAttribute_NoDereference は dereference 無しの解決
// （source_transform によるフィルタ兼抽出 + mailbox_domain 補完）を確認する。
func TestResolveUserAttribute_NoDereference(t *testing.T) {
	mr := &MailboxResolution{Roles: []RoleResolution{{
		Role:            domain.AssignmentRoleMember,
		Method:          MethodUserAttribute,
		SourceAttribute: "memberOf",
		SourceTransform: regexp.MustCompile(`^cn=mbx-(?P<value>[\w-]+),.*$`),
		MailboxDomain:   "example.com",
	}}}
	entry := Entry{DN: "cn=alice,ou=Users,dc=x", Attributes: map[string][]string{
		"memberOf": {
			"cn=mbx-sales,ou=Groups,dc=x",
			"cn=unrelated-group,ou=Groups,dc=x", // 正規表現に一致しない → スキップ
			"cn=mbx-hr,ou=Groups,dc=x",
		},
	}}

	tuples := mr.ResolveUserAttribute(nil, entry, NewDerefCache())
	if len(tuples) != 2 {
		t.Fatalf("tuples = %d 件, want 2: %+v", len(tuples), tuples)
	}
	if tuples[0].MailboxEmail != "sales@example.com" || tuples[1].MailboxEmail != "hr@example.com" {
		t.Errorf("tuples = %+v", tuples)
	}
}

// TestResolveUserAttribute_WithDereference は dereference（1回の再検索）を含む
// フルパイプラインと、埋め込み値の強制エスケープを確認する。
func TestResolveUserAttribute_WithDereference(t *testing.T) {
	groupEntry := Entry{DN: "cn=sales,ou=Groups,dc=x", Attributes: map[string][]string{
		"mail": {"sales@example.com"},
	}}
	searcher := &countingSearcher{searchFunc: func(_, filter string, _ []string) ([]Entry, error) {
		if filter == "(cn=sales)" {
			return []Entry{groupEntry}, nil
		}
		return nil, nil
	}}

	mr := &MailboxResolution{Roles: []RoleResolution{{
		Role:            domain.AssignmentRoleMember,
		Method:          MethodUserAttribute,
		SourceAttribute: "memberOf",
		SourceTransform: regexp.MustCompile(`^cn=(?P<value>[^,]+),ou=Groups.*$`),
		Dereference:     &DereferenceRule{BaseDN: "ou=Groups,dc=x", Filter: "(cn={value})"},
		TargetAttribute: "mail",
	}}}
	entry := Entry{DN: "cn=alice,ou=Users,dc=x", Attributes: map[string][]string{
		"memberOf": {"cn=sales,ou=Groups,dc=x"},
	}}

	tuples := mr.ResolveUserAttribute(searcher, entry, NewDerefCache())
	if len(tuples) != 1 || tuples[0].MailboxEmail != "sales@example.com" {
		t.Fatalf("tuples = %+v", tuples)
	}
}

// TestResolveUserAttribute_DereferenceEscaping は LDAP 特殊文字を含む値が
// 必ずエスケープされてフィルタに埋め込まれることを確認する（インジェクション対策）。
func TestResolveUserAttribute_DereferenceEscaping(t *testing.T) {
	searcher := &countingSearcher{}
	mr := &MailboxResolution{Roles: []RoleResolution{{
		Role:            domain.AssignmentRoleMember,
		Method:          MethodUserAttribute,
		SourceAttribute: "memberOf",
		// 変換なし: 値がそのまま dereference に渡る
		Dereference:     &DereferenceRule{BaseDN: "ou=Groups,dc=x", Filter: "(cn={value})"},
		TargetAttribute: "mail",
	}}}
	// フィルタ構文を壊そうとする悪意ある値
	entry := Entry{DN: "cn=mallory,ou=Users,dc=x", Attributes: map[string][]string{
		"memberOf": {"*)(objectClass=*"},
	}}

	mr.ResolveUserAttribute(searcher, entry, NewDerefCache())

	if len(searcher.calls) != 1 {
		t.Fatalf("検索回数 = %d, want 1", len(searcher.calls))
	}
	got := searcher.calls[0]
	if strings.Contains(got, "*)(objectClass=*") {
		t.Errorf("エスケープされていない値がフィルタに埋め込まれた: %q", got)
	}
	// go-ldap の EscapeFilter は * → \2a, ( → \28, ) → \29 に変換する
	if !strings.Contains(got, `\2a`) {
		t.Errorf("エスケープ済みの値が含まれるべき: %q", got)
	}
}

// TestResolveUserAttribute_DereferenceCache は同一の (base_dn, filter) の再検索が
// キャッシュされ、実クエリが1回で済むことを確認する（N+1 対策）。
func TestResolveUserAttribute_DereferenceCache(t *testing.T) {
	searcher := &countingSearcher{searchFunc: func(_, _ string, _ []string) ([]Entry, error) {
		return []Entry{{DN: "cn=sales,ou=Groups,dc=x", Attributes: map[string][]string{"mail": {"sales@example.com"}}}}, nil
	}}
	mr := &MailboxResolution{Roles: []RoleResolution{{
		Role:            domain.AssignmentRoleMember,
		Method:          MethodUserAttribute,
		SourceAttribute: "memberOf",
		Dereference:     &DereferenceRule{BaseDN: "ou=Groups,dc=x", Filter: "(cn={value})"},
		TargetAttribute: "mail",
	}}}

	cache := NewDerefCache()
	// 同じグループに属する2ユーザーを同一キャッシュで処理する（定期同期の1サイクルを模す）
	for _, user := range []string{"alice", "bob"} {
		entry := Entry{DN: "cn=" + user + ",ou=Users,dc=x", Attributes: map[string][]string{
			"memberOf": {"sales"},
		}}
		tuples := mr.ResolveUserAttribute(searcher, entry, cache)
		if len(tuples) != 1 {
			t.Fatalf("%s の tuples = %+v", user, tuples)
		}
	}

	if len(searcher.calls) != 1 {
		t.Errorf("実クエリ回数 = %d, want 1（2ユーザー目はキャッシュから解決されるべき）", len(searcher.calls))
	}
}

// TestResolveUserAttribute_DereferenceSearchErrorSkips は再検索エラーが
// その値のスキップに留まる（他の値の処理を止めない）ことを確認する。
func TestResolveUserAttribute_DereferenceSearchErrorSkips(t *testing.T) {
	searcher := &countingSearcher{searchFunc: func(_, filter string, _ []string) ([]Entry, error) {
		if strings.Contains(filter, "bad") {
			return nil, errors.New("search failed")
		}
		return []Entry{{Attributes: map[string][]string{"mail": {"good@example.com"}}}}, nil
	}}
	mr := &MailboxResolution{Roles: []RoleResolution{{
		Role:            domain.AssignmentRoleMember,
		Method:          MethodUserAttribute,
		SourceAttribute: "memberOf",
		Dereference:     &DereferenceRule{BaseDN: "ou=Groups,dc=x", Filter: "(cn={value})"},
		TargetAttribute: "mail",
	}}}
	entry := Entry{Attributes: map[string][]string{"memberOf": {"bad", "good"}}}

	tuples := mr.ResolveUserAttribute(searcher, entry, NewDerefCache())
	if len(tuples) != 1 || tuples[0].MailboxEmail != "good@example.com" {
		t.Errorf("tuples = %+v（エラーの値はスキップ、正常な値は処理されるべき）", tuples)
	}
}

// TestResolveGroupSearchForUser は JIT 用の member 絞り込みフィルタが
// (&(元filter)(member_attr=エスケープ済みDN)) の形で組み立てられることを確認する。
func TestResolveGroupSearchForUser(t *testing.T) {
	searcher := &countingSearcher{searchFunc: func(_, filter string, _ []string) ([]Entry, error) {
		return []Entry{{DN: "cn=sales,ou=Groups,dc=x", Attributes: map[string][]string{"mail": {"sales@example.com"}}}}, nil
	}}
	mr := &MailboxResolution{Roles: []RoleResolution{{
		Role:            domain.AssignmentRoleOwner,
		Method:          MethodGroupSearch,
		BaseDN:          "ou=Groups,dc=x",
		Filter:          "(mail=*)",
		MemberAttr:      "owner",
		TargetAttribute: "mail",
	}}}

	tuples := mr.ResolveGroupSearchForUser(searcher, "cn=Alice (Sales),ou=Users,dc=x")
	if len(tuples) != 1 || tuples[0].Role != domain.AssignmentRoleOwner {
		t.Fatalf("tuples = %+v", tuples)
	}
	if len(searcher.calls) != 1 {
		t.Fatalf("検索回数 = %d, want 1", len(searcher.calls))
	}
	got := searcher.calls[0]
	if !strings.HasPrefix(got, "(&(mail=*)(owner=") {
		t.Errorf("フィルタの形が期待と異なる: %q", got)
	}
	// DN 内の括弧はエスケープされているべき
	if strings.Contains(got, "(Sales)") {
		t.Errorf("DN 内の特殊文字がエスケープされていない: %q", got)
	}
}

// TestApplyTransform は変換の抽出順序（named group "value" > 最初のキャプチャ > 全体）を確認する。
func TestApplyTransform(t *testing.T) {
	tests := []struct {
		name   string
		re     *regexp.Regexp
		in     string
		want   string
		wantOK bool
	}{
		{"named value グループ", regexp.MustCompile(`^cn=(?P<value>[^,]+),`), "cn=sales,ou=g", "sales", true},
		{"無名キャプチャ", regexp.MustCompile(`^cn=([^,]+),`), "cn=sales,ou=g", "sales", true},
		{"キャプチャ無し（全体マッチ）", regexp.MustCompile(`^[a-z]+$`), "sales", "sales", true},
		{"不一致はスキップ", regexp.MustCompile(`^cn=`), "ou=only", "", false},
		{"nil はそのまま", nil, "raw-value", "raw-value", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := applyTransform(tt.re, tt.in)
			if ok != tt.wantOK || got != tt.want {
				t.Errorf("applyTransform() = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// TestNormalizeDN は表記ゆれ（大文字小文字・空白）の吸収を確認する。
func TestNormalizeDN(t *testing.T) {
	a := NormalizeDN("CN=Alice, OU=Users, DC=corp, DC=local")
	b := NormalizeDN("cn=alice,ou=users,dc=corp,dc=local")
	if a != b {
		t.Errorf("正規化後は一致するべき: %q != %q", a, b)
	}
}

// TestFixedRolesForEmail は大文字小文字を無視した一致と複数ロールの返却を確認する。
func TestFixedRolesForEmail(t *testing.T) {
	mr := &MailboxResolution{Roles: []RoleResolution{
		{Role: domain.AssignmentRoleApprover, Method: MethodFixed, FixedUserEmails: []string{"Admin@X.com"}},
		{Role: domain.AssignmentRoleOwner, Method: MethodFixed, FixedUserEmails: []string{"admin@x.com", "other@x.com"}},
		{Role: domain.AssignmentRoleMember, Method: MethodUserAttribute, SourceAttribute: "memberOf"},
	}}
	roles := mr.FixedRolesForEmail("admin@x.com")
	if len(roles) != 2 {
		t.Fatalf("roles = %v, want [admin owner]", roles)
	}
	if roles[0] != domain.AssignmentRoleApprover || roles[1] != domain.AssignmentRoleOwner {
		t.Errorf("roles = %v", roles)
	}
	if got := mr.FixedRolesForEmail("nobody@x.com"); len(got) != 0 {
		t.Errorf("該当なしは空であるべき: %v", got)
	}
}
