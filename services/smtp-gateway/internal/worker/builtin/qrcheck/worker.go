// Package qrcheck はメール添付画像から QR コードをデコードし、
// 埋め込まれた URL をレピュテーション検査する inspect ワーカーを実装する。
// QR コードデコードには gozxing（外部サービス不要）を使う。
// OCR テキスト抽出はオプションで Tesseract REST API を呼ぶ（デフォルト無効）。
package qrcheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // GIF デコーダー登録
	_ "image/jpeg" // JPEG デコーダー登録
	_ "image/png"  // PNG デコーダー登録
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jhillyerd/enmime"
	"github.com/makiuchi-d/gozxing"
	gozxingqr "github.com/makiuchi-d/gozxing/qrcode"
	"gopkg.in/yaml.v3"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

const workerName = "qr-worker"

// qrDecoder は QR コードデコーダーのインターフェース。テスト時にモック可能。
type qrDecoder interface {
	Decode(img image.Image) (string, error)
}

// ocrScanner は OCR テキスト抽出のインターフェース。テスト時にモック可能。
type ocrScanner interface {
	ScanText(ctx context.Context, data []byte, mimeType string) (string, error)
}

// reputationChecker は外部 URL レピュテーション API の抽象インターフェース。
type reputationChecker interface {
	Check(ctx context.Context, urls []string) (hits []string, err error)
}

// QRDecodeConfig は QR コードデコードの設定を保持する。
type QRDecodeConfig struct {
	Enabled bool `yaml:"enabled"`
}

// OCRConfig は Tesseract OCR の設定を保持する。
type OCRConfig struct {
	Enabled        bool   `yaml:"enabled"`
	Endpoint       string `yaml:"endpoint"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

// ReputationAPIConfig は外部レピュテーション API の設定を保持する。
type ReputationAPIConfig struct {
	Backend        string `yaml:"backend"` // none | safe_browsing | web_risk
	APIKey         string `yaml:"api_key"`
	Endpoint       string `yaml:"endpoint"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	ClientID       string `yaml:"client_id"`
	ClientVersion  string `yaml:"client_version"`
}

// ScoresConfig は各検知項目のスコアを保持する。
type ScoresConfig struct {
	DenyListMatch    int `yaml:"deny_list_match"`
	ReputationAPIHit int `yaml:"reputation_api_hit"`
}

// Config は qr-worker の設定を保持する。
type Config struct {
	// MaxImages はメール1通で検査する画像の上限。
	MaxImages int `yaml:"max_images"`
	// MaxImagePixels は OOM 防止のための画像ピクセル数上限（幅×高さ）。
	MaxImagePixels int                 `yaml:"max_image_pixels"`
	QRDecode       QRDecodeConfig      `yaml:"qr_decode"`
	OCR            OCRConfig           `yaml:"ocr"`
	DenyList       []string            `yaml:"deny_list"`
	ReputationAPI  ReputationAPIConfig `yaml:"reputation_api"`
	Scores         ScoresConfig        `yaml:"scores"`
}

// Worker は QR コード検査ワーカーである。
type Worker struct {
	maxImages      int
	maxImagePixels int
	qrDecoder      qrDecoder  // nil のとき QR デコード無効
	ocrClient      ocrScanner // nil のとき OCR 無効
	denyList       []string   // 小文字に正規化済み
	reputation     reputationChecker
	scores         ScoresConfig
}

// urlInImagePattern は QR コード／OCR テキストから URL を抽出するパターン。
var urlInImagePattern = regexp.MustCompile(`https?://\S+`)

