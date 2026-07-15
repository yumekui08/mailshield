package policy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

func TestBuildFacts_MailAttributes(t *testing.T) {
	mail := &domain.Mail{
		FromAddress:   "Alice@External.TEST",
		ToAddresses:   []string{"bob@internal.test", "carol@partner.example"},
		Subject:       "請求書",
		SizeBytes:     2048,
		HasAttachment: true,
		Direction:     domain.DirectionInbound,
	}
	results := []*domain.InspectResult{
		{WorkerName: "av-worker", Score: 30},
		{WorkerName: "url-worker", Score: 40},
	}
	facts := buildFacts(mail, results)

	checks := map[string]any{
		"mail.from":           "alice@external.test",
		"mail.from_domain":    "external.test",
		"mail.to_domains":     "internal.test,partner.example",
		"mail.subject":        "請求書",
		"mail.size_bytes":     2048,
		"mail.has_attachment": true,
		"mail.direction":      "inbound",
		"total_score":         70,
	}
	for k, want := range checks {
		if got := facts[k]; got != want {
			t.Errorf("facts[%q] = %v, want %v", k, got, want)
		}
	}
}

func evalWith(t *testing.T, condition string, facts map[string]any, lists map[string]map[string]bool) bool {
	t.Helper()
	ok, err := evalCondition(condition, evalContext{facts: facts, lists: lists})
	if err != nil {
		t.Fatalf("evalCondition(%q) error: %v", condition, err)
	}
	return ok
}

func TestEvalCondition_Contains(t *testing.T) {
	facts := map[string]any{"mail.subject": "至急 請求書の確認をお願いします"}
	if !evalWith(t, "mail.subject contains 請求書", facts, nil) {
		t.Error("部分一致すべき")
	}
	if evalWith(t, "mail.subject contains 見積書", facts, nil) {
		t.Error("含まれない語で誤マッチ")
	}
}

func TestEvalCondition_InList(t *testing.T) {
	lists := map[string]map[string]bool{
		"freemail": {"gmail.com": true, "yahoo.co.jp": true},
	}
	facts := map[string]any{
		"mail.from":        "attacker@gmail.com",
		"mail.from_domain": "gmail.com",
	}
	// アドレスからドメインを取り出して照合
	if !evalWith(t, "mail.from in_list freemail", facts, lists) {
		t.Error("アドレスのドメイン部で in_list マッチすべき")
	}
	// ドメイン fact 直接
	if !evalWith(t, "mail.from_domain in_list freemail", facts, lists) {
		t.Error("ドメイン fact で in_list マッチすべき")
	}
	facts["mail.from_domain"] = "corp.example"
	if evalWith(t, "mail.from_domain in_list freemail", facts, lists) {
		t.Error("リスト外で誤マッチ")
	}
}

// TestEvalCondition_InList_MultiRecipient は mail.to / mail.to_domains のように
// カンマ連結された複数宛先で、いずれか 1 つでもリストに含まれれば true になることを検証する。
func TestEvalCondition_InList_MultiRecipient(t *testing.T) {
	lists := map[string]map[string]bool{
		"freemail": {"gmail.com": true, "yahoo.co.jp": true},
	}
	// 先頭がフリーメール・末尾が社内ドメイン（旧実装は末尾しか見ず取りこぼしていた）
	facts := map[string]any{
		"mail.to":         "victim@gmail.com,boss@corp.example",
		"mail.to_domains": "gmail.com,corp.example",
	}
	if !evalWith(t, "mail.to_domains in_list freemail", facts, lists) {
		t.Error("複数宛先ドメインのいずれかがフリーメールなら in_list マッチすべき")
	}
	if !evalWith(t, "mail.to in_list freemail", facts, lists) {
		t.Error("複数宛先アドレスのいずれかのドメインがフリーメールなら in_list マッチすべき")
	}
	// どの宛先もリスト外なら false
	facts["mail.to_domains"] = "corp.example,partner.example"
	if evalWith(t, "mail.to_domains in_list freemail", facts, lists) {
		t.Error("全宛先がリスト外なら誤マッチしてはいけない")
	}
}

func TestEvalCondition_TotalScoreThreshold(t *testing.T) {
	facts := map[string]any{"total_score": 120}
	if !evalWith(t, "total_score >= 100", facts, nil) {
		t.Error("合算スコア閾値マッチすべき")
	}
	facts["total_score"] = 80
	if evalWith(t, "total_score >= 100", facts, nil) {
		t.Error("閾値未満で誤マッチ")
	}
}

func TestEvalCondition_AND(t *testing.T) {
	facts := map[string]any{
		"mail.direction":      "outbound",
		"mail.has_attachment": true,
		"total_score":         60,
	}
	// すべて真
	if !evalWith(t, "mail.direction == outbound && mail.has_attachment == true && total_score >= 50", facts, nil) {
		t.Error("全条件が真のとき AND は真であるべき")
	}
	// 1つ偽
	if evalWith(t, "mail.direction == outbound && total_score >= 100", facts, nil) {
		t.Error("1条件が偽のとき AND は偽であるべき")
	}
}

func TestEvalCondition_NotEquals(t *testing.T) {
	facts := map[string]any{"mail.direction": "inbound"}
	if !evalWith(t, "mail.direction != outbound", facts, nil) {
		t.Error("!= は異なる値でマッチすべき")
	}
	if evalWith(t, "mail.direction != inbound", facts, nil) {
		t.Error("!= は同じ値でマッチしないべき")
	}
}

func TestLoadLists_FileAndValues(t *testing.T) {
	dir := t.TempDir()
	listFile := filepath.Join(dir, "denydomains.txt")
	if err := os.WriteFile(listFile, []byte("# コメント\nevil.example\n  BadGuy.test  \n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lists, err := loadLists(map[string]ListConfig{
		"deny": {Values: []string{"Inline.example"}, File: "denydomains.txt"},
	}, dir)
	if err != nil {
		t.Fatal(err)
	}
	set := lists["deny"]
	for _, want := range []string{"inline.example", "evil.example", "badguy.test"} {
		if !set[want] {
			t.Errorf("リストに %q が含まれるべき: %v", want, set)
		}
	}
	if set["#"] || set[""] {
		t.Error("コメント・空行が混入している")
	}
}

func TestNew_WithListsAndInList(t *testing.T) {
	dir := t.TempDir()
	policyFile := filepath.Join(dir, "policy.yaml")
	os.WriteFile(filepath.Join(dir, "free.txt"), []byte("gmail.com\n"), 0o644)
	yaml := `
lists:
  freemail:
    file: free.txt
    values: [yahoo.co.jp]
rules:
  - name: freemail_to_approval
    condition: "mail.direction == outbound && mail.to_domains in_list freemail"
    action: approval
  - name: default
    condition: "true"
    action: deliver
`
	if err := os.WriteFile(policyFile, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := New(policyFile, nil)
	if err != nil {
		t.Fatal(err)
	}
	mail := &domain.Mail{
		FromAddress: "emp@corp.example",
		ToAddresses: []string{"someone@gmail.com"},
		Direction:   domain.DirectionOutbound,
	}
	action, rule := e.Evaluate(mail, nil)
	if action != ActionApproval || rule != "freemail_to_approval" {
		t.Errorf("フリーメール宛送信は approval になるべき: action=%s rule=%s", action, rule)
	}
}
