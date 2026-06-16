// Package router は MAIL FROM / RCPT TO の正規表現マッチによってルートを決定する。
// ルートは設定ファイルの定義順に評価し、最初にマッチしたルートを返す（first-match-wins）。
package router

import (
	"fmt"
	"regexp"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/config"
)

type compiledRoute struct {
	cfg     *config.RouteConfig
	fromRe  *regexp.Regexp // nil = 全マッチ
	toRe    *regexp.Regexp // nil = 全マッチ
	toMatch string         // "any" | "all"
}

// Router は正規表現コンパイル済みのルート一覧を保持する。
type Router struct {
	routes []compiledRoute
}

// New は RouteConfig スライスから Router を構築する。
// 各ルートの From / To 正規表現をコンパイルし、不正な場合はエラーを返す。
func New(routes []config.RouteConfig) (*Router, error) {
	compiled := make([]compiledRoute, 0, len(routes))
	for i := range routes {
		r := &routes[i]
		cr := compiledRoute{cfg: r, toMatch: "any"}
		if r.Match.ToMatch == "all" {
			cr.toMatch = "all"
		}
		if r.Match.From != "" {
			re, err := regexp.Compile(r.Match.From)
			if err != nil {
				return nil, fmt.Errorf("ルート %q の from 正規表現が不正: %w", r.Name, err)
			}
			cr.fromRe = re
		}
		if r.Match.To != "" {
			re, err := regexp.Compile(r.Match.To)
			if err != nil {
				return nil, fmt.Errorf("ルート %q の to 正規表現が不正: %w", r.Name, err)
			}
			cr.toRe = re
		}
		compiled = append(compiled, cr)
	}
	return &Router{routes: compiled}, nil
}

// Resolve は MAIL FROM と RCPT TO アドレスに基づいてルートを解決する。
// 設定順に評価し、最初にマッチしたルートの RouteConfig を返す。
// どのルートにもマッチしない場合は nil, false を返す。
func (rt *Router) Resolve(from string, rcptTo []string) (*config.RouteConfig, bool) {
	for i := range rt.routes {
		cr := &rt.routes[i]
		if cr.fromRe != nil && !cr.fromRe.MatchString(from) {
			continue
		}
		if cr.toRe != nil && !cr.matchTo(rcptTo) {
			continue
		}
		return cr.cfg, true
	}
	return nil, false
}

func (cr *compiledRoute) matchTo(rcptTo []string) bool {
	if cr.toMatch == "all" {
		for _, to := range rcptTo {
			if !cr.toRe.MatchString(to) {
				return false
			}
		}
		return true
	}
	// any（デフォルト）
	for _, to := range rcptTo {
		if cr.toRe.MatchString(to) {
			return true
		}
	}
	return false
}
