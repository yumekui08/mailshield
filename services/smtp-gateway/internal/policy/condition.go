package policy

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// 条件式の評価。
//
// サポートする構文（1 行。文字列リテラル内に構造トークン && || ( ) は想定しない）:
//
//	true / false
//	{key} == {value}            等値（ブールは大文字小文字を無視）
//	{key} != {value}            不等
//	{key} >= {int}              数値比較（>= > <= <）
//	{key} contains {substr}     部分文字列（大文字小文字を無視）
//	{key} in_list {listname}    lists で定義した集合に {key} の値（またはドメイン部）が含まれる
//	A && B                      論理積（AND）
//	A || B                      論理和（OR）
//	not X / not (A && B)        否定
//	(A || B) && C               グルーピング
//
// 演算子の優先順位（高い順）: not > && > ||。括弧で上書きできる。
// {key} は buildFacts が組み立てた fact 名（例: "av-worker.detected"、"mail.from"、
// "mail.from_domain"、"total_score"）。
type evalContext struct {
	facts map[string]any
	// lists は名前付き集合（すべて小文字で保持）。
	lists map[string]map[string]bool
}

// evalCondition は論理式（AND / OR / NOT / 括弧）を評価する。
func evalCondition(condition string, ctx evalContext) (bool, error) {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return false, nil
	}
	tokens, err := tokenizeCondition(condition)
	if err != nil {
		return false, err
	}
	p := &condParser{tokens: tokens}
	result, err := p.parseOr(ctx)
	if err != nil {
		return false, err
	}
	if p.pos != len(p.tokens) {
		return false, fmt.Errorf("条件式の解析に失敗（余分なトークン）: %s", condition)
	}
	return result, nil
}

// ─── 条件式パーサ（再帰下降） ──────────────────────────────────────────────

type condTokenKind int

const (
	tokLeaf condTokenKind = iota
	tokAnd
	tokOr
	tokNot
	tokLParen
	tokRParen
)

type condToken struct {
	kind condTokenKind
	text string // tokLeaf のときの比較式
}

// tokenizeCondition は構造トークン（&& || ( )）で分割し、前置 not を切り出す。
func tokenizeCondition(s string) ([]condToken, error) {
	var raw []condToken
	var buf strings.Builder
	flush := func() {
		if t := strings.TrimSpace(buf.String()); t != "" {
			raw = append(raw, condToken{kind: tokLeaf, text: t})
		}
		buf.Reset()
	}
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "&&"):
			flush()
			raw = append(raw, condToken{kind: tokAnd})
			i += 2
		case strings.HasPrefix(s[i:], "||"):
			flush()
			raw = append(raw, condToken{kind: tokOr})
			i += 2
		case s[i] == '(':
			flush()
			raw = append(raw, condToken{kind: tokLParen})
			i++
		case s[i] == ')':
			flush()
			raw = append(raw, condToken{kind: tokRParen})
			i++
		default:
			buf.WriteByte(s[i])
			i++
		}
	}
	flush()

	// 前置 not をリーフから切り出す（"not" 単独、または "not " で始まるリーフ）。
	var tokens []condToken
	for _, t := range raw {
		if t.kind == tokLeaf {
			lower := strings.ToLower(t.text)
			if lower == "not" {
				tokens = append(tokens, condToken{kind: tokNot})
				continue
			}
			if strings.HasPrefix(lower, "not ") {
				tokens = append(tokens, condToken{kind: tokNot})
				tokens = append(tokens, condToken{kind: tokLeaf, text: strings.TrimSpace(t.text[4:])})
				continue
			}
		}
		tokens = append(tokens, t)
	}
	return tokens, nil
}

type condParser struct {
	tokens []condToken
	pos    int
}

func (p *condParser) peek() (condToken, bool) {
	if p.pos < len(p.tokens) {
		return p.tokens[p.pos], true
	}
	return condToken{}, false
}

func (p *condParser) parseOr(ctx evalContext) (bool, error) {
	result, err := p.parseAnd(ctx)
	if err != nil {
		return false, err
	}
	for {
		t, ok := p.peek()
		if !ok || t.kind != tokOr {
			break
		}
		p.pos++
		rhs, err := p.parseAnd(ctx)
		if err != nil {
			return false, err
		}
		result = result || rhs
	}
	return result, nil
}

func (p *condParser) parseAnd(ctx evalContext) (bool, error) {
	result, err := p.parseUnary(ctx)
	if err != nil {
		return false, err
	}
	for {
		t, ok := p.peek()
		if !ok || t.kind != tokAnd {
			break
		}
		p.pos++
		rhs, err := p.parseUnary(ctx)
		if err != nil {
			return false, err
		}
		result = result && rhs
	}
	return result, nil
}

func (p *condParser) parseUnary(ctx evalContext) (bool, error) {
	t, ok := p.peek()
	if ok && t.kind == tokNot {
		p.pos++
		v, err := p.parseUnary(ctx)
		if err != nil {
			return false, err
		}
		return !v, nil
	}
	return p.parsePrimary(ctx)
}

