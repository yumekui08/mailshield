package qrcheck

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/jhillyerd/enmime"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

// --- モック ---

type mockQRDecoder struct {
	text string
	err  error
}

func (m *mockQRDecoder) Decode(_ image.Image) (string, error) {
	return m.text, m.err
}

type mockOCRScanner struct {
	text string
	err  error
}

func (m *mockOCRScanner) ScanText(_ context.Context, _ []byte, _ string) (string, error) {
	return m.text, m.err
}

type mockReputationChecker struct {
	hits []string
	err  error
}

func (m *mockReputationChecker) Check(_ context.Context, _ []string) ([]string, error) {
	return m.hits, m.err
}

// countingQRDecoder はデコードが呼ばれた回数を記録するモック。
type countingQRDecoder struct {
	count *int
}

func (d *countingQRDecoder) Decode(_ image.Image) (string, error) {
	*d.count++
	return "", errors.New("no QR")
}

// --- テスト用ヘルパー ---

// make1x1PNG は 1×1 の白色 PNG バイナリを返す。
func make1x1PNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// buildImageEML は PNG 添付ファイルを1枚含む EML バイト列を返す。
func buildImageEML(pngData []byte) []byte {
	b := enmime.Builder().
		From("sender@example.com", "sender@example.com").
		To("recv@example.com", "recv@example.com").
		Subject("Test").
		Text([]byte("See attachment")).
		AddAttachment(pngData, "image/png", "image.png")
	root, _ := b.Build()
	var buf bytes.Buffer
	_ = root.Encode(&buf)
	return buf.Bytes()
}

// buildMultiImageEML は PNG 添付ファイルを n 枚含む EML バイト列を返す。
func buildMultiImageEML(pngData []byte, n int) []byte {
	b := enmime.Builder().
		From("sender@example.com", "sender@example.com").
		To("recv@example.com", "recv@example.com").
		Subject("Test").
		Text([]byte("See attachments"))
	for i := 0; i < n; i++ {
		b = b.AddAttachment(pngData, "image/png", "image.png")
	}
	root, _ := b.Build()
	var buf bytes.Buffer
	_ = root.Encode(&buf)
	return buf.Bytes()
}

func buildTextOnlyEML() []byte {
	return []byte("From: sender@example.com\r\nTo: recv@example.com\r\nSubject: Test\r\n\r\nNo images here.")
}

func newTestWorker(qr qrDecoder, ocr ocrScanner, rep reputationChecker, denyList []string, scores ScoresConfig) *Worker {
	return &Worker{
		maxImages:      10,
		maxImagePixels: 4096 * 4096,
		qrDecoder:      qr,
		ocrClient:      ocr,
		denyList:       denyList,
		reputation:     rep,
		scores:         scores,
	}
}

func mail(eml []byte) *domain.Mail {
	return &domain.Mail{
		FromAddress: "sender@example.com",
		ToAddresses: []string{"recv@example.com"},
		RawEML:      eml,
	}
}

var defaultScores = ScoresConfig{DenyListMatch: 100, ReputationAPIHit: 90}

// --- テスト ---

