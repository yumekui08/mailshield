// Package policyfile は smtp-gateway が読む routes.d/<route>/policy.yaml を
// api-server 側から読み書き・検証するためのモデルとファイル IO を提供する。
// smtp-gateway とは別モジュールのため、policy 構造は最小限で二重定義する。
package policyfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ActionSpec は 1 アクションとそのパラメータ（actions: リスト要素）。
type ActionSpec struct {
	Type         string `yaml:"type" json:"type"`
	Destination  string `yaml:"destination,omitempty" json:"destination,omitempty"`
	DelayMinutes int    `yaml:"delay_minutes,omitempty" json:"delay_minutes,omitempty"`
	Name         string `yaml:"name,omitempty" json:"name,omitempty"`
	Value        string `yaml:"value,omitempty" json:"value,omitempty"`
}

// Rule は policy.yaml の 1 ルール。smtp-gateway の policy.Rule に対応する最小定義。
type Rule struct {
	Name         string       `yaml:"name" json:"name"`
	Description  string       `yaml:"description,omitempty" json:"description,omitempty"`
	Enabled      *bool        `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Priority     int          `yaml:"priority,omitempty" json:"priority,omitempty"`
	Tags         []string     `yaml:"tags,omitempty" json:"tags,omitempty"`
	Condition    string       `yaml:"condition" json:"condition"`
	Action       string       `yaml:"action,omitempty" json:"action,omitempty"`
	Destination  string       `yaml:"destination,omitempty" json:"destination,omitempty"`
	DelayMinutes int          `yaml:"delay_minutes,omitempty" json:"delay_minutes,omitempty"`
	Actions      []ActionSpec `yaml:"actions,omitempty" json:"actions,omitempty"`
}

// ListConfig は名前付きリスト定義。
type ListConfig struct {
	Values []string `yaml:"values,omitempty" json:"values,omitempty"`
	File   string   `yaml:"file,omitempty" json:"file,omitempty"`
}

// Document は policy.yaml のトップレベル構造。
type Document struct {
	Lists map[string]ListConfig `yaml:"lists,omitempty" json:"lists,omitempty"`
	Rules []Rule                `yaml:"rules" json:"rules"`
}

// Route は 1 ルートの情報（route.yaml + policy.yaml）。
type Route struct {
	Dir        string   `json:"dir"`       // routes.d 配下のディレクトリ名（例: 10-inbound）
	Name       string   `json:"name"`      // route.yaml の name
	Direction  string   `json:"direction"` // inbound / outbound
	PolicyPath string   `json:"-"`         // policy.yaml の絶対パス
	Document   Document `json:"policy"`
}

// 既知アクション種別。
var terminalActions = map[string]bool{
	"deliver": true, "reject": true, "quarantine": true, "approval": true, "delay": true,
}
var nonTerminalActions = map[string]bool{
	"add_header": true, "remove_header": true, "add_subject_prefix": true, "log_only": true,
}

func isKnownAction(t string) bool { return terminalActions[t] || nonTerminalActions[t] }

// RoutesDir は config ディレクトリ（mailshield.yaml のあるディレクトリ）から
// routes.d の絶対パスを返す。
func RoutesDir(gatewayConfigFile string) string {
	return filepath.Join(filepath.Dir(gatewayConfigFile), "routes.d")
}

// ListRoutes は routes.d 配下の全ルートを読み込む（ディレクトリ名の昇順）。
func ListRoutes(routesDir string) ([]Route, error) {
	entries, err := os.ReadDir(routesDir)
	if err != nil {
		return nil, fmt.Errorf("routes.d 読み込み失敗 (%s): %w", routesDir, err)
	}
	var routes []Route
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		r, err := ReadRoute(routesDir, e.Name())
		if err != nil {
			return nil, err
		}
		if r != nil {
			routes = append(routes, *r)
		}
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].Dir < routes[j].Dir })
	return routes, nil
}

// ReadRoute は 1 ルートの route.yaml と policy.yaml を読み込む。
// route.yaml が無いディレクトリは nil を返す（スキップ対象）。
func ReadRoute(routesDir, dir string) (*Route, error) {
	routeDir := filepath.Join(routesDir, dir)
	routeFile := filepath.Join(routeDir, "route.yaml")
	rdata, err := os.ReadFile(routeFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("route.yaml 読み込み失敗 (%s): %w", routeFile, err)
	}
	var meta struct {
		Name      string `yaml:"name"`
		Direction string `yaml:"direction"`
	}
	if err := yaml.Unmarshal(rdata, &meta); err != nil {
		return nil, fmt.Errorf("route.yaml パース失敗 (%s): %w", routeFile, err)
	}

	route := &Route{Dir: dir, Name: meta.Name, Direction: meta.Direction,
		PolicyPath: filepath.Join(routeDir, "policy.yaml")}

	pdata, err := os.ReadFile(route.PolicyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return route, nil // policy.yaml なし = ルールなし
		}
		return nil, fmt.Errorf("policy.yaml 読み込み失敗 (%s): %w", route.PolicyPath, err)
	}
	if err := yaml.Unmarshal(pdata, &route.Document); err != nil {
		return nil, fmt.Errorf("policy.yaml パース失敗 (%s): %w", route.PolicyPath, err)
	}
	return route, nil
}

// FindRoute は指定ディレクトリ名のルートを返す（見つからなければ nil）。
func FindRoute(routesDir, dir string) (*Route, error) {
	// パストラバーサル防止: dir は単一セグメントのみ許可
	if dir == "" || strings.ContainsAny(dir, "/\\") || dir == "." || dir == ".." {
		return nil, fmt.Errorf("不正なルート名: %q", dir)
	}
	return ReadRoute(routesDir, dir)
}

// ValidateDocument はルールの構造的な妥当性を検証する（アクション種別・条件・デフォルトルール）。
// 条件式の詳細な文法検証は smtp-gateway の /reload に委ねる。
func ValidateDocument(doc *Document) error {
	if len(doc.Rules) == 0 {
		return fmt.Errorf("ルールが 1 つもありません")
	}
	hasDefault := false
	for i, r := range doc.Rules {
		if strings.TrimSpace(r.Name) == "" {
			return fmt.Errorf("%d 番目のルールに name がありません", i+1)
		}
		if strings.TrimSpace(r.Condition) == "" {
			return fmt.Errorf("ルール %q に condition がありません", r.Name)
		}
		specs := r.specs()
		if len(specs) == 0 {
			return fmt.Errorf("ルール %q にアクションがありません", r.Name)
		}
		for _, s := range specs {
			if !isKnownAction(s.Type) {
				return fmt.Errorf("ルール %q の未知のアクション: %q", r.Name, s.Type)
			}
		}
		if strings.TrimSpace(r.Condition) == "true" {
			hasDefault = true
		}
	}
	if !hasDefault {
		return fmt.Errorf("フォールバックルール（condition: \"true\"）がありません。メール消失を防ぐため必ず 1 つ用意してください")
	}
	return nil
}

func (r *Rule) specs() []ActionSpec {
	if len(r.Actions) > 0 {
		return r.Actions
	}
	if r.Action != "" {
		return []ActionSpec{{Type: r.Action, Destination: r.Destination, DelayMinutes: r.DelayMinutes}}
	}
	return nil
}

// Marshal は Document を policy.yaml のバイト列にシリアライズする。
// 注意: 既存ファイルのコメントは保持されない（UI 管理下ではファイルが SSOT ではなく UI が SSOT）。
func Marshal(doc *Document) ([]byte, error) {
	var buf strings.Builder
	buf.WriteString("# このファイルは MailShield 管理 UI により生成・更新されます。\n")
	buf.WriteString("# 手動編集した場合、UI からの保存で上書きされることがあります。\n")
	enc := yaml.NewEncoder(&stringWriter{&buf})
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("policy.yaml シリアライズ失敗: %w", err)
	}
	enc.Close()
	return []byte(buf.String()), nil
}

type stringWriter struct{ b *strings.Builder }

func (w *stringWriter) Write(p []byte) (int, error) { return w.b.Write(p) }

// WriteAtomic は policy.yaml を一時ファイル経由で原子的に置き換える。
func WriteAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("一時ファイル書き込み失敗: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("policy.yaml 置き換え失敗: %w", err)
	}
	return nil
}