// New は qr-worker を初期化する。
func New(workerConfigDir string) (*Worker, error) {
	cfg, err := loadConfig(workerConfigDir)
	if err != nil {
		return nil, fmt.Errorf("qr-worker 設定ロード失敗: %w", err)
	}

	denyList := make([]string, len(cfg.DenyList))
	for i, d := range cfg.DenyList {
		denyList[i] = strings.ToLower(d)
	}

	apiKey := cfg.ReputationAPI.APIKey
	if envKey := os.Getenv("REPUTATION_API_KEY"); envKey != "" {
		apiKey = envKey
	}

	var checker reputationChecker
	switch cfg.ReputationAPI.Backend {
	case "safe_browsing":
		if apiKey != "" {
			checker = newSafeBrowsingChecker(apiKey, cfg.ReputationAPI.Endpoint, cfg.ReputationAPI.TimeoutSeconds, cfg.ReputationAPI.ClientID, cfg.ReputationAPI.ClientVersion)
		}
	case "web_risk":
		if apiKey != "" {
			checker = newWebRiskChecker(apiKey, cfg.ReputationAPI.Endpoint, cfg.ReputationAPI.TimeoutSeconds)
		}
	}

	var qrDec qrDecoder
	if cfg.QRDecode.Enabled {
		qrDec = &gozxingQRDecoder{}
	}

	var ocrCli ocrScanner
	if cfg.OCR.Enabled && cfg.OCR.Endpoint != "" {
		ocrCli = newOCRClient(cfg.OCR.Endpoint, cfg.OCR.TimeoutSeconds)
	}

	maxImagePixels := cfg.MaxImagePixels
	if maxImagePixels <= 0 {
		maxImagePixels = 4096 * 4096
	}

	return &Worker{
		maxImages:      cfg.MaxImages,
		maxImagePixels: maxImagePixels,
		qrDecoder:      qrDec,
		ocrClient:      ocrCli,
		denyList:       denyList,
		reputation:     checker,
		scores:         cfg.Scores,
	}, nil
}

func (w *Worker) Name() string { return workerName }

// Inspect は EML の添付画像から URL を抽出し、レピュテーション検査を行う。
func (w *Worker) Inspect(ctx context.Context, m *domain.Mail) (*domain.InspectResult, error) {
	result := &domain.InspectResult{
		WorkerName: workerName,
		Details:    make(map[string]any),
	}

	env, err := enmime.ReadEnvelope(bytes.NewReader(m.RawEML))
	if err != nil {
		return result, nil
	}

	urls := w.extractURLsFromImages(ctx, env)
	result.Details["total_urls_found"] = len(urls)

	if len(urls) == 0 {
		return result, nil
	}

	var denyHits, apiHits []string
	maxScore := 0

	// deny リスト照合
	for _, u := range urls {
		if w.isDenied(u) {
			denyHits = append(denyHits, u)
			if w.scores.DenyListMatch > maxScore {
				maxScore = w.scores.DenyListMatch
			}
		}
	}

	// 外部 API 照合（deny リストにない URL のみ）
	if w.reputation != nil {
		toCheck := exclude(urls, denyHits)
		if len(toCheck) > 0 {
			hits, err := w.reputation.Check(ctx, toCheck)
			if err != nil {
				result.Details["api_error"] = err.Error()
			} else {
				apiHits = hits
				if len(hits) > 0 && w.scores.ReputationAPIHit > maxScore {
					maxScore = w.scores.ReputationAPIHit
				}
			}
		}
	}

	if len(denyHits) > 0 {
		result.Details["deny_list_hits"] = denyHits
	}
	if len(apiHits) > 0 {
		result.Details["reputation_api_hits"] = apiHits
	}
	if maxScore > 100 {
		maxScore = 100
	}
	result.Score = maxScore
	result.Detected = len(denyHits) > 0 || len(apiHits) > 0
	return result, nil
}

// extractURLsFromImages は EML の添付・インライン画像から URL を重複排除して返す。
func (w *Worker) extractURLsFromImages(ctx context.Context, env *enmime.Envelope) []string {
	type imgPart struct {
		data        []byte
		contentType string
	}

	var parts []imgPart
	for _, att := range env.Attachments {
		if strings.HasPrefix(att.ContentType, "image/") {
			parts = append(parts, imgPart{att.Content, att.ContentType})
		}
	}
	for _, inl := range env.Inlines {
		if strings.HasPrefix(inl.ContentType, "image/") {
			parts = append(parts, imgPart{inl.Content, inl.ContentType})
		}
	}

	seen := make(map[string]struct{})
	var result []string

	for i, p := range parts {
		if i >= w.maxImages {
			break
		}
		for _, u := range w.scanImage(ctx, p.data, p.contentType) {
			u = strings.TrimRight(u, ".,;:!?\"')")
			if u == "" {
				continue
			}
			key := strings.ToLower(u)
			if _, ok := seen[key]; !ok {
				seen[key] = struct{}{}
				result = append(result, u)
			}
		}
	}
	return result
}