func (p *condParser) parsePrimary(ctx evalContext) (bool, error) {
	t, ok := p.peek()
	if !ok {
		return false, fmt.Errorf("条件式が途中で終了しました")
	}
	switch t.kind {
	case tokLParen:
		p.pos++
		v, err := p.parseOr(ctx)
		if err != nil {
			return false, err
		}
		closing, ok := p.peek()
		if !ok || closing.kind != tokRParen {
			return false, fmt.Errorf("閉じ括弧が見つかりません")
		}
		p.pos++
		return v, nil
	case tokLeaf:
		p.pos++
		return evalLeaf(t.text, ctx)
	default:
		return false, fmt.Errorf("予期しないトークンです（条件式の構文エラー）")
	}
}

// evalLeaf は単一の比較式（true/false 定数を含む）を評価する。
func evalLeaf(clause string, ctx evalContext) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(clause)) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return evalClause(clause, ctx)
}

// leafOperators は比較式で認識する演算子トークン（長いものから先に評価する）。
var leafOperators = []string{" contains ", " starts_with ", " ends_with ", " matches ", " in_list ", " == ", " != ", " >= ", " <= ", " > ", " < "}

// ValidateCondition は条件式の構文（括弧の対応・演算子の有無・数値比較の右辺）を検証する。
// fact やリストの存在（意味論）は検証しない。ポリシー読み込み時に不正な式を早期に弾く用途。
func ValidateCondition(condition string) error {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return fmt.Errorf("条件式が空です")
	}
	tokens, err := tokenizeCondition(condition)
	if err != nil {
		return err
	}
	p := &condParser{tokens: tokens}
	if err := p.validateOr(); err != nil {
		return err
	}
	if p.pos != len(p.tokens) {
		return fmt.Errorf("条件式の構文エラー（余分なトークン）: %s", condition)
	}
	return nil
}

func (p *condParser) validateOr() error {
	if err := p.validateAnd(); err != nil {
		return err
	}
	for {
		t, ok := p.peek()
		if !ok || t.kind != tokOr {
			break
		}
		p.pos++
		if err := p.validateAnd(); err != nil {
			return err
		}
	}
	return nil
}

func (p *condParser) validateAnd() error {
	if err := p.validateUnary(); err != nil {
		return err
	}
	for {
		t, ok := p.peek()
		if !ok || t.kind != tokAnd {
			break
		}
		p.pos++
		if err := p.validateUnary(); err != nil {
			return err
		}
	}
	return nil
}

func (p *condParser) validateUnary() error {
	t, ok := p.peek()
	if ok && t.kind == tokNot {
		p.pos++
		return p.validateUnary()
	}
	return p.validatePrimary()
}

func (p *condParser) validatePrimary() error {
	t, ok := p.peek()
	if !ok {
		return fmt.Errorf("条件式が途中で終了しました")
	}
	switch t.kind {
	case tokLParen:
		p.pos++
		if err := p.validateOr(); err != nil {
			return err
		}
		closing, ok := p.peek()
		if !ok || closing.kind != tokRParen {
			return fmt.Errorf("閉じ括弧が見つかりません")
		}
		p.pos++
		return nil
	case tokLeaf:
		p.pos++
		return validateLeafSyntax(t.text)
	default:
		return fmt.Errorf("予期しないトークンです（条件式の構文エラー）")
	}
}

