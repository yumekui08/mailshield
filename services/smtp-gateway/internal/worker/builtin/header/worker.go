// Package header はメールヘッダーのなりすまし兆候を検査する inspect ワーカーを実装する。
// SPF/DKIM/DMARC 認証失敗・Reply-To ドメイン不一致・ブランドなりすましをスコアリングする。
package header

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

const workerName = "header-inspector"

// ScoresConfig は各検知項目のスコアを保持する。
type ScoresConfig struct {
	SPFFail         int `yaml:"spf_fail"`
	DKIMFail        int `yaml:"dkim_fail"`
	DMARCFail       int `yaml:"dmarc_fail"`
	ReplyToMismatch int `yaml:"reply_to_mismatch"`
	BrandSpoofing   int `yaml:"brand_spoofing"`
}

// Config は header-inspector の設定を保持する。
type Config struct {
	// Threshold はこのスコア以上で detected=true にする閾値。
	Threshold  int          `yaml:"threshold"`
	Scores     ScoresConfig `yaml:"scores"`
	BrandNames []string     `yaml:"brand_names"`
}

// Worker はヘッダー検査ワーカーである。
type Worker struct {
	threshold  int
	scores     ScoresConfig
	brandNames []string // 小文字に正規化済み
}

// New は header-inspector を初期化する。
func New(workerConfigDir string) (*Worker, error) {
	cfg, err := loadConfig(workerConfigDir)
	if err != nil {
		return nil, fmt.Errorf("header-inspector 設定ロード失敗: %w", err)
	}

	brands := make([]string, len(cfg.BrandNames))
	for i, b := range cfg.BrandNames {
		brands[i] = strings.ToLower(b)
	}

	return &Worker{
		threshold:  cfg.Threshold,
		scores:     cfg.Scores,
		brandNames: brands,
	}, nil
}

func (w *Worker) Name() string { return workerName }

// Inspect はヘッダーのなりすまし兆候を検査してスコアを返す。
func (w *Worker) Inspect(_ context.Context, m *domain.Mail) (*domain.InspectResult, error) {
	result := &domain.InspectResult{
		WorkerName: workerName,
		Details:    make(map[string]any),
	}

	totalScore := 0
	var reasons []string

	// SPF/DKIM/DMARC チェック（domain.Mail に既にパース済み）
	if m.AuthResults.SPF == domain.AuthFail {
		totalScore += w.scores.SPFFail
		reasons = append(reasons, "spf_fail")
	}
	if m.AuthResults.DKIM == domain.AuthFail {
		totalScore += w.scores.DKIMFail
		reasons = append(reasons, "dkim_fail")
	}
	if m.AuthResults.DMARC == domain.AuthFail {
		totalScore += w.scores.DMARCFail
		reasons = append(reasons, "dmarc_fail")
	}

	// EML ヘッダーパース（Reply-To・ブランドなりすまし）
	headerScore, headerReasons, headerDetails := w.checkHeaders(m.RawEML, m.FromAddress)
	totalScore += headerScore
	reasons = append(reasons, headerReasons...)
	for k, v := range headerDetails {
		result.Details[k] = v
	}

	if len(reasons) > 0 {
		result.Details["reasons"] = reasons
	}
	if totalScore > 100 {
		totalScore = 100
	}
	result.Score = totalScore
	if totalScore >= w.threshold {
		result.Detected = true
	}

	return result, nil
}

// checkHeaders は EML を net/mail でパースし Reply-To とブランドなりすましを検査する。
// EML パース失敗時はスコア 0・理由なしで返す（auth results のスコアのみ使う）。
func (w *Worker) checkHeaders(rawEML []byte, envelopeFrom string) (score int, reasons []string, details map[string]any) {
	details = make(map[string]any)

	msg, err := mail.ReadMessage(bytes.NewReader(rawEML))
	if err != nil {
		return 0, nil, details
	}

	fromDomain := extractDomain(envelopeFrom)

	// Reply-To ドメイン不一致チェック
	replyTo := msg.Header.Get("Reply-To")
	if replyTo != "" {
		replyToDomain := extractDomainFromHeader(replyTo)
		if replyToDomain != "" && !strings.EqualFold(fromDomain, replyToDomain) {
			score += w.scores.ReplyToMismatch
			reasons = append(reasons, "reply_to_mismatch")
			details["reply_to"] = replyTo
		}
	}

	// ブランドなりすまし検査（From 表示名にブランド名を含むが実ドメインが一致しない）
	fromHeader := msg.Header.Get("From")
	if fromHeader != "" {
		addr, err := mail.ParseAddress(fromHeader)
		if err == nil && addr.Name != "" {
			nameLower := strings.ToLower(addr.Name)
			fromHeaderDomain := extractDomain(addr.Address)
			for _, brand := range w.brandNames {
				if strings.Contains(nameLower, brand) && !strings.Contains(fromHeaderDomain, brand) {
					score += w.scores.BrandSpoofing
					reasons = append(reasons, "brand_spoofing:"+brand)
					details["from_name"] = addr.Name
					details["from_domain"] = fromHeaderDomain
					break
				}
			}
		}
	}

	return score, reasons, details
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
	if cfg.Threshold == 0 {
		cfg.Threshold = defaultConfig().Threshold
	}
	if cfg.Scores == (ScoresConfig{}) {
		cfg.Scores = defaultConfig().Scores
	}
	if len(cfg.BrandNames) == 0 {
		cfg.BrandNames = defaultConfig().BrandNames
	}
	return &cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		Threshold: 60,
		Scores: ScoresConfig{
			SPFFail:         30,
			DKIMFail:        40,
			DMARCFail:       30,
			ReplyToMismatch: 40,
			BrandSpoofing:   60,
		},
		BrandNames: []string{
			"amazon", "google", "microsoft", "paypal", "apple",
			"rakuten", "yahoo", "ntt", "docomo", "softbank",
		},
	}
}

func extractDomain(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return ""
	}
	return strings.ToLower(parts[1])
}

func extractDomainFromHeader(headerValue string) string {
	addr, err := mail.ParseAddress(headerValue)
	if err != nil {
		return ""
	}
	return extractDomain(addr.Address)
}
