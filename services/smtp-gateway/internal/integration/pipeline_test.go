// Package integration_test はパイプライン全体（検査→変換→ポリシー評価）を
// 外部サービスなしで統合テストする。
// 各テストは一時ディレクトリに Lua ワーカーを書き込み、
// worker.Manager → InspectPipeline / TransformPipeline → policy.Engine という
// 実際の処理チェーンを通してシナリオを検証する。
package integration_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/config"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/pipeline"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/policy"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/header"
)

// ─── Lua ワーカースクリプト ──────────────────────────────────

const subjectVirusInspectorSrc = `
local M = {}
M.name = "subject-virus-inspector"
M.type = "inspect"
function M.inspect(mail, config)
    local keywords = config.keywords or { "virus" }
    local score    = config.score    or 100
    local subject  = mail.subject    or ""
    local lower    = string.lower(subject)
    for _, kw in ipairs(keywords) do
        if string.find(lower, kw, 1, true) then
            return { detected = true, score = score, details = { reason = "subject contains '" .. kw .. "'" } }
        end
    end
    return { detected = false, score = 0, details = {} }
end
return M
`

const subjectVirusTransformerSrc = `
local M = {}
M.name = "subject-virus-transformer"
M.type = "transform"
function M.transform(mail, config)
    local keywords = config.keywords or { "virus" }
    local prefix   = config.prefix   or "[迷惑メール注意] "
    local subject  = mail.subject    or ""
    local lower    = string.lower(subject)
    for _, kw in ipairs(keywords) do
        if string.find(lower, kw, 1, true) then
            mail.subject = prefix .. subject
            break
        end
    end
    return mail
end
return M
`

// ─── ヘルパー ───────────────────────────────────────────────

func writeWorker(t *testing.T, workersDir, name, src string) {
	t.Helper()
	dir := filepath.Join(workersDir, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "init.lua"), []byte(src), 0644); err != nil {
		t.Fatalf("WriteFile init.lua: %v", err)
	}
}

// buildHeaderWorkerConfig はヘッダー検査ワーカーの設定ファイルを configDir に書く。
func buildHeaderWorkerConfig(t *testing.T, configDir string) {
	t.Helper()
	content := `threshold: 60
scores:
  spf_fail: 30
  dkim_fail: 40
  dmarc_fail: 30
  reply_to_mismatch: 40
  brand_spoofing: 60
brand_names:
  - amazon
  - google
  - microsoft
`
	if err := os.WriteFile(filepath.Join(configDir, "header-inspector.yaml"), []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile header-inspector.yaml: %v", err)
	}
}

// buildPipeline は WorkersConfig と組み込みワーカーを元に InspectPipeline と TransformPipeline を構築する。
func buildPipeline(
	t *testing.T,
	workersDir, configDir string,
	wCfg config.WorkersConfig,
	builtinInspect []domain.InspectWorker,
	builtinTransform []domain.TransformWorker,
) (*pipeline.InspectPipeline, *pipeline.TransformPipeline) {
	t.Helper()
	mgr, err := worker.New(workersDir, configDir, &wCfg, builtinInspect, builtinTransform)
	if err != nil {
		t.Fatalf("worker.New: %v", err)
	}
	return pipeline.NewInspectPipeline(mgr.InspectEntries()),
		pipeline.NewTransformPipeline(mgr.TransformWorkers())
}

// buildPolicy はインラインの YAML ルール文字列から policy.Engine を構築する。
// rulesYAML が空の場合は deliver-all デフォルトエンジンを返す。
func buildPolicy(t *testing.T, rulesYAML string) *policy.Engine {
	t.Helper()
	if rulesYAML == "" {
		pe, err := policy.New("", "localhost:25")
		if err != nil {
			t.Fatalf("policy.New(empty): %v", err)
		}
		return pe
	}
	f, err := os.CreateTemp(t.TempDir(), "policy-*.yaml")
	if err != nil {
		t.Fatalf("CreateTemp policy: %v", err)
	}
	if _, err := fmt.Fprint(f, rulesYAML); err != nil {
		t.Fatal(err)
	}
	f.Close()
	pe, err := policy.New(f.Name(), "localhost:25")
	if err != nil {
		t.Fatalf("policy.New(%s): %v", f.Name(), err)
	}
	return pe
}

