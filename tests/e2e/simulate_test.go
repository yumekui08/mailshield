//go:build e2e

// simulate_test.go は POST /simulate エンドポイントを使って
// ワーカーパイプラインとポリシーエンジンを E2E でテストする。
// docker-compose で smtp-gateway が起動していれば MAILSHIELD_GATEWAY_URL を
// 設定しなくても localhost:8080 に接続する。
//
// 実行方法:
//
//	cd tests/e2e && go test -v -tags e2e -run TestSimulate ./...
package e2e_test

import (
	"strings"
	"testing"
)

// ─── inbound ルートテスト ────────────────────────────────────────────────────

// TestSimulate_InboundNormal は通常メールが inbound ルートに乗り
// deliver アクションで返ること、全ワーカーが未検知であることを確認する。
func TestSimulate_InboundNormal_Deliver(t *testing.T) {
	requireGateway(t)
	r := simulateFile(t, "testdata/emls/inbound-normal.eml")

	if r.RouteName != "inbound" {
		t.Errorf("route: want inbound, got %q", r.RouteName)
	}
	if r.Action != "deliver" {
		t.Errorf("action: want deliver, got %q", r.Action)
	}
	if r.SubjectChanged {
		t.Errorf("通常メールで件名が変化してはいけない: %q → %q", r.OriginalSubject, r.TransformedSubject)
	}
	for _, ir := range r.InspectResults {
		if ir.Detected {
			t.Errorf("ワーカー %q が通常メールを誤検知しました (score=%d)", ir.Worker, ir.Score)
		}
	}
}

// TestSimulate_InboundVirusSubject_Detected は件名に "virus" を含むメールを
// subject-virus-inspector が検知し score=100 を返すことを確認する。
func TestSimulate_InboundVirusSubject_Detected(t *testing.T) {
	requireGateway(t)
	r := simulateFile(t, "testdata/emls/inbound-virus-subject.eml")

	ir := findWorkerResult(r, "subject-virus-inspector")
	if ir == nil {
		t.Fatal("subject-virus-inspector が inspect_results に見つかりません")
	}
	if !ir.Detected {
		t.Error("subject-virus-inspector は件名に 'virus' を含むメールを検知しなければなりません")
	}
	if ir.Score != 100 {
		t.Errorf("score: want 100, got %d", ir.Score)
	}
}

// TestSimulate_InboundVirusSubject_SubjectPrefixed は virus 件名のメールが
// "[迷惑メール注意] " プレフィックス付きで変換され、deliver されることを確認する。
func TestSimulate_InboundVirusSubject_SubjectPrefixed(t *testing.T) {
	requireGateway(t)
	r := simulateFile(t, "testdata/emls/inbound-virus-subject.eml")

	if !r.SubjectChanged {
		t.Fatal("件名は変換されなければなりません")
	}
	const wantPrefix = "[迷惑メール注意] "
	if !strings.HasPrefix(r.TransformedSubject, wantPrefix) {
		t.Errorf("変換後件名は %q で始まる必要があります, got: %q", wantPrefix, r.TransformedSubject)
	}
	if r.Action != "deliver" {
		t.Errorf("virus_subject ルールは deliver を返します, got: %q", r.Action)
	}
}

// TestSimulate_InboundBrandSpoof_HeaderDetects はブランドなりすましメールを
// header-inspector が score>=60（閾値）で検知することを確認する。
func TestSimulate_InboundBrandSpoof_HeaderDetects(t *testing.T) {
	requireGateway(t)
	r := simulateFile(t, "testdata/emls/inbound-brand-spoof.eml")

	ir := findWorkerResult(r, "header-inspector")
	if ir == nil {
		t.Skip("header-inspector が inspect_results にありません（ルート設定で無効化されている可能性）")
	}
	if !ir.Detected {
		t.Errorf("header-inspector は Amazon ブランドなりすましを検知しなければなりません (score=%d)", ir.Score)
	}
	if ir.Score < 60 {
		t.Errorf("ブランドなりすましスコアは ≥60 であるべきです, got %d", ir.Score)
	}
}

// TestSimulate_InboundXSSHtml_ScriptTagRemoved は HTML メール内の <script> タグが
// sanitize-worker によって除去されることを確認する。
func TestSimulate_InboundXSSHtml_ScriptTagRemoved(t *testing.T) {
	requireGateway(t)
	r := simulateFile(t, "testdata/emls/inbound-xss-html.eml")

	if r.TransformedEML == "" {
		t.Skip("TransformedEML が空です")
	}
	if strings.Contains(r.TransformedEML, "<script") {
		t.Error("変換後 EML に <script> タグが残っています (sanitize-worker が除去しなければなりません)")
	}
}

// TestSimulate_InboundXSSHtml_IframeRemoved は <iframe> が除去されることを確認する。
func TestSimulate_InboundXSSHtml_IframeRemoved(t *testing.T) {
	requireGateway(t)
	r := simulateFile(t, "testdata/emls/inbound-xss-html.eml")

	if r.TransformedEML == "" {
		t.Skip("TransformedEML が空です")
	}
	if strings.Contains(r.TransformedEML, "<iframe") {
		t.Error("変換後 EML に <iframe> が残っています (sanitize-worker が除去しなければなりません)")
	}
}