func TestQRCheck_NoImages(t *testing.T) {
	w := newTestWorker(nil, nil, nil, nil, defaultScores)
	r, err := w.Inspect(context.Background(), mail(buildTextOnlyEML()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Detected {
		t.Errorf("want detected=false for mail with no images")
	}
	if r.Details["total_urls_found"] != 0 {
		t.Errorf("want total_urls_found=0")
	}
}

func TestQRCheck_QRDenyListHit(t *testing.T) {
	qr := &mockQRDecoder{text: "https://malware.example.com/evil"}
	w := newTestWorker(qr, nil, nil, []string{"malware.example.com"}, defaultScores)
	r, err := w.Inspect(context.Background(), mail(buildImageEML(make1x1PNG())))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.Detected {
		t.Errorf("want detected=true for QR with deny URL")
	}
	if r.Score != 100 {
		t.Errorf("want score=100, got %d", r.Score)
	}
}

func TestQRCheck_QRSubdomainDenyListHit(t *testing.T) {
	qr := &mockQRDecoder{text: "https://sub.evil.test/path"}
	w := newTestWorker(qr, nil, nil, []string{"evil.test"}, defaultScores)
	r, err := w.Inspect(context.Background(), mail(buildImageEML(make1x1PNG())))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.Detected {
		t.Errorf("want detected=true for QR subdomain in deny list")
	}
}

func TestQRCheck_QRNotURL(t *testing.T) {
	// QR コードのテキストが URL でない場合は検出なし
	qr := &mockQRDecoder{text: "WIFI:S:MyNetwork;T:WPA;P:password;;"}
	w := newTestWorker(qr, nil, nil, nil, defaultScores)
	r, err := w.Inspect(context.Background(), mail(buildImageEML(make1x1PNG())))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Detected {
		t.Errorf("want detected=false for QR with non-URL content")
	}
	if r.Details["total_urls_found"] != 0 {
		t.Errorf("want total_urls_found=0, got %v", r.Details["total_urls_found"])
	}
}

func TestQRCheck_QRDecodeError_Skipped(t *testing.T) {
	// QR デコード失敗は skip して検出なし
	qr := &mockQRDecoder{err: errors.New("no QR code found")}
	w := newTestWorker(qr, nil, nil, []string{"malware.example.com"}, defaultScores)
	r, err := w.Inspect(context.Background(), mail(buildImageEML(make1x1PNG())))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Detected {
		t.Errorf("want detected=false when QR decode fails")
	}
}

func TestQRCheck_OCRDenyListHit(t *testing.T) {
	ocr := &mockOCRScanner{text: "Visit https://phishing.example.net/login for details"}
	w := newTestWorker(nil, ocr, nil, []string{"phishing.example.net"}, defaultScores)
	r, err := w.Inspect(context.Background(), mail(buildImageEML(make1x1PNG())))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.Detected {
		t.Errorf("want detected=true for OCR URL in deny list")
	}
	if r.Score != 100 {
		t.Errorf("want score=100, got %d", r.Score)
	}
}

func TestQRCheck_APIHit(t *testing.T) {
	qr := &mockQRDecoder{text: "https://suspicious.example.com/page"}
	rep := &mockReputationChecker{hits: []string{"https://suspicious.example.com/page"}}
	w := newTestWorker(qr, nil, rep, nil, defaultScores)
	r, err := w.Inspect(context.Background(), mail(buildImageEML(make1x1PNG())))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !r.Detected {
		t.Errorf("want detected=true via reputation API")
	}
	if r.Score != 90 {
		t.Errorf("want score=90, got %d", r.Score)
	}
}

func TestQRCheck_MaxImages(t *testing.T) {
	var scanCount int
	w := &Worker{
		maxImages:      2,
		maxImagePixels: 4096 * 4096,
		qrDecoder:      &countingQRDecoder{count: &scanCount},
		scores:         defaultScores,
	}
	r, err := w.Inspect(context.Background(), mail(buildMultiImageEML(make1x1PNG(), 3)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = r
	if scanCount != 2 {
		t.Errorf("want 2 images scanned (max_images=2), got %d", scanCount)
	}
}

func TestQRCheck_Deduplication(t *testing.T) {
	// QR と OCR が同じ URL を返しても total_urls_found は 1 になること
	sameURL := "https://evil.example.com/page"
	qr := &mockQRDecoder{text: sameURL}
	ocr := &mockOCRScanner{text: "found " + sameURL + " here"}
	w := newTestWorker(qr, ocr, nil, []string{"evil.example.com"}, defaultScores)
	r, err := w.Inspect(context.Background(), mail(buildImageEML(make1x1PNG())))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Details["total_urls_found"] != 1 {
		t.Errorf("want total_urls_found=1 (dedup), got %v", r.Details["total_urls_found"])
	}
}

func TestQRCheck_CustomScores(t *testing.T) {
	scores := ScoresConfig{DenyListMatch: 70, ReputationAPIHit: 50}
	qr := &mockQRDecoder{text: "https://bad.test/x"}
	w := newTestWorker(qr, nil, nil, []string{"bad.test"}, scores)
	r, err := w.Inspect(context.Background(), mail(buildImageEML(make1x1PNG())))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Score != 70 {
		t.Errorf("want score=70 (custom), got %d", r.Score)
	}
}
