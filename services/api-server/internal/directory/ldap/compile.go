package ldap

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/koizumib/mailshield/services/api-server/internal/domain"
)

// CompileChainRule は設定のステップ列（各要素は 1 キーのマップ）を検証・コンパイルして
// チェーン方式の RoleResolution を返す。auth パッケージから呼ばれる。
func CompileChainRule(role domain.AssignmentRole, rawSteps []map[string]any) (RoleResolution, error) {
	rr := RoleResolution{Role: role}
	if len(rawSteps) == 0 {
		return rr, fmt.Errorf("chain が空です")
	}
	steps := make([]chainStep, 0, len(rawSteps))
	for i, raw := range rawSteps {
		if len(raw) != 1 {
			return rr, fmt.Errorf("chain[%d]: ステップは 1 種類のキーのみ指定してください（%d 個指定された）", i, len(raw))
		}
		var kind string
		var val any
		for k, v := range raw {
			kind, val = k, v
		}
		step, err := compileStep(kind, val)
		if err != nil {
			return rr, fmt.Errorf("chain[%d] (%s): %w", i, kind, err)
		}
		steps = append(steps, step)
	}
	// 最初は self / const、最後は to_mailbox でなければならない。
	if _, ok := steps[0].(selfStep); !ok {
		if _, ok := steps[0].(constStep); !ok {
			return rr, fmt.Errorf("chain は self または const で始める必要があります")
		}
	}
	if _, ok := steps[len(steps)-1].(toMailboxStep); !ok {
		return rr, fmt.Errorf("chain は to_mailbox で終える必要があります")
	}
	rr.Chain = steps
	return rr, nil
}

// CompileLuaRule は Lua フック方式の RoleResolution を返す。
func CompileLuaRule(role domain.AssignmentRole, path string) (RoleResolution, error) {
	hook, err := NewLuaHook(path, role)
	if err != nil {
		return RoleResolution{}, err
	}
	return RoleResolution{Role: role, Lua: hook}, nil
}

// FixedRule は fixed 方式の RoleResolution を返す。
func FixedRule(role domain.AssignmentRole, emails []string) RoleResolution {
	return RoleResolution{Role: role, FixedUserEmails: emails}
}

func compileStep(kind string, val any) (chainStep, error) {
	switch kind {
	case "self":
		attr, ok := val.(string)
		if !ok || attr == "" {
			return nil, fmt.Errorf("self には属性名（文字列）を指定してください")
		}
		return selfStep{attr: attr}, nil

	case "const":
		vals, err := toStringSlice(val)
		if err != nil || len(vals) == 0 {
			return nil, fmt.Errorf("const には値のリストを指定してください")
		}
		return constStep{values: vals}, nil

	case "regex":
		pattern, ok := val.(string)
		if !ok || pattern == "" {
			return nil, fmt.Errorf("regex には正規表現（文字列）を指定してください")
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("正規表現のコンパイル失敗: %w", err)
		}
		return regexStep{re: re}, nil

	case "attr":
		name, ok := val.(string)
		if !ok || name == "" {
			return nil, fmt.Errorf("attr には属性名（文字列）を指定してください")
		}
		return attrStep{name: name}, nil

	case "search":
		m, err := toStringMap(val)
		if err != nil {
			return nil, fmt.Errorf("search には base_dn と filter を指定してください")
		}
		base := strings.TrimSpace(m["base_dn"])
		filter := strings.TrimSpace(m["filter"])
		if base == "" || filter == "" {
			return nil, fmt.Errorf("search には base_dn と filter の両方が必要です")
		}
		if !strings.Contains(filter, valuePlaceholder) {
			return nil, fmt.Errorf("search.filter には {value} プレースホルダが必要です: %q", filter)
		}
		var attrs []string
		if a := strings.TrimSpace(m["attrs"]); a != "" {
			for _, x := range strings.Split(a, ",") {
				if x = strings.TrimSpace(x); x != "" {
					attrs = append(attrs, x)
				}
			}
		}
		return searchStep{base: base, filter: filter, attrs: attrs}, nil

	case "to_mailbox":
		domainName := ""
		if val != nil {
			if m, err := toStringMap(val); err == nil {
				domainName = strings.TrimSpace(m["domain"])
			}
		}
		return toMailboxStep{domain: domainName}, nil

	default:
		return nil, fmt.Errorf("未知のステップ種別: %q（self | const | regex | search | attr | to_mailbox）", kind)
	}
}

// toStringSlice は any（string / []any / []string）を文字列スライスに変換する。
func toStringSlice(v any) ([]string, error) {
	switch t := v.(type) {
	case string:
		return []string{t}, nil
	case []string:
		return t, nil
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("文字列以外の要素が含まれています")
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("リストまたは文字列を指定してください")
	}
}

// toStringMap は any（map[string]any / map[any]any）を map[string]string に変換する。
func toStringMap(v any) (map[string]string, error) {
	out := map[string]string{}
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			out[k] = fmt.Sprintf("%v", val)
		}
	case map[any]any:
		for k, val := range t {
			out[fmt.Sprintf("%v", k)] = fmt.Sprintf("%v", val)
		}
	default:
		return nil, fmt.Errorf("マップを指定してください")
	}
	return out, nil
}
