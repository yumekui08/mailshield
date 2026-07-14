package header

import (
	"context"
	"strings"
	"testing"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

func newWorkerWithInternal(internal []string) *Worker {
	return &Worker{
		threshold:       60,
		scores:          defaultConfig().Scores,
		brandNames:      []string{"amazon"},
		internalDomains: internal,
	}
}

func inspectEML(t *testing.T, w *Worker, envelopeFrom string, rawEML []byte) *domain.InspectResult {
	t.Helper()
	res, err := w.Inspect(context.Background(), &domain.Mail{
		FromAddress: envelopeFrom,
		RawEML:      rawEML,
		AuthResults: domain.DefaultAuthResults(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func hasReason(res *domain.InspectResult, prefix string) bool {
	reasons, _ := res.Details["reasons"].([]string)
	for _, r := range reasons {
		if strings.HasPrefix(r, prefix) {
			return true
		}
	}
	return false
}

func TestDisplayNameSpoofing(t *testing.T) {
	w := newWorkerWithInternal(nil)
	// 表示名に別ドメインのアドレスを埋め込み、実 From は evil.test
	eml := buildEML(`"経理部 ceo@corp.example" <attacker@evil.test>`, "", "至急")
	res := inspectEML(t, w, "attacker@evil.test", eml)
	if !hasReason(res, "display_name_spoofing") {
		t.Errorf("表示名偽装を検知すべき (score=%d, reasons=%v)", res.Score, res.Details["reasons"])
	}
}

func TestEnvelopeFromMismatch(t *testing.T) {
	w := newWorkerWithInternal(nil)
	// ヘッダー From は bank.example、エンベロープは bounce.evil.test
	eml := buildEML("Bank <notice@bank.example>", "", "お知らせ")
	res := inspectEML(t, w, "bounce@bounce.evil.test", eml)
	if !hasReason(res, "envelope_from_mismatch") {
		t.Errorf("From/envelope 乖離を検知すべき (score=%d, reasons=%v)", res.Score, res.Details["reasons"])
	}
}

func TestLookalikeDomain(t *testing.T) {
	w := newWorkerWithInternal([]string{"corp.example"})

	// レーベンシュタイン距離1（corp → c0rp は confusable 正規化で一致）
	eml := buildEML("Staff <staff@c0rp.example>", "", "hi")
	res := inspectEML(t, w, "staff@c0rp.example", eml)
	if !hasReason(res, "lookalike_domain") {
		t.Errorf("confusable ドメインを検知すべき (score=%d, reasons=%v)", res.Score, res.Details["reasons"])
	}

	// 1文字挿入（corpp.example）
	eml2 := buildEML("Staff <staff@corpp.example>", "", "hi")
	res2 := inspectEML(t, w, "staff@corpp.example", eml2)
	if !hasReason(res2, "lookalike_domain") {
		t.Errorf("1文字違いドメインを検知すべき (score=%d, reasons=%v)", res2.Score, res2.Details["reasons"])
	}
}

func TestLookalikeDomain_ExactMatchNotFlagged(t *testing.T) {
	w := newWorkerWithInternal([]string{"corp.example"})
	// 完全一致は正当（内部からの正規メール）。envelope も一致させる。
	eml := buildEML("Staff <staff@corp.example>", "", "hi")
	res := inspectEML(t, w, "staff@corp.example", eml)
	if hasReason(res, "lookalike_domain") {
		t.Errorf("完全一致ドメインを誤検知 (reasons=%v)", res.Details["reasons"])
	}
}

func TestIsLookalike(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"c0rp.example", "corp.example", true},   // confusable 0→o
		{"corp1.example", "corpl.example", true}, // confusable 1→l
		{"corpp.example", "corp.example", true},  // 1文字挿入
		{"corp.example", "corp.example", true},   // 完全一致（isLookalike 単体では true。呼び出し側で除外）
		{"totally-different.test", "corp.example", false},
	}
	for _, c := range cases {
		if got := isLookalike(c.a, c.b); got != c.want {
			t.Errorf("isLookalike(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
