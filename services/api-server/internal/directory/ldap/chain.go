package ldap

import (
	"fmt"
	"regexp"
	"strings"

	goldap "github.com/go-ldap/ldap/v3"
)

// 線形チェーンによるメールボックス解決。
//
// 1 ルール = ステップの列。各ステップは「現在の値（文字列）集合」または
// 「現在のエントリ集合」を次の状態へ変換する。分岐・ループ・変数束縛は持たない
// （＝チューリング完全にしない）。ステップ種別:
//
//	self <attr>            起点: ユーザーエントリの属性値を取り出す（"dn" は DN 自身）
//	const [v...]           起点: 固定値を注入する
//	regex <pattern>        値を正規表現で抽出・絞り込み（名前付き "value" / 先頭グループ / 全体）
//	search {base, filter}  値ごとに filter の {value} を（エスケープして）差し込み再検索 → エントリ
//	attr <name>            エントリから属性値を取り出す → 値
//	to_mailbox {domain?}   終端: 値をメールボックスアドレスに確定（@ が無ければ domain を補完）
//
// 値集合とエントリ集合は search / attr で交互に切り替わる。チェーンは必ず
// to_mailbox で終わる（コンパイル時に検証する）。

// stateKind は現在のパイプライン状態が値集合かエントリ集合かを表す。
type stateKind int

const (
	kindStart stateKind = iota // まだ何も生成していない（self / const のみ受け付ける）
	kindValues
	kindEntries
)

// pipeState はチェーン実行中の状態。
type pipeState struct {
	kind      stateKind
	values    []string
	entries   []Entry
	mailboxes []string // to_mailbox で確定したメールボックスアドレス（終端出力）
	done      bool     // to_mailbox に到達したか
}

// execCtx はチェーン実行のコンテキスト（ユーザーエントリと LDAP 検索手段）。
type execCtx struct {
	userEntry Entry
	searcher  Searcher
	cache     derefCache
}

// chainStep はチェーンの 1 ステップ。
type chainStep interface {
	apply(ec *execCtx, st *pipeState) error
}

// ─── 各ステップ ───────────────────────────────────────────────────────────────

type selfStep struct{ attr string }

func (s selfStep) apply(ec *execCtx, st *pipeState) error {
	if st.kind != kindStart {
		return fmt.Errorf("self は先頭ステップにのみ置けます")
	}
	if strings.EqualFold(s.attr, "dn") {
		st.values = []string{ec.userEntry.DN}
	} else {
		st.values = append([]string(nil), ec.userEntry.Attributes[s.attr]...)
	}
	st.kind = kindValues
	return nil
}

type constStep struct{ values []string }

func (s constStep) apply(_ *execCtx, st *pipeState) error {
	if st.kind == kindEntries {
		return fmt.Errorf("const はエントリ状態の後には置けません")
	}
	st.values = append(st.values, s.values...)
	st.kind = kindValues
	return nil
}

type regexStep struct{ re *regexp.Regexp }

func (s regexStep) apply(_ *execCtx, st *pipeState) error {
	if st.kind != kindValues {
		return fmt.Errorf("regex は値状態にのみ適用できます")
	}
	out := st.values[:0:0]
	for _, v := range st.values {
		if got, ok := applyTransform(s.re, v); ok {
			out = append(out, got)
		}
	}
	st.values = out
	return nil
}

type searchStep struct {
	base   string
	filter string // {value} を含む
	attrs  []string
}

func (s searchStep) apply(ec *execCtx, st *pipeState) error {
	if st.kind != kindValues {
		return fmt.Errorf("search は値状態にのみ適用できます")
	}
	var entries []Entry
	for _, v := range st.values {
		filter := strings.ReplaceAll(s.filter, valuePlaceholder, goldap.EscapeFilter(v))
		key := s.base + "\x00" + filter
		found, ok := ec.cache[key]
		if !ok {
			var err error
			found, err = ec.searcher.SearchUsers(s.base, filter, s.attrs)
			if err != nil {
				return fmt.Errorf("search 失敗 (base=%s, filter=%s): %w", s.base, filter, err)
			}
			ec.cache[key] = found
		}
		entries = append(entries, found...)
	}
	st.entries = entries
	st.values = nil
	st.kind = kindEntries
	return nil
}

type attrStep struct{ name string }

func (s attrStep) apply(_ *execCtx, st *pipeState) error {
	if st.kind != kindEntries {
		return fmt.Errorf("attr はエントリ状態にのみ適用できます（search の後に置いてください）")
	}
	var out []string
	for _, e := range st.entries {
		out = append(out, e.Attributes[s.name]...)
	}
	st.values = out
	st.entries = nil
	st.kind = kindValues
	return nil
}

type toMailboxStep struct{ domain string }

func (s toMailboxStep) apply(_ *execCtx, st *pipeState) error {
	if st.kind != kindValues {
		return fmt.Errorf("to_mailbox は値状態にのみ適用できます")
	}
	for _, v := range st.values {
		if email, ok := finalizeMailboxEmail(v, s.domain); ok {
			st.mailboxes = append(st.mailboxes, email)
		}
	}
	st.done = true
	return nil
}

// finalizeMailboxEmail は値をメールボックスアドレスに確定する。
// domain が非空で値に "@" が無ければ "値@domain" に補完する。
func finalizeMailboxEmail(value, domain string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if domain != "" && !strings.Contains(value, "@") {
		value = value + "@" + domain
	}
	return value, true
}

// runChain はコンパイル済みのチェーンを 1 ユーザーについて実行し、確定した
// メールボックスアドレス一覧を返す。search 失敗などはエラーとして返し、
// 呼び出し側（ResolveForUser）がそのルールをスキップする。
func runChain(steps []chainStep, ec *execCtx) ([]string, error) {
	st := &pipeState{kind: kindStart}
	for _, step := range steps {
		if err := step.apply(ec, st); err != nil {
			return nil, err
		}
	}
	if !st.done {
		return nil, fmt.Errorf("チェーンが to_mailbox で終わっていません")
	}
	return st.mailboxes, nil
}
