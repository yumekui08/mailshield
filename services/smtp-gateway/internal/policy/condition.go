package policy

import (
	"fmt"
	"strconv"
	"strings"
)

// 条件式の評価。
//
// サポートする構文（1 行・左結合の AND のみ。OR は複数ルールで表現する）:
//
//	true / false
//	{key} == {value}            等値（ブールは大文字小文字を無視）
//	{key} != {value}            不等
//	{key} >= {int}              数値比較（>= > <= <）
//	{key} contains {substr}     部分文字列（大文字小文字を無視）
//	{key} in_list {listname}    lists で定義した集合に {key} の値（またはドメイン部）が含まれる
//	A && B && ...               上記の論理積
//
// {key} は buildFacts が組み立てた fact 名（例: "av-worker.detected"、"mail.from"、
// "mail.from_domain"、"total_score"）。
type evalContext struct {
	facts map[string]any
	// lists は名前付き集合（すべて小文字で保持）。
	lists map[string]map[string]bool
}

// evalCondition は AND で連結された条件式を評価する。
func evalCondition(condition string, ctx evalContext) (bool, error) {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return false, nil
	}
	if condition == "true" {
		return true, nil
	}
	if condition == "false" {
		return false, nil
	}

	for _, clause := range splitAND(condition) {
		ok, err := evalClause(strings.TrimSpace(clause), ctx)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// splitAND は "&&" で条件を分割する（文字列リテラル内の && は想定しない）。
func splitAND(condition string) []string {
	return strings.Split(condition, "&&")
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
