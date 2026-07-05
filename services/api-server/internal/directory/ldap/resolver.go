package ldap

import (
	"fmt"
	"regexp"
	"strings"

	goldap "github.com/go-ldap/ldap/v3"

	"github.com/koizumib/mailshield/services/api-server/internal/directory"
	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

// 解決方式の識別子。設定ファイルの method フィールドの値に対応する。
const (
	MethodUserAttribute = "user_attribute"
	MethodGroupSearch   = "group_search"
	MethodFixed         = "fixed"
)

// DereferenceRule は user_attribute 方式の再検索（最大1回）ルール。
type DereferenceRule struct {
	BaseDN string
	// Filter は "{value}" プレースホルダを含む LDAP フィルタ。
	// 埋め込み時に前段の値は必ず goldap.EscapeFilter でエスケープされる
	// （LDAP インジェクション対策。設定で無効化する手段は提供しない）。
	Filter string
}

// valuePlaceholder は DereferenceRule.Filter 内で前段の値に置換されるプレースホルダ。
const valuePlaceholder = "{value}"

// RoleResolution は 1 ロール分のコンパイル済み解決設定。
// Method に応じて対応するフィールド群だけが使われる。
type RoleResolution struct {
	Role   domain.AssignmentRole
	Method string

	// ─── user_attribute ───
	SourceAttribute string
	SourceTransform *regexp.Regexp // nil なら変換なし。マッチしない値はスキップ（フィルタを兼ねる）
	Dereference     *DereferenceRule
	TargetAttribute string
	TargetTransform *regexp.Regexp
	MailboxDomain   string

	// ─── group_search ───
	BaseDN     string
	Filter     string
	MemberAttr string

	// ─── fixed ───
	// FixedUserEmails はこのロールを常に付与するユーザーのメールアドレス（比較は大文字小文字を無視）。
	FixedUserEmails []string
}

// MailboxResolution は全ロール分の解決設定。
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
		if r.Method != MethodFixed {
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

// ResolveUserAttribute は user_attribute 方式の全ロールについて、
// userEntry から解決したタプルを返す。
// 有界パイプライン: source_attribute → source_transform? → dereference?（最大1回）
// → target_attribute → target_transform? → mailbox_domain 補完。
// 個々の値の解決失敗（transform 不一致・dereference 0件）はスキップであってエラーではない。
func (m *MailboxResolution) ResolveUserAttribute(searcher Searcher, userEntry Entry, cache derefCache) []directory.MailboxAssignmentTuple {
	if m == nil {
		return nil
	}
	var tuples []directory.MailboxAssignmentTuple
	for _, r := range m.Roles {
		if r.Method != MethodUserAttribute {
			continue
		}
		for _, raw := range userEntry.Attributes[r.SourceAttribute] {
			value, ok := applyTransform(r.SourceTransform, raw)
			if !ok {
				continue
			}

			if r.Dereference == nil {
				if email, ok := r.finalizeMailboxEmail(value); ok {
					tuples = append(tuples, directory.MailboxAssignmentTuple{MailboxEmail: email, Role: r.Role})
				}
				continue
			}

			entries, err := r.Dereference.search(searcher, value, []string{r.TargetAttribute}, cache)
			if err != nil {
				// 検索エラーはこの値のスキップに留める（1件の不整合が同期全体を止めないように）。
				// エラー自体は search 内でログ済み。
				continue
			}
			for _, entry := range entries {
				for _, tv := range entry.Attributes[r.TargetAttribute] {
					target, ok := applyTransform(r.TargetTransform, tv)
					if !ok {
						continue
					}
					if email, ok := r.finalizeMailboxEmail(target); ok {
						tuples = append(tuples, directory.MailboxAssignmentTuple{MailboxEmail: email, Role: r.Role})
					}
				}
			}
		}
	}
	return tuples
}

// search はプレースホルダを強制エスケープつきで置換して再検索する。キャッシュがあればそれを返す。
func (d *DereferenceRule) search(searcher Searcher, value string, attrs []string, cache derefCache) ([]Entry, error) {
	filter := strings.ReplaceAll(d.Filter, valuePlaceholder, goldap.EscapeFilter(value))
	key := d.BaseDN + "\x00" + filter
	if entries, ok := cache[key]; ok {
		return entries, nil
	}
	entries, err := searcher.SearchUsers(d.BaseDN, filter, attrs)
	if err != nil {
		return nil, fmt.Errorf("dereference 検索失敗 (base_dn=%s, filter=%s): %w", d.BaseDN, filter, err)
	}
	cache[key] = entries
	return entries, nil
}

// ResolveGroupSearchAll は group_search 方式の全ロールについて一括検索を行い、
// 「正規化済みメンバー DN → タプル一覧」を返す（定期同期用。1 ロールにつき検索1回で完結する）。
func (m *MailboxResolution) ResolveGroupSearchAll(searcher Searcher) (map[string][]directory.MailboxAssignmentTuple, error) {
	if m == nil {
		return nil, nil
	}
	result := make(map[string][]directory.MailboxAssignmentTuple)
	for _, r := range m.Roles {
		if r.Method != MethodGroupSearch {
			continue
		}
		groups, err := searcher.SearchUsers(r.BaseDN, r.Filter, []string{r.MemberAttr, r.TargetAttribute})
		if err != nil {
			return nil, fmt.Errorf("group_search 検索失敗 (role=%s, base_dn=%s): %w", r.Role, r.BaseDN, err)
		}
		for _, g := range groups {
			email, ok := r.mailboxEmailFromGroupEntry(g)
			if !ok {
				continue
			}
			for _, memberDN := range g.Attributes[r.MemberAttr] {
				key := NormalizeDN(memberDN)
				result[key] = append(result[key], directory.MailboxAssignmentTuple{MailboxEmail: email, Role: r.Role})
			}
		}
	}
	return result, nil
}

// ResolveGroupSearchForUser は group_search 方式の全ロールについて、
// userDN がメンバーであるグループだけを絞り込み検索して解決する（JIT ログイン用）。
// フィルタに (&(元filter)(member_attr=userDN)) を組み立てるため、1 ロールにつき検索1回で済む。
func (m *MailboxResolution) ResolveGroupSearchForUser(searcher Searcher, userDN string) []directory.MailboxAssignmentTuple {
	if m == nil {
		return nil
	}
	var tuples []directory.MailboxAssignmentTuple
	for _, r := range m.Roles {
		if r.Method != MethodGroupSearch {
			continue
		}
		filter := fmt.Sprintf("(&%s(%s=%s))", r.Filter, r.MemberAttr, goldap.EscapeFilter(userDN))
		groups, err := searcher.SearchUsers(r.BaseDN, filter, []string{r.TargetAttribute})
		if err != nil {
			// JIT ではエラーをスキップに留める（ログインは成功させ、次回同期で回復する）
			continue
		}
		for _, g := range groups {
			if email, ok := r.mailboxEmailFromGroupEntry(g); ok {
				tuples = append(tuples, directory.MailboxAssignmentTuple{MailboxEmail: email, Role: r.Role})
			}
		}
	}
	return tuples
}

// mailboxEmailFromGroupEntry はグループエントリ自身の target_attribute から
// メールボックスアドレスを取り出す（最初に解決できた値を採用する）。
func (r *RoleResolution) mailboxEmailFromGroupEntry(g Entry) (string, bool) {
	for _, tv := range g.Attributes[r.TargetAttribute] {
		if target, ok := applyTransform(r.TargetTransform, tv); ok {
			if email, ok := r.finalizeMailboxEmail(target); ok {
				return email, true
			}
		}
	}
	return "", false
}

// finalizeMailboxEmail は解決済みの値をメールボックスアドレスに確定する。
// mailbox_domain が設定されており値に "@" が無ければドメインを補完する。
func (r *RoleResolution) finalizeMailboxEmail(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if r.MailboxDomain != "" && !strings.Contains(value, "@") {
		value = value + "@" + r.MailboxDomain
	}
	return value, true
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
