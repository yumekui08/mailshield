package ldap

import (
	"log/slog"
	"regexp"
	"strings"

	goldap "github.com/go-ldap/ldap/v3"

	"github.com/koizumib/mailshield/services/api-server/internal/directory"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

// valuePlaceholder は search ステップの filter 内で前段の値に置換されるプレースホルダ。
const valuePlaceholder = "{value}"

// RoleResolution は 1 ルール分のコンパイル済み解決設定。
// Chain / Lua / FixedUserEmails のいずれか 1 つだけが設定される。
type RoleResolution struct {
	Role domain.AssignmentRole

	// Chain は線形チェーン（ユーザーの属性からメールボックスを導出）。
	Chain []chainStep

	// Lua はチェーンで表現できない変則ケース用のフック。
	Lua *LuaHook

	// FixedUserEmails は「この同期ソースが管理する全メールボックス × このロール」を
	// 付与するユーザーのメールアドレス（比較は大文字小文字を無視）。
	// 全メールボックスの集合に依存するため ResolveForUser では解決せず、
	// FixedRolesForEmail + 呼び出し側の 2 パス処理で反映する。
	FixedUserEmails []string
}

// isChain はチェーン方式のルールかを返す。
func (r *RoleResolution) isChain() bool { return len(r.Chain) > 0 }

// isFixed は fixed 方式のルールかを返す。
func (r *RoleResolution) isFixed() bool { return len(r.FixedUserEmails) > 0 }

// MailboxResolution は全ルール分の解決設定。
type MailboxResolution struct {
	Roles []RoleResolution
}

// Empty は解決設定が 1 つも無い（メールボックス自動反映が無効）ことを返す。
func (m *MailboxResolution) Empty() bool {
	return m == nil || len(m.Roles) == 0
}

// FixedRolesForEmail は fixed 方式で email に付与されるロール一覧を返す。
func (m *MailboxResolution) FixedRolesForEmail(email string) []domain.AssignmentRole {
	if m == nil {
		return nil
	}
	var roles []domain.AssignmentRole
	for _, r := range m.Roles {
		if !r.isFixed() {
			continue
		}
		for _, fixed := range r.FixedUserEmails {
			if strings.EqualFold(fixed, email) {
				roles = append(roles, r.Role)
				break
			}
		}
	}
	return roles
}

// derefCache は同期サイクル内の dereference 検索結果キャッシュ。
// 多数のユーザーが同じグループを共有するため、(base_dn, filter) 単位のメモ化で
// 実クエリ数を「ユニークなグループ数」まで削減する（N+1 対策）。
type derefCache map[string][]Entry

// NewDerefCache は 1 同期サイクル（または 1 ログイン処理）分のキャッシュを返す。
func NewDerefCache() derefCache {
	return make(derefCache)
}

// ResolveForUser は chain / lua 方式の全ルールを userEntry について実行し、
// 割り当てタプルを返す（fixed 方式は FixedRolesForEmail + 呼び出し側の 2 パスで処理する）。
//
// いずれかのルールが失敗（search 失敗・lua エラー）した場合は解決できたタプルと
// 併せて error を返す。呼び出し側は error 時にそのユーザーの reconcile をスキップする
// （不完全なタプルで上書きすると、正当な既存割り当てを誤削除しうるため）。
func (m *MailboxResolution) ResolveForUser(searcher Searcher, userEntry Entry, cache derefCache) ([]directory.MailboxAssignmentTuple, error) {
	if m == nil {
		return nil, nil
	}
	ec := &execCtx{userEntry: userEntry, searcher: searcher, cache: cache}
	var tuples []directory.MailboxAssignmentTuple
	var firstErr error
	for i := range m.Roles {
		r := &m.Roles[i]
		switch {
		case r.isChain():
			emails, err := runChain(r.Chain, ec)
			if err != nil {
				slog.Warn("メールボックス解決: チェーン実行失敗", "role", r.Role, "dn", userEntry.DN, "error", err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			for _, email := range emails {
				tuples = append(tuples, directory.MailboxAssignmentTuple{MailboxEmail: email, Role: r.Role})
			}
		case r.Lua != nil:
			lts, err := r.Lua.Resolve(userEntry)
			if err != nil {
				slog.Warn("メールボックス解決: lua フック失敗", "role", r.Role, "dn", userEntry.DN, "error", err)
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			tuples = append(tuples, lts...)
		}
	}
	return tuples, firstErr
}

// applyTransform は正規表現変換を適用する。re が nil なら値をそのまま返す。
// マッチしない場合は false（値のスキップ）。抽出値は、名前付きグループ "value" が
// あればその値、無ければ最初のキャプチャグループ、キャプチャが無ければマッチ全体。
func applyTransform(re *regexp.Regexp, s string) (string, bool) {
	if re == nil {
		return s, true
	}
	match := re.FindStringSubmatch(s)
	if match == nil {
		return "", false
	}
	for i, name := range re.SubexpNames() {
		if name == "value" {
			return match[i], true
		}
	}
	if len(match) > 1 {
		return match[1], true
	}
	return match[0], true
}

// NormalizeDN は DN を比較用に正規化する（パース後の正規形を小文字化）。
// group_search の member 属性と、ユーザーエントリの DN を突き合わせる際の
// 表記ゆれ（大文字小文字・空白）を吸収する。パース失敗時は小文字化のみ行う。
func NormalizeDN(dn string) string {
	parsed, err := goldap.ParseDN(dn)
	if err != nil {
		return strings.ToLower(strings.TrimSpace(dn))
	}
	return strings.ToLower(parsed.String())
}
