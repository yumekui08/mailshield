package router

import (
	"testing"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/config"
)

func makeRoutes() []config.RouteConfig {
	return []config.RouteConfig{
		{
			Name:      "inbound",
			Direction: "inbound",
			Match: config.RouteMatchConfig{
				To:      "@internal\\.test$",
				ToMatch: "any",
			},
		},
		{
			Name:      "outbound",
			Direction: "outbound",
			Match: config.RouteMatchConfig{
				From: "@internal\\.test$",
			},
		},
		{
			Name:      "default",
			Direction: "inbound",
			Match:     config.RouteMatchConfig{}, // 全マッチ
		},
	}
}

func TestResolve_InboundByTo(t *testing.T) {
	rt, err := New(makeRoutes())
	if err != nil {
		t.Fatal(err)
	}
	route, ok := rt.Resolve("sender@external.example", []string{"user@internal.test"})
	if !ok {
		t.Fatal("ルートがマッチしなかった")
	}
	if route.Name != "inbound" {
		t.Errorf("route.Name = %q, want inbound", route.Name)
	}
}

func TestResolve_OutboundByFrom(t *testing.T) {
	rt, err := New(makeRoutes())
	if err != nil {
		t.Fatal(err)
	}
	// outbound: from が internal.test かつ to が外部
	route, ok := rt.Resolve("user@internal.test", []string{"external@example.com"})
	if !ok {
		t.Fatal("ルートがマッチしなかった")
	}
	// inbound ルートは to が external.example なのでマッチしない → outbound にマッチするはず
	if route.Name != "outbound" {
		t.Errorf("route.Name = %q, want outbound", route.Name)
	}
}

func TestResolve_InternalMailMatchesInbound(t *testing.T) {
	rt, err := New(makeRoutes())
	if err != nil {
		t.Fatal(err)
	}
	// internal.test → internal.test: inbound ルート（to がマッチ）が先に評価される
	route, ok := rt.Resolve("sender@internal.test", []string{"user@internal.test"})
	if !ok {
		t.Fatal("ルートがマッチしなかった")
	}
	if route.Name != "inbound" {
		t.Errorf("route.Name = %q, want inbound", route.Name)
	}
}

func TestResolve_FallsBackToDefault(t *testing.T) {
	rt, err := New(makeRoutes())
	if err != nil {
		t.Fatal(err)
	}
	// どちらのドメインでもない場合は default ルートにマッチ
	route, ok := rt.Resolve("a@external.com", []string{"b@external.com"})
	if !ok {
		t.Fatal("default ルートがマッチしなかった")
	}
	if route.Name != "default" {
		t.Errorf("route.Name = %q, want default", route.Name)
	}
}

func TestResolve_NoMatchWithoutDefault(t *testing.T) {
	routes := []config.RouteConfig{
		{
			Name: "inbound",
			Match: config.RouteMatchConfig{To: "@internal\\.test$"},
		},
	}
	rt, err := New(routes)
	if err != nil {
		t.Fatal(err)
	}
	_, ok := rt.Resolve("a@external.com", []string{"b@external.com"})
	if ok {
		t.Error("マッチしないはずなのにルートが返された")
	}
}

func TestResolve_ToMatchAll(t *testing.T) {
	routes := []config.RouteConfig{
		{
			Name: "all-internal",
			Match: config.RouteMatchConfig{
				To:      "@internal\\.test$",
				ToMatch: "all",
			},
		},
	}
	rt, err := New(routes)
	if err != nil {
		t.Fatal(err)
	}

	// 全員 internal → マッチ
	_, ok := rt.Resolve("sender@external.com", []string{"a@internal.test", "b@internal.test"})
	if !ok {
		t.Error("全員 internal なのにマッチしなかった")
	}

	// 1人でも外部 → マッチしない
	_, ok = rt.Resolve("sender@external.com", []string{"a@internal.test", "c@external.com"})
	if ok {
		t.Error("外部が混在しているのにマッチした")
	}
}

func TestNew_InvalidRegex(t *testing.T) {
	routes := []config.RouteConfig{
		{Name: "bad", Match: config.RouteMatchConfig{From: "[invalid"}},
	}
	_, err := New(routes)
	if err == nil {
		t.Error("不正な正規表現でエラーにならなかった")
	}
}