// makeEML は最小限の RFC 5322 メール文字列を生成する。
func makeEML(from, to, subject, authResults, body string) []byte {
	h := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n", from, to, subject)
	if authResults != "" {
		h += "Authentication-Results: " + authResults + "\r\n"
	}
	return []byte(h + "\r\n" + body)
}

// ─── シナリオテスト ───────────────────────────────────────────

// TestPipeline_NormalMail_NoTransform はウイルスワードを含まない通常メールが
// 変換されずに deliver アクションになることを確認する（シナリオ 1）。
func TestPipeline_NormalMail_NoTransform(t *testing.T) {
	workersDir := t.TempDir()
	configDir := t.TempDir()
	writeWorker(t, workersDir, "subject-virus-inspector", subjectVirusInspectorSrc)
	writeWorker(t, workersDir, "subject-virus-transformer", subjectVirusTransformerSrc)

	wCfg := config.WorkersConfig{
		Inspect: []config.InspectWorkerConfig{
			{Name: "subject-virus-inspector", Enabled: true, TimeoutSeconds: 5},
		},
		Transform: []config.TransformWorkerConfig{
			{Name: "subject-virus-transformer", Enabled: true, Order: 1},
		},
	}
	inspP, transP := buildPipeline(t, workersDir, configDir, wCfg, nil, nil)
	pe := buildPolicy(t, "") // deliver-all

	eml := makeEML("sender@external.test", "user@internal.test", "Hello World", "", "Normal mail body")
	mail := &domain.Mail{
		MessageID:   "test-1",
		RawEML:      eml,
		FromAddress: "sender@external.test",
		ToAddresses: []string{"user@internal.test"},
		Subject:     "Hello World",
		AuthResults: domain.DefaultAuthResults(),
	}

	ctx := context.Background()
	inspResults, err := inspP.Run(ctx, mail)
	if err != nil {
		t.Fatalf("InspectPipeline: %v", err)
	}
	if len(inspResults) != 1 {
		t.Fatalf("検査結果数 = %d, want 1", len(inspResults))
	}
	if inspResults[0].Detected {
		t.Errorf("detected=true, want false（ウイルスワードなし）")
	}

	transformed, err := transP.Run(ctx, mail)
	if err != nil {
		t.Fatalf("TransformPipeline: %v", err)
	}
	if transformed.Subject != "Hello World" {
		t.Errorf("Subject = %q, want %q（変換されないはず）", transformed.Subject, "Hello World")
	}

	action, _ := pe.Evaluate(inspResults)
	if action != policy.ActionDeliver {
		t.Errorf("action = %q, want deliver", action)
	}
}