// decodeImageSafe は画像ヘッダーを先読みしてサイズを確認してからデコードする。
// 巨大な PNG 等（< 1MB 圧縮, > 4GB 展開）による OOM を防ぐ。
// maxPixels は許容するピクセル数上限（幅×高さ）。
func decodeImageSafe(data []byte, maxPixels int) (image.Image, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("画像ヘッダー読み取り失敗: %w", err)
	}
	if cfg.Width*cfg.Height > maxPixels {
		return nil, fmt.Errorf("画像サイズが上限を超えています (%dx%d > %d pixels)", cfg.Width, cfg.Height, maxPixels)
	}
	img, _, err2 := image.Decode(bytes.NewReader(data))
	return img, err2
}

// scanImage は1枚の画像から URL を抽出する。QR デコードと OCR の両方を試みる。
func (w *Worker) scanImage(ctx context.Context, data []byte, contentType string) []string {
	var found []string

	if w.qrDecoder != nil {
		img, err := decodeImageSafe(data, w.maxImagePixels)
		if err == nil {
			if text, err := w.qrDecoder.Decode(img); err == nil && text != "" {
				for _, u := range urlInImagePattern.FindAllString(text, -1) {
					found = append(found, u)
				}
			}
		}
	}

	if w.ocrClient != nil {
		if text, err := w.ocrClient.ScanText(ctx, data, contentType); err == nil {
			for _, u := range urlInImagePattern.FindAllString(text, -1) {
				found = append(found, u)
			}
		}
	}

	return found
}

func (w *Worker) isDenied(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, d := range w.denyList {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

func exclude(all, remove []string) []string {
	if len(remove) == 0 {
		return all
	}
	removeSet := make(map[string]struct{}, len(remove))
	for _, r := range remove {
		removeSet[strings.ToLower(r)] = struct{}{}
	}
	var result []string
	for _, u := range all {
		if _, ok := removeSet[strings.ToLower(u)]; !ok {
			result = append(result, u)
		}
	}
	return result
}

// gozxingQRDecoder は gozxing ライブラリを使う QR コードデコーダー実装。
type gozxingQRDecoder struct{}

func (d *gozxingQRDecoder) Decode(img image.Image) (string, error) {
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", fmt.Errorf("BinaryBitmap 作成失敗: %w", err)
	}
	reader := gozxingqr.NewQRCodeReader()
	result, err := reader.Decode(bmp, nil)
	if err != nil {
		// QR コードが見つからない場合は正常（エラーとして扱わない）
		return "", err
	}
	return result.GetText(), nil
}

func loadConfig(dir string) (*Config, error) {
	path := filepath.Join(dir, workerName+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return defaultConfig(), nil
		}
		return nil, fmt.Errorf("設定ファイル読み込み失敗 (%s): %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("設定ファイルパース失敗 (%s): %w", path, err)
	}
	if cfg.MaxImages <= 0 {
		cfg.MaxImages = defaultConfig().MaxImages
	}
	if cfg.MaxImagePixels <= 0 {
		cfg.MaxImagePixels = defaultConfig().MaxImagePixels
	}
	if cfg.Scores == (ScoresConfig{}) {
		cfg.Scores = defaultConfig().Scores
	}
	return &cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		MaxImages:      10,
		MaxImagePixels: 4096 * 4096,
		QRDecode:       QRDecodeConfig{Enabled: true},
		OCR:            OCRConfig{Enabled: false, TimeoutSeconds: 30},
		ReputationAPI:  ReputationAPIConfig{Backend: "none"},
		Scores: ScoresConfig{
			DenyListMatch:    100,
			ReputationAPIHit: 90,
		},
	}
}
