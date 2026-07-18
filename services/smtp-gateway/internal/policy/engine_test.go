package policy

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

// evaluate と buildFacts はパッケージ内関数のためパッケージ内テストとする

// ─── EvaluateAndAct テスト ────────────────────────────────────

// fakeDeliverer は Deliver 呼び出しを記録するテスト用実装。
type fakeDeliverer struct {
	called      bool
	destination string
}

func (f *fakeDeliverer) Deliver(_ context.Context, _ *domain.Mail, destination string) error {
	f.called = true
	f.destination = destination
	return nil
}

func newEngineFromYAML(t *testing.T, yaml string, d Deliverer) *Engine {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "policy*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(yaml); err != nil {
		t.Fatal(err)
	}
	f.Close()
	e, err := New(f.Name(), d)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestEvaluateAndAct_ReturnsCorrectAction(t *testing.T) {
	tests := []struct {
		name       string
		rules      string
		results    []*domain.InspectResult
		wantAction ActionType
	}{
		{
			name: "virus 検知 → reject",
			rules: `
rules:
  - name: virus_block
    condition: "subject-virus-inspector.detected == true"
    action: reject
  - name: default
    condition: "true"
    action: deliver
    destination: "mailpit:1025"
`,
			results: []*domain.InspectResult{
				{WorkerName: "subject-virus-inspector", Detected: true, Score: 100, Details: map[string]any{}},
			},
			wantAction: ActionReject,
		},
		{
			name: "virus なし → deliver",
			rules: `
rules:
  - name: virus_block
    condition: "subject-virus-inspector.detected == true"
    action: reject
  - name: default
    condition: "true"
    action: deliver
    destination: "mailpit:1025"
`,
			results: []*domain.InspectResult{
				{WorkerName: "subject-virus-inspector", Detected: false, Score: 0, Details: map[string]any{}},
			},
			wantAction: ActionDeliver,
		},
		{
			name: "スコア閾値超過 → quarantine",
			rules: `
rules:
  - name: high_score
    condition: "dlp-worker.score >= 80"
    action: quarantine
  - name: default
    condition: "true"
    action: deliver
    destination: "mailpit:1025"
`,
			results: []*domain.InspectResult{
				{WorkerName: "dlp-worker", Detected: true, Score: 90, Details: map[string]any{}},
			},
			wantAction: ActionQuarantine,
		},
		{
			name: "承認フロー → approval",
			rules: `
rules:
  - name: needs_approval
    condition: "true"
    action: approval
`,
			results:    nil,
			wantAction: ActionApproval,
		},
		{
			name: "マッチなし → 空文字列",
			rules: `
rules:
  - name: only_virus
    condition: "subject-virus-inspector.detected == true"
    action: reject
`,
			results:    []*domain.InspectResult{},
			wantAction: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fd := &fakeDeliverer{}
			e := newEngineFromYAML(t, tt.rules, fd)
			mail := &domain.Mail{MessageID: "test-id"}

			result, err := e.EvaluateAndAct(context.Background(), mail, tt.results)
			if err != nil {
				t.Fatalf("EvaluateAndAct() error = %v", err)
			}
			action := result.Action
			if action != tt.wantAction {
				t.Errorf("action = %q, want %q", action, tt.wantAction)
			}
			if tt.wantAction == ActionDeliver {
				if !fd.called {
					t.Error("deliver アクションなのに Deliverer が呼ばれていません")
				}
				if fd.destination != "mailpit:1025" {
					t.Errorf("destination = %q, want %q", fd.destination, "mailpit:1025")
				}
			} else if fd.called {
				t.Errorf("%s アクションで Deliverer が呼ばれました", tt.wantAction)
			}
		})
	}
}

func TestEvaluateAndAct_Delay(t *testing.T) {
	e := newEngineFromYAML(t, `
rules:
  - name: outbound_delay
    condition: "mail.direction == outbound"
    action: delay
    delay_minutes: 3
  - name: default
    condition: "true"
    action: deliver
`, &fakeDeliverer{})

	mail := &domain.Mail{MessageID: "m1", Direction: domain.DirectionOutbound}
	result, err := e.EvaluateAndAct(context.Background(), mail, nil)
	if err != nil {
		t.Fatalf("EvaluateAndAct() error = %v", err)
	}
	if result.Action != ActionDelay {
		t.Errorf("action = %q, want delay", result.Action)
	}
	if result.DelayMinutes != 3 {
		t.Errorf("delay_minutes = %d, want 3", result.DelayMinutes)
	}
}

