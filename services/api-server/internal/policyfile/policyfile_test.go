package policyfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRoute(t *testing.T, routesDir, dir, routeYAML, policyYAML string) {
	t.Helper()
	d := filepath.Join(routesDir, dir)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "route.yaml"), []byte(routeYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if policyYAML != "" {
		if err := os.WriteFile(filepath.Join(d, "policy.yaml"), []byte(policyYAML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListRoutes(t *testing.T) {
	rd := t.TempDir()
	writeRoute(t, rd, "10-inbound",
		"name: inbound\ndirection: inbound\n",
		"rules:\n  - name: default\n    condition: \"true\"\n    action: deliver\n")
	writeRoute(t, rd, "20-outbound",
		"name: outbound\ndirection: outbound\n",
		"rules:\n  - name: block\n    condition: \"dlp.detected == true\"\n    action: quarantine\n  - name: default\n    condition: \"true\"\n    action: deliver\n")

	routes, err := ListRoutes(rd)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 {
		t.Fatalf("ルート数 = %d, want 2", len(routes))
	}
	if routes[0].Name != "inbound" || routes[1].Name != "outbound" {
		t.Errorf("ディレクトリ昇順で並ぶべき: %s %s", routes[0].Name, routes[1].Name)
	}
	if len(routes[1].Document.Rules) != 2 {
		t.Errorf("outbound のルール数 = %d, want 2", len(routes[1].Document.Rules))
	}
}

func TestValidateDocument(t *testing.T) {
	ok := &Document{Rules: []Rule{
		{Name: "block", Condition: "dlp.detected == true", Action: "quarantine"},
		{Name: "default", Condition: "true", Action: "deliver"},
	}}
	if err := ValidateDocument(ok); err != nil {
		t.Errorf("正当なドキュメントがエラー: %v", err)
	}

	noDefault := &Document{Rules: []Rule{{Name: "x", Condition: "a == 1", Action: "reject"}}}
	if err := ValidateDocument(noDefault); err == nil {
		t.Error("デフォルトルール無しはエラーになるべき")
	}

	badAction := &Document{Rules: []Rule{{Name: "x", Condition: "true", Action: "teleport"}}}
	if err := ValidateDocument(badAction); err == nil {
		t.Error("未知アクションはエラーになるべき")
	}

	noAction := &Document{Rules: []Rule{{Name: "x", Condition: "true"}}}
	if err := ValidateDocument(noAction); err == nil {
		t.Error("アクション無しはエラーになるべき")
	}

	// 非終端アクションのみ + デフォルトは valid
	nonTerminal := &Document{Rules: []Rule{
		{Name: "tag", Condition: "mail.direction == inbound", Actions: []ActionSpec{{Type: "add_subject_prefix", Value: "[EXT] "}}},
		{Name: "default", Condition: "true", Action: "deliver"},
	}}
	if err := ValidateDocument(nonTerminal); err != nil {
		t.Errorf("非終端アクションのみのルールがエラー: %v", err)
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	doc := &Document{Rules: []Rule{
		{Name: "tag", Condition: "mail.direction == inbound", Actions: []ActionSpec{
			{Type: "add_subject_prefix", Value: "[EXTERNAL] "},
		}},
		{Name: "default", Condition: "true", Action: "deliver", Destination: "mailpit:1025"},
	}}
	data, err := Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "add_subject_prefix") {
		t.Errorf("シリアライズ結果にアクションが無い:\n%s", data)
	}
	// 書き戻して再パースできる
	rd := t.TempDir()
	writeRoute(t, rd, "10-inbound", "name: inbound\ndirection: inbound\n", "")
	path := filepath.Join(rd, "10-inbound", "policy.yaml")
	if err := WriteAtomic(path, data); err != nil {
		t.Fatal(err)
	}
	r, err := ReadRoute(rd, "10-inbound")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Document.Rules) != 2 || r.Document.Rules[0].Actions[0].Type != "add_subject_prefix" {
		t.Errorf("ラウンドトリップ失敗: %+v", r.Document.Rules)
	}
}

func TestFindRoute_PathTraversal(t *testing.T) {
	rd := t.TempDir()
	for _, bad := range []string{"../etc", "a/b", "..", ""} {
		if _, err := FindRoute(rd, bad); err == nil {
			t.Errorf("不正なルート名 %q はエラーになるべき", bad)
		}
	}
}