// validateLeafSyntax は単一比較式の構文を検証する（演算子の有無・数値比較の右辺）。
func validateLeafSyntax(clause string) error {
	switch strings.ToLower(strings.TrimSpace(clause)) {
	case "true", "false":
		return nil
	}
	for _, op := range leafOperators {
		if idx := strings.Index(clause, op); idx >= 0 {
			key := strings.TrimSpace(clause[:idx])
			val := strings.TrimSpace(clause[idx+len(op):])
			if key == "" {
				return fmt.Errorf("条件式の左辺が空です: %q", clause)
			}
			if val == "" {
				return fmt.Errorf("条件式の右辺が空です: %q", clause)
			}
			switch strings.TrimSpace(op) {
			case ">=", "<=", ">", "<":
				if _, err := strconv.Atoi(val); err != nil {
					return fmt.Errorf("数値比較の右辺が整数ではありません (%q): %q", val, clause)
				}
			case "matches":
				if _, err := regexp.Compile(strings.Trim(val, `"`)); err != nil {
					return fmt.Errorf("正規表現が不正です (%q): %w", val, err)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("未対応の条件式（演算子が見つかりません）: %q", clause)
}

// evalClause は単一の比較式を評価する。
func evalClause(clause string, ctx evalContext) (bool, error) {
	// 演算子は長いものから順に試す（">=" を ">" より先に）
	type op struct {
		token string
		eval  func(key, val string, ctx evalContext) (bool, error)
	}
	ops := []op{
		{" contains ", evalContains},
		{" starts_with ", evalStartsWith},
		{" ends_with ", evalEndsWith},
		{" matches ", evalMatches},
		{" in_list ", evalInList},
		{" == ", evalEquals},
		{" != ", evalNotEquals},
		{" >= ", func(k, v string, c evalContext) (bool, error) { return evalNum(k, v, c, ">=") }},
		{" <= ", func(k, v string, c evalContext) (bool, error) { return evalNum(k, v, c, "<=") }},
		{" > ", func(k, v string, c evalContext) (bool, error) { return evalNum(k, v, c, ">") }},
		{" < ", func(k, v string, c evalContext) (bool, error) { return evalNum(k, v, c, "<") }},
	}
	for _, o := range ops {
		if idx := strings.Index(clause, o.token); idx >= 0 {
			key := strings.TrimSpace(clause[:idx])
			val := strings.TrimSpace(clause[idx+len(o.token):])
			return o.eval(key, val, ctx)
		}
	}
	return false, fmt.Errorf("未対応の条件式: %s", clause)
}

func evalEquals(key, val string, ctx evalContext) (bool, error) {
	fact, ok := ctx.facts[key]
	if !ok {
		return false, nil
	}
	return strings.EqualFold(fmt.Sprintf("%v", fact), val), nil
}

func evalNotEquals(key, val string, ctx evalContext) (bool, error) {
	fact, ok := ctx.facts[key]
	if !ok {
		return true, nil // 未定義 fact は「その値ではない」
	}
	return !strings.EqualFold(fmt.Sprintf("%v", fact), val), nil
}

func evalStartsWith(key, prefix string, ctx evalContext) (bool, error) {
	fact, ok := ctx.facts[key]
	if !ok {
		return false, nil
	}
	return strings.HasPrefix(
		strings.ToLower(fmt.Sprintf("%v", fact)),
		strings.ToLower(strings.Trim(prefix, `"`)),
	), nil
}

func evalEndsWith(key, suffix string, ctx evalContext) (bool, error) {
	fact, ok := ctx.facts[key]
	if !ok {
		return false, nil
	}
	return strings.HasSuffix(
		strings.ToLower(fmt.Sprintf("%v", fact)),
		strings.ToLower(strings.Trim(suffix, `"`)),
	), nil
}

// evalMatches は fact 値が正規表現にマッチするかを返す（Go RE2 構文）。
func evalMatches(key, pattern string, ctx evalContext) (bool, error) {
	fact, ok := ctx.facts[key]
	if !ok {
		return false, nil
	}
	re, err := regexp.Compile(strings.Trim(pattern, `"`))
	if err != nil {
		return false, fmt.Errorf("正規表現が不正です (%q): %w", pattern, err)
	}
	return re.MatchString(fmt.Sprintf("%v", fact)), nil
}

func evalContains(key, substr string, ctx evalContext) (bool, error) {
	fact, ok := ctx.facts[key]
	if !ok {
		return false, nil
	}
	return strings.Contains(
		strings.ToLower(fmt.Sprintf("%v", fact)),
		strings.ToLower(strings.Trim(substr, `"`)),
	), nil
}

// evalInList は fact の値が名前付きリストに含まれるかを返す。
// fact 値がカンマ連結（mail.to / mail.to_domains のように複数宛先）の場合は
// 各要素を個別に照合し、いずれか 1 つでも一致すれば true を返す。
// 各要素が "@" を含む（メールアドレス）場合はドメイン部でも照合する。
func evalInList(key, listName string, ctx evalContext) (bool, error) {
	set, ok := ctx.lists[strings.ToLower(listName)]
	if !ok {
		return false, fmt.Errorf("未定義のリスト: %s", listName)
	}
	fact, ok := ctx.facts[key]
	if !ok {
		return false, nil
	}
	value := strings.ToLower(fmt.Sprintf("%v", fact))
	for _, elem := range strings.Split(value, ",") {
		elem = strings.TrimSpace(elem)
		if elem == "" {
			continue
		}
		if set[elem] {
			return true, nil
		}
		if at := strings.LastIndex(elem, "@"); at >= 0 && set[elem[at+1:]] {
			return true, nil
		}
	}
	return false, nil
}

func evalNum(key, val string, ctx evalContext, cmp string) (bool, error) {
	threshold, err := strconv.Atoi(strings.TrimSpace(val))
	if err != nil {
		return false, fmt.Errorf("数値比較の右辺が整数でない (%s): %w", val, err)
	}
	fact, ok := ctx.facts[key]
	if !ok {
		return false, nil
	}
	var n int
	switch v := fact.(type) {
	case int:
		n = v
	case int64:
		n = int(v)
	case float64:
		n = int(v)
	default:
		return false, nil
	}
	switch cmp {
	case ">=":
		return n >= threshold, nil
	case "<=":
		return n <= threshold, nil
	case ">":
		return n > threshold, nil
	case "<":
		return n < threshold, nil
	}
	return false, nil
}