// TestPipeline_VirusMail_TransformAndDeliver はウイルス件名メールが検知・変換されて
// deliver アクションになることを確認する（シナリオ 2）。
func TestPipeline_VirusMail_TransformAndDeliver(t *testing.T) {
	workersDir := t.TempDir()
	configDir := t.TempDir()
	writeWorker(t, workersDir, "subject-virus-inspector", subjectVirusInspectorSrc)
	writeWorker(t, workersDir, "subject-virus-transformer", subjectVirusTransformerSrc)

	wCfg := config.WorkersConfig{
		Inspect: []config.InspectWorkerConfig{
			{Name: "subject-virus-inspector", Enabled: true, TimeoutSeconds: 5},
		},
		Transform: []config.TransformWorkerConfig{
			{Name: "subject-virus-transformer", Enabled: true, Order: 1},
		},
	}
	inspP, transP := buildPipeline(t, workersDir, configDir, wCfg, nil, nil)
	pe := buildPolicy(t, "") // deliver-all

	origSubject := "virus test mail"
	eml := makeEML("sender@external.test", "user@internal.test", origSubject, "", "Virus body")
	mail := &domain.Mail{
		MessageID:   "test-2",
		RawEML:      eml,
		FromAddress: "sender@external.test",
		ToAddresses: []string{"user@internal.test"},
		Subject:     origSubject,
		AuthResults: domain.DefaultAuthResults(),
	}

	ctx := context.Background()
	inspResults, _ := inspP.Run(ctx, mail)

	if len(inspResults) != 1 {
		t.Fatalf("検査結果数 = %d, want 1", len(inspResults))
	}
	if !inspResults[0].Detected {
		t.Error("detected=false, want true（virus キーワード検知）")
	}
	if inspResults[0].Score != 100 {
		t.Errorf("score = %d, want 100", inspResults[0].Score)
	}

	transformed, err := transP.Run(ctx, mail)
	if err != nil {
		t.Fatalf("TransformPipeline: %v", err)
	}
	wantSubject := "[迷惑メール注意] " + origSubject
	if transformed.Subject != wantSubject {
		t.Errorf("Subject = %q, want %q", transformed.Subject, wantSubject)
	}

	action, _ := pe.Evaluate(inspResults)
	if action != policy.ActionDeliver {
		t.Errorf("action = %q, want deliver", action)
	}
}

// TestPipeline_VirusMail_PolicyReject はウイルス件名メールがポリシーで拒否されることを確認する（シナリオ 3）。
func TestPipeline_VirusMail_PolicyReject(t *testing.T) {
	workersDir := t.TempDir()
	configDir := t.TempDir()
	writeWorker(t, workersDir, "subject-virus-inspector", subjectVirusInspectorSrc)

	wCfg := config.WorkersConfig{
		Inspect: []config.InspectWorkerConfig{
			{Name: "subject-virus-inspector", Enabled: true, TimeoutSeconds: 5},
		},
	}
	inspP, _ := buildPipeline(t, workersDir, configDir, wCfg, nil, nil)
	pe := buildPolicy(t, `
rules:
  - name: virus_reject
    condition: "subject-virus-inspector.detected == true"
    action: reject
  - name: default_deliver
    condition: "true"
    action: deliver
`)

	mail := &domain.Mail{
		MessageID: "test-3",
		Subject:   "virus detected",
		AuthResults: domain.DefaultAuthResults(),
	}

	ctx := context.Background()
	inspResults, _ := inspP.Run(ctx, mail)

	action, matchedRule := pe.Evaluate(inspResults)
	if action != policy.ActionReject {
		t.Errorf("action = %q, want reject", action)
	}
	if matchedRule != "virus_reject" {
		t.Errorf("matchedRule = %q, want virus_reject", matchedRule)
	}
}

