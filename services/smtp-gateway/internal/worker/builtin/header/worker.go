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
	SPFFail              int `yaml:"spf_fail"`
	DKIMFail             int `yaml:"dkim_fail"`
	DMARCFail            int `yaml:"dmarc_fail"`
	ReplyToMismatch      int `yaml:"reply_to_mismatch"`
	BrandSpoofing        int `yaml:"brand_spoofing"`
	DisplayNameSpoofing  int `yaml:"display_name_spoofing"`
	EnvelopeFromMismatch int `yaml:"envelope_from_mismatch"`
	LookalikeDomain      int `yaml:"lookalike_domain"`
}

// Config は header-inspector の設定を保持する。
type Config struct {
	// Threshold はこのスコア以上で detected=true にする閾値。
	Threshold  int          `yaml:"threshold"`
	Scores     ScoresConfig `yaml:"scores"`
	BrandNames []string     `yaml:"brand_names"`
	// InternalDomains は自組織のドメイン。lookalike 検知と表示名偽装の基準に使う。
	InternalDomains []string `yaml:"internal_domains"`
}

// Worker はヘッダー検査ワーカーである。
type Worker struct {
	threshold       int
	scores          ScoresConfig
	brandNames      []string // 小文字に正規化済み
	internalDomains []string // 小文字に正規化済み
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
	internal := make([]string, len(cfg.InternalDomains))
	for i, d := range cfg.InternalDomains {
		internal[i] = strings.ToLower(strings.TrimSpace(d))
	}

	return &Worker{
		threshold:       cfg.Threshold,
		scores:          cfg.Scores,
		brandNames:      brands,
		internalDomains: internal,
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

	envelopeFromDomain := extractDomain(envelopeFrom)

	// Reply-To ドメイン不一致チェック（ヘッダー From ドメインと比較）
	fromHeader := msg.Header.Get("From")
	fromHeaderDomain := extractDomainFromHeader(fromHeader)

	replyTo := msg.Header.Get("Reply-To")
	if replyTo != "" && fromHeaderDomain != "" {
		replyToDomain := extractDomainFromHeader(replyTo)
		if replyToDomain != "" && !strings.EqualFold(fromHeaderDomain, replyToDomain) {
			score += w.scores.ReplyToMismatch
			reasons = append(reasons, "reply_to_mismatch")
			details["reply_to"] = replyTo
		}
	}

	// From（ヘッダー）と envelope-from のドメイン乖離チェック。
	// メーリングリスト等では正当に乖離することもあるが、なりすましの典型兆候でもある。
	if fromHeaderDomain != "" && envelopeFromDomain != "" &&
		!strings.EqualFold(fromHeaderDomain, envelopeFromDomain) {
		score += w.scores.EnvelopeFromMismatch
		reasons = append(reasons, "envelope_from_mismatch")
		details["header_from_domain"] = fromHeaderDomain
		details["envelope_from_domain"] = envelopeFromDomain
	}

	if fromHeader != "" {
		if addr, err := mail.ParseAddress(fromHeader); err == nil {
			// ブランドなりすまし（表示名にブランド名を含むが実ドメインが一致しない）
			if addr.Name != "" {
				nameLower := strings.ToLower(addr.Name)
				for _, brand := range w.brandNames {
					if strings.Contains(nameLower, brand) && !strings.Contains(fromHeaderDomain, brand) {
						score += w.scores.BrandSpoofing
						reasons = append(reasons, "brand_spoofing:"+brand)
						details["from_name"] = addr.Name
						details["from_domain"] = fromHeaderDomain
						break
					}
				}

				// 表示名偽装（表示名にメールアドレスを埋め込み、実 From と別ドメイン）。
				// 例: "経理部 <ceo@corp.example>" と表示しつつ実体は attacker@evil.test
				if embedded := embeddedAddressDomain(addr.Name); embedded != "" &&
					!strings.EqualFold(embedded, fromHeaderDomain) {
					score += w.scores.DisplayNameSpoofing
					reasons = append(reasons, "display_name_spoofing")
					details["display_name_address_domain"] = embedded
				}
			}

			// lookalike ドメイン（自組織ドメインに酷似だが完全一致でない）
			if lookalikeOf := w.matchLookalike(fromHeaderDomain); lookalikeOf != "" {
				score += w.scores.LookalikeDomain
				reasons = append(reasons, "lookalike_domain:"+lookalikeOf)
				details["lookalike_target"] = lookalikeOf
			}
		}
	}

	return score, reasons, details
}

// embeddedAddressDomain は表示名にメールアドレスが埋め込まれている場合の
// そのドメイン部を返す（なければ空）。
func embeddedAddressDomain(displayName string) string {
	at := strings.LastIndex(displayName, "@")
	if at < 0 {
		return ""
	}
	rest := displayName[at+1:]
	// ドメインとして妥当な範囲を切り出す（空白・引用符・山括弧で終端）
	end := strings.IndexAny(rest, " \t\"'<>()")
	if end >= 0 {
		rest = rest[:end]
	}
	rest = strings.Trim(strings.ToLower(rest), ".")
	if !strings.Contains(rest, ".") {
		return ""
	}
	return rest
}

// matchLookalike は domain が内部ドメインのいずれかに酷似（ただし完全一致でない）
// 場合、その内部ドメインを返す。一致・非類似なら空文字列。
func (w *Worker) matchLookalike(domain string) string {
	if domain == "" {
		return ""
	}
	for _, internal := range w.internalDomains {
		if internal == "" || domain == internal {
			continue // 完全一致は正当
		}
		if isLookalike(domain, internal) {
			return internal
		}
	}
	return ""
}

// isLookalike は 2 つのドメインが視覚的に紛らわしいかを返す。
//   - confusable 文字を正規化して一致（0↔o, 1↔l/i, rn↔m 等）
//   - またはレーベンシュタイン距離が 1（1 文字の挿入・削除・置換）
func isLookalike(a, b string) bool {
	if normalizeConfusable(a) == normalizeConfusable(b) {
		return true
	}
	return levenshtein(a, b) == 1
}

// normalizeConfusable は視覚的に紛らわしい文字を代表文字へ正規化する。
func normalizeConfusable(s string) string {
	r := strings.NewReplacer(
		"0", "o", "1", "l", "i", "l", "|", "l",
		"5", "s", "8", "b", "rn", "m", "vv", "w",
	)
	return r.Replace(strings.ToLower(s))
}

// levenshtein は 2 文字列の編集距離を返す。
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min3(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
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
			SPFFail:              30,
			DKIMFail:             40,
			DMARCFail:            30,
			ReplyToMismatch:      40,
			BrandSpoofing:        60,
			DisplayNameSpoofing:  60,
			EnvelopeFromMismatch: 30,
			LookalikeDomain:      70,
		},
		BrandNames: []string{
			"amazon", "google", "microsoft", "paypal", "apple",
			"rakuten", "yahoo", "ntt", "docomo", "softbank",
		},
		// InternalDomains は既定では空（lookalike 検知は設定した組織でのみ有効）。
		InternalDomains: nil,
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
