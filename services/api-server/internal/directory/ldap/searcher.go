// Package ldap は LDAP ディレクトリ（Active Directory / OpenLDAP 等）からユーザーを
// 検索し、internal/directory.Provisioner を通して users テーブルへ同期する。
package ldap

// Entry は LDAP 検索結果 1 件分の属性を表す。
// go-ldap の具象型（*ldap.SearchResult 等）に Syncer を直接依存させないための
// 最小表現（コンシューマー側で定義するインターフェース原則に倣う）。
type Entry struct {
	DN string
	// Attributes は属性名 → 値（複数値対応。例: memberOf は複数の DN を持つ）。
	Attributes map[string][]string
}

// FirstAttr は指定属性の最初の値を返す。値がなければ空文字列を返す。
func (e Entry) FirstAttr(name string) string {
	vals := e.Attributes[name]
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

// Searcher は Syncer が必要とする LDAP 検索操作の最小集合。
// 実装は go-ldap の *ldap.Conn をラップする（conn.go の Dial を参照）。
// テストではフェイク実装に差し替える。
type Searcher interface {
	// SearchUsers は baseDN 配下で filter にマッチするエントリを attrs 属性つきで返す。
	// ページング（AD 等のサーバー側件数上限対策）は実装側の責務とする。
	SearchUsers(baseDN, filter string, attrs []string) ([]Entry, error)
	// Close は接続を解放する。
	Close() error
}