// TestPipeline_HeaderInspector_SPFDKIMFail_Quarantine は SPF/DKIM 失敗メールが
// header-inspector で検知されて隔離アクションになることを確認する（シナリオ 4）。
func TestPipeline_HeaderInspector_SPFDKIMFail_Quarantine(t *testing.T) {
	workersDir := t.TempDir()
	configDir := t.TempDir()
	buildHeaderWorkerConfig(t, configDir)

	headerWorker, err := header.New(configDir)
	if err != nil {
		t.Fatalf("header.New: %v", err)
	}

	wCfg := config.WorkersConfig{
		Inspect: []config.InspectWorkerConfig{
			{Name: "header-inspector", Enabled: true, TimeoutSeconds: 5},
		},
	}
	inspP, _ := buildPipeline(t, workersDir, configDir, wCfg, []domain.InspectWorker{headerWorker}, nil)
	pe := buildPolicy(t, `
rules:
  - name: auth_fail_quarantine
    condition: "header-inspector.detected == true"
    action: quarantine
  - name: default_deliver
    condition: "true"
    action: deliver
`)

	eml := makeEML("spammer@evil.com", "victim@internal.test", "Urgent: your account", "", "Click here")
	mail := &domain.Mail{
		MessageID:   "test-4",
		RawEML:      eml,
		FromAddress: "spammer@evil.com",
		ToAddresses: []string{"victim@internal.test"},
		Subject:     "Urgent: your account",
		// SPF fail (30) + DKIM fail (40) = score 70, threshold 60 → detected=true
		AuthResults: domain.AuthResults{
			SPF:  domain.AuthFail,
			DKIM: domain.AuthFail,
			DMARC: domain.AuthNone,
		},
	}

	ctx := context.Background()
	inspResults, err := inspP.Run(ctx, mail)
	if err != nil {
		t.Fatalf("InspectPipeline: %v", err)
	}
	if len(inspResults) != 1 {
		t.Fatalf("検査結果数 = %d, want 1", len(inspResults))
	}
	if !inspResults[0].Detected {
		t.Errorf("detected=false, want true（SPF+DKIM fail でスコア70 >= threshold 60）")
	}
	if inspResults[0].Score < 60 {
		t.Errorf("score = %d, want >= 60", inspResults[0].Score)
	}

	action, matchedRule := pe.Evaluate(inspResults)
	if action != policy.ActionQuarantine {
		t.Errorf("action = %q, want quarantine", action)
	}
	if matchedRule != "auth_fail_quarantine" {
		t.Errorf("matchedRule = %q, want auth_fail_quarantine", matchedRule)
	}
}

// TestPipeline_HeaderInspector_AllPass_Deliver は SPF/DKIM/DMARC all pass のメールが
// 配送されることを確認する（シナリオ 5）。
func TestPipeline_HeaderInspector_AllPass_Deliver(t *testing.T) {
	workersDir := t.TempDir()
	configDir := t.TempDir()
	buildHeaderWorkerConfig(t, configDir)

	headerWorker, err := header.New(configDir)
	if err != nil {
		t.Fatalf("header.New: %v", err)
	}

	wCfg := config.WorkersConfig{
		Inspect: []config.InspectWorkerConfig{
			{Name: "header-inspector", Enabled: true, TimeoutSeconds: 5},
		},
	}
	inspP, _ := buildPipeline(t, workersDir, configDir, wCfg, []domain.InspectWorker{headerWorker}, nil)
	pe := buildPolicy(t, `
rules:
  - name: auth_fail_quarantine
    condition: "header-inspector.detected == true"
    action: quarantine
  - name: default_deliver
    condition: "true"
    action: deliver
`)

	eml := makeEML("legit@example.com", "user@internal.test", "Hello", "", "body")
	mail := &domain.Mail{
		MessageID:   "test-5",
		RawEML:      eml,
		FromAddress: "legit@example.com",
		ToAddresses: []string{"user@internal.test"},
		Subject:     "Hello",
		AuthResults: domain.AuthResults{
			SPF:  domain.AuthPass,
			DKIM: domain.AuthPass,
			DMARC: domain.AuthPass,
		},
	}

	ctx := context.Background()
	inspResults, err := inspP.Run(ctx, mail)
	if err != nil {
		t.Fatalf("InspectPipeline: %v", err)
	}
	if inspResults[0].Detected {
		t.Errorf("detected=true, want false（認証成功・スコア0）")
	}

	action, _ := pe.Evaluate(inspResults)
	if action != policy.ActionDeliver {
		t.Errorf("action = %q, want deliver", action)
	}
}

