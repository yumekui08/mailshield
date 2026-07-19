// Package dbconfig は api-server が publish した設定スナップショット（canonical JSON）を
// gateway 側でパースし、変数展開してパイプライン構築の入力にする（ADR 008 ③-2b）。
// api-server の domain.ConfigSnapshot と JSON 形が一致する（別モジュールのため型は二重定義）。
package dbconfig

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
)

// Snapshot は設定スナップショット全体。
type Snapshot struct {
	Variables       []Variable       `json:"variables"`
	WorkerInstances []WorkerInstance `json:"worker_instances"`
	Policies        []PolicyInstance `json:"policies"`
	Routings        []Routing        `json:"routings"`
}

type PolicyInstance struct {
	Alias   string `json:"alias"`
	Content string `json:"content"`
}

// PolicyByAlias は alias→ポリシー内容の索引を返す。
func (s *Snapshot) PolicyByAlias() map[string]string {
	m := make(map[string]string, len(s.Policies))
	for _, p := range s.Policies {
		m[p.Alias] = p.Content
	}
	return m
}

type Variable struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type WorkerInstance struct {
	Alias                 string         `json:"alias"`
	DisplayName           string         `json:"display_name"`
	WorkerType            string         `json:"worker_type"`
	Kind                  string         `json:"kind"` // inspect | transform
	Config                map[string]any `json:"config"`
	DefaultTimeoutSeconds int            `json:"default_timeout_seconds"`
	IsEnabled             bool           `json:"is_enabled"`
}

type Binding struct {
	Alias          string `json:"alias"`
	Enabled        bool   `json:"enabled"`
	TimeoutSeconds *int   `json:"timeout_seconds"`
}

type Routing struct {
	Name       string    `json:"name"`
	Priority   int       `json:"priority"`
	MatchExpr  string    `json:"match_expr"`
	Direction  string    `json:"direction"`
	IsCatchAll bool      `json:"is_catchall"`
	IsEnabled  bool      `json:"is_enabled"`
	PolicyRef  string    `json:"policy_ref"`
	Inspect    []Binding `json:"inspect"`
	Transform  []Binding `json:"transform"`
}

// Parse は canonical スナップショット JSON をデコードする。
func Parse(content []byte) (*Snapshot, error) {
	var s Snapshot
	if err := json.Unmarshal(content, &s); err != nil {
		return nil, fmt.Errorf("設定スナップショットのパース失敗: %w", err)
	}
	return &s, nil
}

var varRefPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// Expand は ${VAR} 参照を変数値に展開する（設定ロード時に 1 度だけ）。
// match_expr とワーカー設定内の文字列値を対象にする。未定義変数はそのまま残す
// （検証は api-server 側の publish で行われ、未定義参照は配布されない）。
func (s *Snapshot) Expand() {
	vars := make(map[string]string, len(s.Variables))
	for _, v := range s.Variables {
		vars[v.Key] = v.Value
	}
	repl := func(in string) string {
		return varRefPattern.ReplaceAllStringFunc(in, func(m string) string {
			name := varRefPattern.FindStringSubmatch(m)[1]
			if val, ok := vars[name]; ok {
				return val
			}
			return m
		})
	}
	for i := range s.Routings {
		s.Routings[i].MatchExpr = repl(s.Routings[i].MatchExpr)
	}
	for i := range s.WorkerInstances {
		s.WorkerInstances[i].Config = expandAny(s.WorkerInstances[i].Config, repl).(map[string]any)
	}
}

// expandAny は map / slice / string を再帰的に走査して文字列値を展開する。
func expandAny(v any, repl func(string) string) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = expandAny(val, repl)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = expandAny(val, repl)
		}
		return t
	case string:
		return repl(t)
	default:
		return v
	}
}

// instByAlias は alias→WorkerInstance の索引を返す。
func (s *Snapshot) instByAlias() map[string]WorkerInstance {
	m := make(map[string]WorkerInstance, len(s.WorkerInstances))
	for _, in := range s.WorkerInstances {
		m[in.Alias] = in
	}
	return m
}

// sortedRoutings は priority 昇順（同値は名前順）で有効なルーティングを返す。
func (s *Snapshot) sortedRoutings() []Routing {
	out := make([]Routing, 0, len(s.Routings))
	for _, rt := range s.Routings {
		if rt.IsEnabled {
			out = append(out, rt)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].Name < out[j].Name
	})
	return out
}