func TestEvaluateAndAct_DelayDefaultsTo5(t *testing.T) {
	e := newEngineFromYAML(t, `
rules:
  - name: d
    condition: "true"
    action: delay
`, &fakeDeliverer{})
	result, err := e.EvaluateAndAct(context.Background(), &domain.Mail{MessageID: "m2"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.DelayMinutes != 5 {
		t.Errorf("delay_minutes 未指定のデフォルト = %d, want 5", result.DelayMinutes)
	}
}

// TestEvaluateAndAct_NilDeliverer は deliverer 未設定で deliver アクションが
// エラーになることを確認する。
func TestEvaluateAndAct_NilDeliverer(t *testing.T) {
	e := newEngineFromYAML(t, `
rules:
  - name: default
    condition: "true"
    action: deliver
`, nil)
	_, err := e.EvaluateAndAct(context.Background(), &domain.Mail{MessageID: "test-id"}, nil)
	if err == nil {
		t.Fatal("deliverer が nil のとき deliver はエラーになるべきです")
	}
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		facts     map[string]any
		want      bool
		wantErr   bool
	}{
		{
			name:      "true は常にtrue",
			condition: "true",
			facts:     nil,
			want:      true,
		},
		{
			name:      "false は常にfalse",
			condition: "false",
			facts:     nil,
			want:      false,
		},
		{
			name:      "bool == true: マッチする",
			condition: "subject-virus-inspector.detected == true",
			facts:     map[string]any{"subject-virus-inspector.detected": true},
			want:      true,
		},
		{
			name:      "bool == true: マッチしない",
			condition: "subject-virus-inspector.detected == true",
			facts:     map[string]any{"subject-virus-inspector.detected": false},
			want:      false,
		},
		{
			name:      "存在しないキーはfalse",
			condition: "unknown.key == true",
			facts:     map[string]any{},
			want:      false,
		},
		{
			name:      "score >= 80: マッチする (int)",
			condition: "dlp-worker.score >= 80",
			facts:     map[string]any{"dlp-worker.score": 100},
			want:      true,
		},
		{
			name:      "score >= 80: 境界値でマッチする",
			condition: "dlp-worker.score >= 80",
			facts:     map[string]any{"dlp-worker.score": 80},
			want:      true,
		},
		{
			name:      "score >= 80: マッチしない",
			condition: "dlp-worker.score >= 80",
			facts:     map[string]any{"dlp-worker.score": 79},
			want:      false,
		},
		{
			name:      "score >= 80: float64でもマッチする",
			condition: "dlp-worker.score >= 80",
			facts:     map[string]any{"dlp-worker.score": float64(90)},
			want:      true,
		},
		{
			name:      "> 比較: 数値構文として有効（fact なしで false）",
			condition: "something > 10",
			facts:     nil,
			want:      false,
		},
		{
			name:      "thresholdが数値でない場合はエラー",
			condition: "score >= abc",
			facts:     nil,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := evalContext{facts: tt.facts, lists: map[string]map[string]bool{}}
			got, err := evalCondition(tt.condition, ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("evalCondition() error = %v, wantErr = %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("evalCondition() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildFacts(t *testing.T) {
	results := []*domain.InspectResult{
		{
			WorkerName: "subject-virus-inspector",
			Score:      100,
			Detected:   true,
			Details: map[string]any{
				"reason": "subject contains 'virus'",
			},
		},
		{
			WorkerName: "dlp-worker",
			Score:      50,
			Detected:   false,
			Details:    map[string]any{},
		},
	}

	facts := buildFacts(nil, results)

	checks := []struct {
		key  string
		want any
	}{
		{"subject-virus-inspector.detected", true},
		{"subject-virus-inspector.score", 100},
		{"subject-virus-inspector.reason", "subject contains 'virus'"},
		{"dlp-worker.detected", false},
		{"dlp-worker.score", 50},
	}

	for _, c := range checks {
		got, ok := facts[c.key]
		if !ok {
			t.Errorf("facts[%q] not found", c.key)
			continue
		}
		if got != c.want {
			t.Errorf("facts[%q] = %v, want %v", c.key, got, c.want)
		}
	}
}

func TestNewEngine_InvalidFile(t *testing.T) {
	_, err := New("/nonexistent/policy.yaml", nil)
	if err == nil {
		t.Error("New() should return error for nonexistent file")
	}
}

// ─── P1: enabled / priority / 非終端アクション ───────────────────────────────

func TestPrepareRules_DisabledExcluded(t *testing.T) {
	e := newEngineFromYAML(t, `
rules:
  - name: off
    enabled: false
    condition: "true"
    action: reject
  - name: on
    condition: "true"
    action: deliver
`, &fakeDeliverer{})
	action, rule := e.Evaluate(&domain.Mail{}, nil)
	if action != ActionDeliver || rule != "on" {
		t.Errorf("無効ルールは除外され deliver/on になるべき: action=%s rule=%s", action, rule)
	}
}

func TestPrepareRules_PrioritySort(t *testing.T) {
	// ファイル順では reject が先だが priority で deliver を先にする
	e := newEngineFromYAML(t, `
rules:
  - name: later
    priority: 100
    condition: "true"
    action: reject
  - name: earlier
    priority: 10
    condition: "true"
    action: deliver
`, &fakeDeliverer{})
	action, rule := e.Evaluate(&domain.Mail{}, nil)
	if action != ActionDeliver || rule != "earlier" {
		t.Errorf("priority 昇順で earlier/deliver が先に評価されるべき: action=%s rule=%s", action, rule)
	}
}

func TestNew_UnknownActionRejected(t *testing.T) {
	f, _ := os.CreateTemp(t.TempDir(), "policy*.yaml")
	f.WriteString("rules:\n  - name: bad\n    condition: \"true\"\n    action: teleport\n")
	f.Close()
	if _, err := New(f.Name(), nil); err == nil {
		t.Error("未知のアクションはエラーになるべき")
	}
}

func TestEvaluateAndAct_NonTerminalThenTerminal(t *testing.T) {
	// タグ付け（非終端）→ 次のルールで配送（終端）
	e := newEngineFromYAML(t, `
rules:
  - name: tag_external
    condition: "mail.direction == inbound"
    actions:
      - type: add_subject_prefix
        value: "[EXTERNAL] "
      - type: add_header
        name: X-MailShield-Origin
        value: external
  - name: default
    condition: "true"
    action: deliver
`, &fakeDeliverer{})

	mail := &domain.Mail{
		MessageID: "m1",
		Direction: domain.DirectionInbound,
		Subject:   "Hello",
		RawEML:    []byte("Subject: Hello\r\n\r\nbody\r\n"),
	}
	result, err := e.EvaluateAndAct(context.Background(), mail, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ActionDeliver {
		t.Errorf("最終アクションは deliver であるべき: %s", result.Action)
	}
	if mail.Subject != "[EXTERNAL] Hello" {
		t.Errorf("件名プレフィックスが適用されていない: %q", mail.Subject)
	}
	eml := string(mail.RawEML)
	if !strings.Contains(eml, "Subject: [EXTERNAL] Hello") {
		t.Errorf("EML の件名が更新されていない:\n%s", eml)
	}
	if !strings.Contains(eml, "X-MailShield-Origin: external") {
		t.Errorf("EML にヘッダーが追加されていない:\n%s", eml)
	}
}

func TestEvaluateAndAct_NonTerminalAffectsLaterCondition(t *testing.T) {
	// 非終端でタグ付け → 後続ルールがそのタグを条件にできる
	e := newEngineFromYAML(t, `
rules:
  - name: tag
    condition: "mail.direction == inbound"
    actions:
      - type: add_subject_prefix
        value: "[EXTERNAL] "
  - name: catch_tagged
    condition: "mail.subject contains [EXTERNAL]"
    action: quarantine
  - name: default
    condition: "true"
    action: deliver
`, &fakeDeliverer{})

	mail := &domain.Mail{
		Direction: domain.DirectionInbound,
		Subject:   "Hi",
		RawEML:    []byte("Subject: Hi\r\n\r\nbody\r\n"),
	}
	result, err := e.EvaluateAndAct(context.Background(), mail, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Action != ActionQuarantine {
		t.Errorf("タグ付け後の件名を条件にした quarantine になるべき: %s", result.Action)
	}
}