// TestPipeline_MultipleWorkers_VirusAndHeaderFail は複数の検査ワーカーが並列動作し、
// ウイルス検知と認証失敗を同時に処理できることを確認する（シナリオ 6）。
func TestPipeline_MultipleWorkers_VirusAndHeaderFail(t *testing.T) {
	workersDir := t.TempDir()
	configDir := t.TempDir()
	writeWorker(t, workersDir, "subject-virus-inspector", subjectVirusInspectorSrc)
	buildHeaderWorkerConfig(t, configDir)

	headerWorker, err := header.New(configDir)
	if err != nil {
		t.Fatalf("header.New: %v", err)
	}

	wCfg := config.WorkersConfig{
		Inspect: []config.InspectWorkerConfig{
			{Name: "subject-virus-inspector", Enabled: true, TimeoutSeconds: 5},
			{Name: "header-inspector", Enabled: true, TimeoutSeconds: 5},
		},
	}
	inspP, _ := buildPipeline(t, workersDir, configDir, wCfg, []domain.InspectWorker{headerWorker}, nil)
	pe := buildPolicy(t, `
rules:
  - name: virus_reject
    condition: "subject-virus-inspector.detected == true"
    action: reject
  - name: default_deliver
    condition: "true"
    action: deliver
`)

	eml := makeEML("spammer@evil.com", "victim@internal.test", "virus detected", "", "evil body")
	mail := &domain.Mail{
		MessageID:   "test-6",
		RawEML:      eml,
		FromAddress: "spammer@evil.com",
		ToAddresses: []string{"victim@internal.test"},
		Subject:     "virus detected",
		AuthResults: domain.AuthResults{
			SPF:  domain.AuthFail,
			DKIM: domain.AuthFail,
			DMARC: domain.AuthFail,
		},
	}

	ctx := context.Background()
	inspResults, err := inspP.Run(ctx, mail)
	if err != nil {
		t.Fatalf("InspectPipeline: %v", err)
	}
	if len(inspResults) != 2 {
		t.Fatalf("検査結果数 = %d, want 2", len(inspResults))
	}

	resultMap := make(map[string]*domain.InspectResult)
	for _, r := range inspResults {
		resultMap[r.WorkerName] = r
	}
	if !resultMap["subject-virus-inspector"].Detected {
		t.Error("subject-virus-inspector: detected=false, want true")
	}
	if !resultMap["header-inspector"].Detected {
		t.Error("header-inspector: detected=false, want true（SPF+DKIM+DMARC all fail）")
	}

	action, matchedRule := pe.Evaluate(inspResults)
	if action != policy.ActionReject {
		t.Errorf("action = %q, want reject", action)
	}
	if matchedRule != "virus_reject" {
		t.Errorf("matchedRule = %q, want virus_reject", matchedRule)
	}
}

// TestPipeline_BounceMailFromEmpty は MAIL FROM: <> のバウンスメールが正常処理されることを確認する（シナリオ 7）。
func TestPipeline_BounceMailFromEmpty(t *testing.T) {
	workersDir := t.TempDir()
	configDir := t.TempDir()
	writeWorker(t, workersDir, "subject-virus-inspector", subjectVirusInspectorSrc)

	wCfg := config.WorkersConfig{
		Inspect: []config.InspectWorkerConfig{
			{Name: "subject-virus-inspector", Enabled: true, TimeoutSeconds: 5},
		},
	}
	inspP, _ := buildPipeline(t, workersDir, configDir, wCfg, nil, nil)
	pe := buildPolicy(t, "")

	mail := &domain.Mail{
		MessageID:   "test-7",
		RawEML:      []byte("From: <>\r\nTo: user@internal.test\r\nSubject: Delivery Failure\r\n\r\nBounce body"),
		FromAddress: "",
		ToAddresses: []string{"user@internal.test"},
		Subject:     "Delivery Failure",
		AuthResults: domain.DefaultAuthResults(),
	}

	ctx := context.Background()
	inspResults, err := inspP.Run(ctx, mail)
	if err != nil {
		t.Fatalf("InspectPipeline: %v", err)
	}
	if inspResults[0].Detected {
		t.Error("バウンスメールのウイルス誤検知")
	}

	action, _ := pe.Evaluate(inspResults)
	if action != policy.ActionDeliver {
		t.Errorf("action = %q, want deliver", action)
	}
}

