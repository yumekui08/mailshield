package urlcheck

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

// mockChecker は reputationChecker のテスト用モック。
type mockChecker struct {
	hits []string
	err  error
}

func (m *mockChecker) Check(_ context.Context, _ []string) ([]string, error) {
	return m.hits, m.err
}

func buildEML(body string) []byte {
	return []byte("From: sender@example.com\r\nTo: recv@example.com\r\nSubject: Test\r\n\r\n" + body)
}

func buildHTMLEML(htmlBody string) []byte {
	return []byte(
		"From: sender@example.com\r\nTo: recv@example.com\r\nSubject: Test\r\n" +
			"Content-Type: text/html\r\n\r\n" + htmlBody,
	)
}

func newWorker(denyList []string, checker reputationChecker, scores ScoresConfig) *Worker {
	maxURLs := 20
	if maxURLs <= 0 {
		maxURLs = 20
	}
	return &Worker{
		maxURLs:    maxURLs,
		denyList:   denyList,
		reputation: checker,
		scores:     scores,
	}
}

var defaultScores = ScoresConfig{DenyListMatch: 100, ReputationAPIHit: 90}

// --- URL なし ---

func TestURLCheck_NoURL(t *testing.T) {
	w := newWorker(nil, nil, defaultScores)
	m := &domain.Mail{RawEML: buildEML("Plain text without any links.")}
	r, err := w.Inspect(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Detected {
		t.Errorf("want detected=false, got true")
	}
	if r.Score != 0 {
		t.Errorf("want score=0, got %d", r.Score)
	}
	if r.Details["total_urls_checked"] != 0 {
		t.Errorf("want total_urls_checked=0")
	}
}

// --- deny リスト照合 ---

func TestURLCheck_DenyListHit(t *testing.T) {
	w := newWorker([]string{"malware.example.com"}, nil, defaultScores)
	m := &domain.Mail{RawEML: buildEML("Visit https://malware.example.com/evil for free!")}
	r, err := w.Inspect(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.Detected {
		t.Errorf("want detected=true")
	}
	if r.Score != 100 {
		t.Errorf("want score=100, got %d", r.Score)
	}
	hits, _ := r.Details["deny_list_hits"].([]string)
	if len(hits) != 1 || !strings.Contains(hits[0], "malware.example.com") {
		t.Errorf("want deny_list_hits to contain malware URL, got %v", hits)
	}
}

func TestURLCheck_DenyListSubdomain(t *testing.T) {
	w := newWorker([]string{"evil.test"}, nil, defaultScores)
	m := &domain.Mail{RawEML: buildEML("https://sub.evil.test/page")}
	r, err := w.Inspect(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.Detected {
		t.Errorf("サブドメインも deny リストに一致すること")
	}
}

func TestURLCheck_DenyListNoHit(t *testing.T) {
	w := newWorker([]string{"malware.example.com"}, nil, defaultScores)
	m := &domain.Mail{RawEML: buildEML("Visit https://safe.example.com/page")}
	r, err := w.Inspect(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Detected {
		t.Errorf("want detected=false for safe URL")
	}
}

// --- 外部 API ---

func TestURLCheck_APIHit(t *testing.T) {
	checker := &mockChecker{hits: []string{"https://phishing.example.net/login"}}
	w := newWorker(nil, checker, defaultScores)
	m := &domain.Mail{RawEML: buildEML("Go to https://phishing.example.net/login now")}
	r, err := w.Inspect(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.Detected {
		t.Errorf("want detected=true via API")
	}
	if r.Score != 90 {
		t.Errorf("want score=90, got %d", r.Score)
	}
}

func TestURLCheck_APIError_ContinuesWithDenyResult(t *testing.T) {
	// API エラーが起きても deny リストのスコアは維持される
	checker := &mockChecker{err: errors.New("API timeout")}
	w := newWorker([]string{"bad.example.com"}, checker, defaultScores)
	m := &domain.Mail{RawEML: buildEML("https://bad.example.com/x and https://other.com/y")}
	r, err := w.Inspect(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.Detected {
		t.Errorf("want detected=true from deny list even when API errors")
	}
	if r.Score != 100 {
		t.Errorf("want score=100 from deny list, got %d", r.Score)
	}
	if _, ok := r.Details["api_error"]; !ok {
		t.Errorf("want api_error in details")
	}
}

func TestURLCheck_DenyListURLNotSentToAPI(t *testing.T) {
	// deny リストに一致した URL は API に送らない
	var checkedURLs []string
	checker := &mockChecker{}
	checker.err = nil
	checker.hits = nil

	// カスタムモック（何が送られたか記録する）
	type recordingChecker struct {
		received []string
	}
	rc := &recordingChecker{}
	w := &Worker{
		maxURLs:  20,
		denyList: []string{"deny.example.com"},
		reputation: reputationCheckerFunc(func(_ context.Context, urls []string) ([]string, error) {
			rc.received = urls
			return nil, nil
		}),
		scores: defaultScores,
	}

	m := &domain.Mail{
		RawEML: buildEML("https://deny.example.com/bad https://safe.example.com/ok"),
	}
	_, err := w.Inspect(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, u := range rc.received {
		if strings.Contains(u, "deny.example.com") {
			t.Errorf("deny リストに一致した URL が API に送られた: %s", u)
		}
	}
	_ = checkedURLs
}

// --- URL 重複排除 ---

func TestURLCheck_Deduplication(t *testing.T) {
	var callCount int
	w := &Worker{
		maxURLs:  20,
		denyList: nil,
		reputation: reputationCheckerFunc(func(_ context.Context, urls []string) ([]string, error) {
			callCount++
			return nil, nil
		}),
		scores: defaultScores,
	}
	// 同じ URL が2回出現
	m := &domain.Mail{
		RawEML: buildEML("https://example.com/page and again https://example.com/page"),
	}
	r, err := w.Inspect(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Details["total_urls_checked"] != 1 {
		t.Errorf("want total_urls_checked=1 (dedup), got %v", r.Details["total_urls_checked"])
	}
}

// --- max_urls 上限 ---

func TestURLCheck_MaxURLs(t *testing.T) {
	w := &Worker{
		maxURLs:    2,
		denyList:   nil,
		reputation: nil,
		scores:     defaultScores,
	}
	body := "https://a.com/1 https://b.com/2 https://c.com/3"
	m := &domain.Mail{RawEML: buildEML(body)}
	r, err := w.Inspect(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Details["total_urls_checked"] != 2 {
		t.Errorf("want total_urls_checked=2, got %v", r.Details["total_urls_checked"])
	}
}

// --- HTML href 抽出 ---

func TestURLCheck_HTMLHrefDetected(t *testing.T) {
	w := newWorker([]string{"evil.example.com"}, nil, defaultScores)
	html := `<a href="https://evil.example.com/phish">Click here</a>`
	m := &domain.Mail{RawEML: buildHTMLEML(html)}
	r, err := w.Inspect(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.Detected {
		t.Errorf("want detected=true for HTML href with deny domain")
	}
}

// --- スコアカスタマイズ ---

func TestURLCheck_CustomScores(t *testing.T) {
	scores := ScoresConfig{DenyListMatch: 70, ReputationAPIHit: 50}
	w := newWorker([]string{"bad.test"}, nil, scores)
	m := &domain.Mail{RawEML: buildEML("https://bad.test/x")}
	r, err := w.Inspect(context.Background(), m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Score != 70 {
		t.Errorf("want score=70 (custom), got %d", r.Score)
	}
}

// --- ヘルパー ---

// reputationCheckerFunc は関数を reputationChecker に変換するアダプター。
type reputationCheckerFunc func(ctx context.Context, urls []string) ([]string, error)

func (f reputationCheckerFunc) Check(ctx context.Context, urls []string) ([]string, error) {
	return f(ctx, urls)
}
