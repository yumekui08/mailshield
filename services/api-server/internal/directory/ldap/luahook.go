package ldap

import (
	"fmt"
	"os"

	glua "github.com/yuin/gopher-lua"

	"github.com/koizumib/mailshield/services/api-server/internal/directory"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

// LuaHook はチェーンで表現できない変則ディレクトリ向けの escape hatch。
// スクリプト内の resolve(user) 関数がユーザー属性テーブルを受け取り、
// { {mailbox=..., role=...}, ... } を返す。LDAP 検索はできない（ユーザー自身の
// 属性からの導出に限る。分岐/ループを含む複雑なパースが必要な場合に使う）。
type LuaHook struct {
	role   domain.AssignmentRole // 省略時はスクリプトが各要素で role を返す
	source string                // Lua スクリプト本文
	path   string                // エラーメッセージ用
}

// NewLuaHook は Lua スクリプトファイルを読み込んで LuaHook を返す。
func NewLuaHook(path string, role domain.AssignmentRole) (*LuaHook, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("lua フック読み込み失敗 (%s): %w", path, err)
	}
	return &LuaHook{role: role, source: string(src), path: path}, nil
}

// Resolve は userEntry に対してスクリプトを実行し、割り当てタプルを返す。
// スクリプトは毎回新しい LState で実行する（並行安全・状態を持ち越さない）。
func (h *LuaHook) Resolve(userEntry Entry) ([]directory.MailboxAssignmentTuple, error) {
	L := glua.NewState()
	defer L.Close()

	if err := L.DoString(h.source); err != nil {
		return nil, fmt.Errorf("lua フック実行失敗 (%s): %w", h.path, err)
	}
	fn := L.GetGlobal("resolve")
	if fn.Type() != glua.LTFunction {
		return nil, fmt.Errorf("lua フック (%s): resolve 関数が定義されていません", h.path)
	}

	L.Push(fn)
	L.Push(userEntryToLua(L, userEntry))
	if err := L.PCall(1, 1, nil); err != nil {
		return nil, fmt.Errorf("lua フック resolve() 呼び出し失敗 (%s): %w", h.path, err)
	}

	ret := L.Get(-1)
	L.Pop(1)
	tbl, ok := ret.(*glua.LTable)
	if !ok {
		return nil, fmt.Errorf("lua フック (%s): resolve はテーブルを返す必要があります", h.path)
	}

	var tuples []directory.MailboxAssignmentTuple
	tbl.ForEach(func(_, v glua.LValue) {
		item, ok := v.(*glua.LTable)
		if !ok {
			return
		}
		mailbox := lStr(item.RawGetString("mailbox"))
		if mailbox == "" {
			return
		}
		role := h.role
		if r := lStr(item.RawGetString("role")); r != "" {
			role = domain.AssignmentRole(r)
		}
		if role == "" {
			return
		}
		tuples = append(tuples, directory.MailboxAssignmentTuple{
			MailboxEmail:       mailbox,
			MailboxDisplayName: lStr(item.RawGetString("display_name")),
			Role:               role,
		})
	})
	return tuples, nil
}

// userEntryToLua は Entry を Lua テーブル { dn=..., attrs={ name={v1,v2}, ... } } に変換する。
func userEntryToLua(L *glua.LState, e Entry) *glua.LTable {
	t := L.NewTable()
	t.RawSetString("dn", glua.LString(e.DN))
	attrs := L.NewTable()
	for name, vals := range e.Attributes {
		arr := L.NewTable()
		for i, v := range vals {
			arr.RawSetInt(i+1, glua.LString(v))
		}
		attrs.RawSetString(name, arr)
	}
	t.RawSetString("attrs", attrs)
	return t
}

func lStr(v glua.LValue) string {
	if s, ok := v.(glua.LString); ok {
		return string(s)
	}
	return ""
}