// TestPipeline_PolicyScoreThreshold はスコア >= N という条件式が正しく動作することを確認する（シナリオ 10）。
func TestPipeline_PolicyScoreThreshold(t *testing.T) {
	workersDir := t.TempDir()
	configDir := t.TempDir()
	writeWorker(t, workersDir, "subject-virus-inspector", subjectVirusInspectorSrc)

	wCfg := config.WorkersConfig{
		Inspect: []config.InspectWorkerConfig{
			{Name: "subject-virus-inspector", Enabled: true, TimeoutSeconds: 5},
		},
	}
	inspP, _ := buildPipeline(t, workersDir, configDir, wCfg, nil, nil)
	pe := buildPolicy(t, `
rules:
  - name: high_score_quarantine
    condition: "subject-virus-inspector.score >= 100"
    action: quarantine
  - name: default_deliver
    condition: "true"
    action: deliver
`)

	tests := []struct {
		subject    string
		wantAction policy.ActionType
	}{
		{"virus test mail", policy.ActionQuarantine}, // score=100
		{"Hello World", policy.ActionDeliver},        // score=0
	}
	for _, tt := range tests {
		t.Run(tt.subject, func(t *testing.T) {
			mail := &domain.Mail{
				MessageID:   "test-score",
				Subject:     tt.subject,
				AuthResults: domain.DefaultAuthResults(),
			}
			ctx := context.Background()
			inspResults, _ := inspP.Run(ctx, mail)
			action, _ := pe.Evaluate(inspResults)
			if action != tt.wantAction {
				t.Errorf("action = %q, want %q", action, tt.wantAction)
			}
		})
	}
}

// TestPipeline_DisabledWorkerSkipped は disabled なワーカーが実行されないことを確認する。
func TestPipeline_DisabledWorkerSkipped(t *testing.T) {
	workersDir := t.TempDir()
	configDir := t.TempDir()
	writeWorker(t, workersDir, "subject-virus-inspector", subjectVirusInspectorSrc)

	wCfg := config.WorkersConfig{
		Inspect: []config.InspectWorkerConfig{
			{Name: "subject-virus-inspector", Enabled: false, TimeoutSeconds: 5}, // disabled
		},
	}
	inspP, _ := buildPipeline(t, workersDir, configDir, wCfg, nil, nil)

	mail := &domain.Mail{
		MessageID: "test-disabled",
		Subject:   "virus test mail", // 検知されるはずだが disabled なので実行されない
		AuthResults: domain.DefaultAuthResults(),
	}

	ctx := context.Background()
	inspResults, err := inspP.Run(ctx, mail)
	if err != nil {
		t.Fatalf("InspectPipeline: %v", err)
	}
	if len(inspResults) != 0 {
		t.Errorf("disabled なワーカーが実行された: results=%d", len(inspResults))
	}
}