// TestSimulate_InboundXSSHtml_OnClickRemoved は onclick= 属性が除去されることを確認する。
func TestSimulate_InboundXSSHtml_OnClickRemoved(t *testing.T) {
	requireGateway(t)
	r := simulateFile(t, "testdata/emls/inbound-xss-html.eml")

	if r.TransformedEML == "" {
		t.Skip("TransformedEML が空です")
	}
	if strings.Contains(r.TransformedEML, "onclick=") {
		t.Error("変換後 EML に onclick= が残っています (sanitize-worker が除去しなければなりません)")
	}
}

// TestSimulate_InboundWithURL_Rewritten は本文内の URL が safelink プロキシ経由に
// 書き換えられ、元の URL が消えることを確認する。
func TestSimulate_InboundWithURL_Rewritten(t *testing.T) {
	requireGateway(t)
	r := simulateFile(t, "testdata/emls/inbound-with-url.eml")

	if r.TransformedEML == "" {
		t.Skip("TransformedEML が空です")
	}
	// url-rewrite-worker の proxy_base_url（safelink.example.com）が含まれる
	if !strings.Contains(r.TransformedEML, "safelink.example.com") {
		t.Error("変換後 EML に safelink.example.com が含まれません (url-rewrite-worker が URL を書き換えなければなりません)")
	}
	// 元の URL（https://www.google.com）は直接残っていてはいけない
	if strings.Contains(r.TransformedEML, "https://www.google.com") {
		t.Error("変換後 EML に元 URL (https://www.google.com) が残っています (url-rewrite-worker が書き換えなければなりません)")
	}
}

// TestSimulate_InboundEICAR_AVDetects は EICAR テスト文字列を含むメールを
// av-worker が検知し quarantine アクションを返すことを確認する。
// ClamAV が起動していない場合は自動スキップ。
func TestSimulate_InboundEICAR_AVDetects(t *testing.T) {
	requireGateway(t)
	r := simulateFile(t, "testdata/emls/inbound-eicar.eml")

	ir := findWorkerResult(r, "av-worker")
	if ir == nil {
		t.Skip("av-worker が inspect_results にありません（ClamAV が起動していない可能性）")
	}
	if !ir.Detected {
		t.Skip("av-worker が EICAR を検知しませんでした（ClamAV が起動していない可能性）")
	}
	if r.Action != "quarantine" {
		t.Errorf("EICAR メールは quarantine にならなければなりません, got action=%q rule=%q", r.Action, r.MatchedRule)
	}
	if r.MatchedRule != "av_detected" {
		t.Errorf("matched_rule: want av_detected, got %q", r.MatchedRule)
	}
}

// ─── outbound ルートテスト ───────────────────────────────────────────────────

// TestSimulate_OutboundNormal_Deliver は内部ドメインからの送信が outbound ルートに乗り
// deliver されることを確認する。
func TestSimulate_OutboundNormal_Deliver(t *testing.T) {
	requireGateway(t)
	r := simulateFile(t, "testdata/emls/outbound-normal.eml")

	if r.RouteName != "outbound" {
		t.Errorf("route: want outbound, got %q", r.RouteName)
	}
	if r.Direction != "outbound" {
		t.Errorf("direction: want outbound, got %q", r.Direction)
	}
	if r.Action != "deliver" {
		t.Errorf("action: want deliver, got %q", r.Action)
	}
}

// TestSimulate_OutboundNormal_DisclaimerAdded は outbound メールに
// disclaimer-worker がフッターマーカーを追加することを確認する。
func TestSimulate_OutboundNormal_DisclaimerAdded(t *testing.T) {
	requireGateway(t)
	r := simulateFile(t, "testdata/emls/outbound-normal.eml")

	if r.TransformedEML == "" {
		t.Skip("TransformedEML が空です")
	}
	// MIME パートをデコードして検索（base64 エンコード対応）
	decoded := decodedEMLBodies(t, r.TransformedEML)
	// disclaimer-worker の marker（重複防止識別子）が存在する
	if !strings.Contains(decoded, "mailshield-disclaimer") {
		t.Error("outbound EML に mailshield-disclaimer マーカーが含まれません (disclaimer-worker が追加しなければなりません)")
	}
	// フッターテキスト本文
	if !strings.Contains(decoded, "組織のメールフィルタリングシステム") {
		t.Error("outbound EML に disclaimer テキストが含まれません")
	}
}

// ─── エラーケース ────────────────────────────────────────────────────────────

// TestSimulate_NoMatchingRoute_Returns422 はどのルートにもマッチしない EML が
// 422 Unprocessable Entity を返すことを確認する。
func TestSimulate_NoMatchingRoute_Returns422(t *testing.T) {
	requireGateway(t)
	eml := []byte("From: unknown@nowhere.example\r\nTo: nobody@nowhere.example\r\nSubject: No Route\r\n\r\nBody\r\n")
	code, _ := simulateRaw(t, eml)
	if code != 422 {
		t.Errorf("ルート未マッチ EML は 422 を返すべきです, got %d", code)
	}
}

// TestSimulate_EmptyBody_Returns400 は空のリクエストボディが 400 を返すことを確認する。
func TestSimulate_EmptyBody_Returns400(t *testing.T) {
	requireGateway(t)
	code, _ := simulateRaw(t, []byte{})
	if code != 400 {
		t.Errorf("空ボディは 400 を返すべきです, got %d", code)
	}
}
