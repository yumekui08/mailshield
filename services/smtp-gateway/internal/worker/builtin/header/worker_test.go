package header

import (
	"context"
	"fmt"
	"testing"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

func newTestWorker(threshold int, scores ScoresConfig, brands []string) *Worker {
	return &Worker{
		threshold:  threshold,
		scores:     scores,
		brandNames: brands,
	}
}

func buildEML(from, replyTo, subject string) []byte {
	headers := fmt.Sprintf("From: %s\r\nTo: victim@internal.test\r\nSubject: %s\r\n", from, subject)
	if replyTo != "" {
		headers += fmt.Sprintf("Reply-To: %s\r\n", replyTo)
	}
	return []byte(headers + "\r\nBody text")
}

var defaultScores = ScoresConfig{
	SPFFail:         30,
	DKIMFail:        40,
	DMARCFail:       30,
	ReplyToMismatch: 40,
	BrandSpoofing:   60,
}

func TestHeaderInspector_CleanMail(t *testing.T) {
	w := newTestWorker(60, defaultScores, []string{"amazon"})
	m := &domain.Mail{
		FromAddress: "sender@legitimate.com",
		RawEML:      buildEML("Sender <sender@legitimate.com>", "", "Hello"),
		AuthResults: domain.DefaultAuthResults(),
	}

	res, err := w.Inspect(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Detected {
		t.Errorf("detected=true, want false (score=%d)", res.Score)
	}
	if res.Score != 0 {
		t.Errorf("score=%d, want 0", res.Score)
	}
}

func TestHeaderInspector_SPFFail(t *testing.T) {
	w := newTestWorker(60, defaultScores, []string{"amazon"})
	m := &domain.Mail{
		FromAddress: "sender@spoofed.com",
		RawEML:      buildEML("Sender <sender@spoofed.com>", "", "Hello"),
		AuthResults: domain.AuthResults{SPF: domain.AuthFail, DKIM: domain.AuthNone, DMARC: domain.AuthNone},
	}

	res, err := w.Inspect(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Score != 30 {
		t.Errorf("score=%d, want 30", res.Score)
	}
	if res.Detected {
		t.Errorf("detected=true, want false (score %d < threshold 60)", res.Score)
	}
}

func TestHeaderInspector_DKIMAndDMARCFail_ExceedsThreshold(t *testing.T) {
	w := newTestWorker(60, defaultScores, []string{"amazon"})
	m := &domain.Mail{
		FromAddress: "sender@spoofed.com",
		RawEML:      buildEML("Sender <sender@spoofed.com>", "", "Hello"),
		AuthResults: domain.AuthResults{SPF: domain.AuthNone, DKIM: domain.AuthFail, DMARC: domain.AuthFail},
	}

	res, err := w.Inspect(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// dkim_fail(40) + dmarc_fail(30) = 70
	if res.Score != 70 {
		t.Errorf("score=%d, want 70", res.Score)
	}
	if !res.Detected {
		t.Errorf("detected=false, want true (score %d >= threshold 60)", res.Score)
	}
}

func TestHeaderInspector_ReplyToMismatch(t *testing.T) {
	w := newTestWorker(60, defaultScores, []string{"amazon"})
	m := &domain.Mail{
		FromAddress: "sender@legitimate.com",
		RawEML:      buildEML("Sender <sender@legitimate.com>", "attacker@evil.com", "Hello"),
		AuthResults: domain.DefaultAuthResults(),
	}

	res, err := w.Inspect(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Score != 40 {
		t.Errorf("score=%d, want 40", res.Score)
	}
	if _, ok := res.Details["reply_to"]; !ok {
		t.Errorf("details[reply_to] not set")
	}
}

func TestHeaderInspector_ReplyToSameDomain_NotFlagged(t *testing.T) {
	w := newTestWorker(60, defaultScores, []string{"amazon"})
	m := &domain.Mail{
		FromAddress: "sender@legitimate.com",
		RawEML:      buildEML("Sender <sender@legitimate.com>", "other@legitimate.com", "Hello"),
		AuthResults: domain.DefaultAuthResults(),
	}

	res, err := w.Inspect(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Score != 0 {
		t.Errorf("score=%d, want 0", res.Score)
	}
}

func TestHeaderInspector_BrandSpoofing(t *testing.T) {
	w := newTestWorker(60, defaultScores, []string{"amazon"})
	m := &domain.Mail{
		FromAddress: "support@phish-site.net",
		RawEML:      buildEML("Amazon Security <support@phish-site.net>", "", "Your account"),
		AuthResults: domain.DefaultAuthResults(),
	}

	res, err := w.Inspect(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Score != 60 {
		t.Errorf("score=%d, want 60", res.Score)
	}
	if !res.Detected {
		t.Errorf("detected=false, want true")
	}
	if res.Details["from_name"] != "Amazon Security" {
		t.Errorf("from_name=%v, want 'Amazon Security'", res.Details["from_name"])
	}
}

func TestHeaderInspector_BrandDomainMatches_NotFlagged(t *testing.T) {
	w := newTestWorker(60, defaultScores, []string{"amazon"})
	m := &domain.Mail{
		FromAddress: "noreply@amazon.com",
		RawEML:      buildEML("Amazon <noreply@amazon.com>", "", "Order confirmation"),
		AuthResults: domain.DefaultAuthResults(),
	}

	res, err := w.Inspect(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Score != 0 {
		t.Errorf("score=%d, want 0", res.Score)
	}
}

func TestHeaderInspector_ScoreCappedAt100(t *testing.T) {
	scores := ScoresConfig{
		SPFFail: 50, DKIMFail: 50, DMARCFail: 50,
		ReplyToMismatch: 50, BrandSpoofing: 50,
	}
	w := newTestWorker(60, scores, []string{"amazon"})
	m := &domain.Mail{
		FromAddress: "support@phish-site.net",
		RawEML:      buildEML("Amazon <support@phish-site.net>", "evil@other.com", "Alert"),
		AuthResults: domain.AuthResults{SPF: domain.AuthFail, DKIM: domain.AuthFail, DMARC: domain.AuthFail},
	}

	res, err := w.Inspect(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Score != 100 {
		t.Errorf("score=%d, want 100 (capped)", res.Score)
	}
}

func TestHeaderInspector_CustomThreshold(t *testing.T) {
	// 閾値を 100 にすると score=60 では detected=false
	w := newTestWorker(100, defaultScores, []string{"amazon"})
	m := &domain.Mail{
		FromAddress: "support@phish-site.net",
		RawEML:      buildEML("Amazon <support@phish-site.net>", "", "Alert"),
		AuthResults: domain.DefaultAuthResults(),
	}

	res, err := w.Inspect(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Detected {
		t.Errorf("detected=true, want false with threshold=100")
	}
}

func TestHeaderInspector_InvalidEML_AuthResultsStillScored(t *testing.T) {
	w := newTestWorker(60, defaultScores, []string{"amazon"})
	m := &domain.Mail{
		FromAddress: "sender@spoofed.com",
		RawEML:      []byte("not valid eml"),
		AuthResults: domain.AuthResults{SPF: domain.AuthFail, DKIM: domain.AuthFail, DMARC: domain.AuthNone},
	}

	res, err := w.Inspect(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// spf(30) + dkim(40) = 70、EML パース失敗でも auth results は反映される
	if res.Score != 70 {
		t.Errorf("score=%d, want 70", res.Score)
	}
}