// TestPipeline_TransformOrder は transform ワーカーが order 順に実行されることを確認する。
func TestPipeline_TransformOrder(t *testing.T) {
	workersDir := t.TempDir()
	configDir := t.TempDir()

	// prefix1 を先に追加し、prefix2 をその後に追加する worker
	const prefix1Src = `
local M = {}
M.name = "prefix1"
M.type = "transform"
function M.transform(mail, config)
    mail.subject = "[P1] " .. (mail.subject or "")
    return mail
end
return M
`
	const prefix2Src = `
local M = {}
M.name = "prefix2"
M.type = "transform"
function M.transform(mail, config)
    mail.subject = "[P2] " .. (mail.subject or "")
    return mail
end
return M
`
	writeWorker(t, workersDir, "prefix1", prefix1Src)
	writeWorker(t, workersDir, "prefix2", prefix2Src)

	// order: prefix2=1, prefix1=2 → [P2] が先に付く → [P1] [P2] original
	wCfg := config.WorkersConfig{
		Transform: []config.TransformWorkerConfig{
			{Name: "prefix2", Enabled: true, Order: 1},
			{Name: "prefix1", Enabled: true, Order: 2},
		},
	}
	_, transP := buildPipeline(t, workersDir, configDir, wCfg, nil, nil)

	mail := &domain.Mail{
		MessageID: "test-order",
		Subject:   "original",
		RawEML:    []byte("Subject: original\r\n\r\nbody"),
	}
	ctx := context.Background()
	result, err := transP.Run(ctx, mail)
	if err != nil {
		t.Fatalf("TransformPipeline: %v", err)
	}
	// order=1 の prefix2 が先、order=2 の prefix1 が後
	// → prefix2 適用: "[P2] original"
	// → prefix1 適用: "[P1] [P2] original"
	want := "[P1] [P2] original"
	if result.Subject != want {
		t.Errorf("Subject = %q, want %q（order 順）", result.Subject, want)
	}
}

// TestPipeline_EmptyPolicy_NoMatch はポリシールールが空の場合にマッチしないことを確認する（シナリオ 9）。
func TestPipeline_EmptyPolicy_NoMatch(t *testing.T) {
	pe := buildPolicy(t, `
rules: []
`)

	inspResults := []*domain.InspectResult{
		{WorkerName: "test", Detected: true, Score: 100},
	}
	action, matchedRule := pe.Evaluate(inspResults)
	if action != "" {
		t.Errorf("action = %q, want empty（ルールなし）", action)
	}
	if matchedRule != "" {
		t.Errorf("matchedRule = %q, want empty", matchedRule)
	}
}

// TestPipeline_WorkerTimeout はワーカーのタイムアウト設定が有効に機能することを確認する。
// 1ms タイムアウトのワーカーは sleep するので必ず timeout になる。
func TestPipeline_WorkerTimeout(t *testing.T) {
	workersDir := t.TempDir()

	const slowInspectorSrc = `
local M = {}
M.name = "slow-inspector"
M.type = "inspect"
function M.inspect(mail, config)
    -- 無限ループで意図的にタイムアウトを起こす
    local i = 0
    while true do
        i = i + 1
    end
    return { detected = false, score = 0, details = {} }
end
return M
`
	writeWorker(t, workersDir, "slow-inspector", slowInspectorSrc)

	wCfg := config.WorkersConfig{
		Inspect: []config.InspectWorkerConfig{
			{Name: "slow-inspector", Enabled: true, TimeoutSeconds: 1},
		},
	}
	configDir := t.TempDir()
	mgr, err := worker.New(workersDir, configDir, &wCfg, nil, nil)
	if err != nil {
		t.Fatalf("worker.New: %v", err)
	}
	inspP := pipeline.NewInspectPipeline(mgr.InspectEntries())

	mail := &domain.Mail{
		MessageID:   "test-timeout",
		Subject:     "test",
		AuthResults: domain.DefaultAuthResults(),
	}

	ctx := context.Background()
	inspResults, err := inspP.Run(ctx, mail)
	// タイムアウトした場合、エラーが返るかまたは空の結果が返る
	// InspectPipeline は timeout をエラーとして扱いその goroutine の結果を除外する
	if err != nil {
		// エラーが返った場合も許容（実装依存）
		t.Logf("InspectPipeline タイムアウト時の動作: error=%v", err)
	}
	// slow-inspector が timeout した場合、結果は 0 件またはエラー結果
	for _, r := range inspResults {
		if r.WorkerName == "slow-inspector" && r.Detected {
			t.Error("タイムアウトしたワーカーが detected=true を返した")
		}
	}
	t.Logf("タイムアウト後の検査結果数: %d", len(inspResults))
}
