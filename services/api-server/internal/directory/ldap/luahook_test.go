package ldap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

func writeLua(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "hook.lua")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestLuaHook_Resolve はユーザー属性から mailbox+role を返す Lua フックを検証する。
func TestLuaHook_Resolve(t *testing.T) {
	src := `
function resolve(user)
    local out = {}
    -- 個人メールボックス（属性 mail）
    for _, m in ipairs(user.attrs.mail or {}) do
        table.insert(out, { mailbox = m, role = "owner" })
    end
    -- memberOf の cn を member に（domain 補完は Lua 側で）
    for _, g in ipairs(user.attrs.memberOf or {}) do
        local cn = string.match(g, "^cn=([^,]+),ou=Groups")
        if cn then table.insert(out, { mailbox = cn .. "@internal.dev", role = "member" }) end
    end
    return out
end
`
	hook, err := NewLuaHook(writeLua(t, src), "")
	if err != nil {
		t.Fatal(err)
	}
	entry := Entry{DN: "uid=tanaka", Attributes: map[string][]string{
		"mail":     {"tanaka@internal.dev"},
		"memberOf": {"cn=sales,ou=Groups,dc=x", "cn=viewers,ou=AppRoles,dc=x"},
	}}
	tuples, err := hook.Resolve(entry)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, tp := range tuples {
		got[tp.MailboxEmail] = string(tp.Role)
	}
	if got["tanaka@internal.dev"] != "owner" {
		t.Errorf("owner が付かない: %v", got)
	}
	if got["sales@internal.dev"] != "member" {
		t.Errorf("member が付かない: %v", got)
	}
	if _, ok := got["viewers@internal.dev"]; ok {
		t.Errorf("AppRoles グループが誤って含まれた: %v", got)
	}
}

// TestLuaHook_DefaultRole はスクリプトが role を省略したとき、フックの既定ロールが使われることを確認する。
func TestLuaHook_DefaultRole(t *testing.T) {
	src := `function resolve(user) return { { mailbox = "x@internal.dev" } } end`
	hook, err := NewLuaHook(writeLua(t, src), domain.AssignmentRoleApprover)
	if err != nil {
		t.Fatal(err)
	}
	tuples, err := hook.Resolve(Entry{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tuples) != 1 || tuples[0].Role != domain.AssignmentRoleApprover {
		t.Errorf("既定ロールが使われていない: %+v", tuples)
	}
}
