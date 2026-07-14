// Package urlcheck はメール本文内の URL を deny リストおよび外部レピュテーション API
// で検査する inspect ワーカーを実装する。
// 外部 API は Safe Browsing（非商用）と Web Risk（商用）を設定で切り替えられる。
package urlcheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jhillyerd/enmime"
	"gopkg.in/yaml.v3"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

const workerName = "url-worker"

// reputationChecker は外部 URL レピュテーション API の抽象インターフェース。
// コンシューマー側（本パッケージ）で定義し、実装は safebrowsing.go / webrisk.go が担う。
type reputationChecker interface {
	Check(ctx context.Context, urls []string) (hits []string, err error)
}

// ReputationAPIConfig は外部 API の設定を保持する。
type ReputationAPIConfig struct {
	// Backend は使用するバックエンド（none / safe_browsing / web_risk）。
	Backend string `yaml:"backend"`
	// APIKey はバックエンドの API キー。環境変数 REPUTATION_API_KEY で上書き可能。
	APIKey string `yaml:"api_key"`
	// Endpoint はバックエンド API の URL。空の場合は各バックエンドのデフォルト値を使う。
	Endpoint string `yaml:"endpoint"`
	// TimeoutSeconds は HTTP クライアントのタイムアウト（秒）。
	TimeoutSeconds int `yaml:"timeout_seconds"`
	// ClientID は Safe Browsing API に送るアプリ識別子。
	ClientID string `yaml:"client_id"`
	// ClientVersion は Safe Browsing API に送るアプリバージョン。
	ClientVersion string `yaml:"client_version"`
}

// ScoresConfig は各検知項目のスコアを保持する。
type ScoresConfig struct {
	DenyListMatch    int `yaml:"deny_list_match"`
	ReputationAPIHit int `yaml:"reputation_api_hit"`
	// DisplayMismatch はアンカーの表示テキストとリンク先のドメイン不一致のスコア。
	DisplayMismatch int `yaml:"display_mismatch"`
}

// Config は url-worker の設定を保持する。
type Config struct {
	// MaxURLs はメール1通で検査する URL の上限。パフォーマンスとコストを制御する。
	MaxURLs       int                 `yaml:"max_urls"`
	DenyList      []string            `yaml:"deny_list"`
	ReputationAPI ReputationAPIConfig `yaml:"reputation_api"`
	Scores        ScoresConfig        `yaml:"scores"`
}

// Worker は URL レピュテーション検査ワーカーである。
type Worker struct {
	maxURLs    int
	denyList   []string // 小文字に正規化済み
	reputation reputationChecker
	scores     ScoresConfig
}

var (
	urlInTextPattern = regexp.MustCompile(`https?://\S+`)
	htmlAttrPattern  = regexp.MustCompile(`(?i)(?:href|src)="(https?://[^"]*)"`)
	// anchorPattern は <a href="...">表示テキスト</a> を捕捉する（1=href, 2=表示テキスト）。
	anchorPattern = regexp.MustCompile(`(?is)<a\s[^>]*href="(https?://[^"]+)"[^>]*>(.*?)</a>`)
	tagStripper   = regexp.MustCompile(`(?s)<[^>]+>`)
)

// New は url-worker を初期化する。
func New(workerConfigDir string) (*Worker, error) {
	cfg, err := loadConfig(workerConfigDir)
	if err != nil {
		return nil, fmt.Errorf("url-worker 設定ロード失敗: %w", err)
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

	return &Worker{
		maxURLs:    cfg.MaxURLs,
		denyList:   denyList,
		reputation: checker,
		scores:     cfg.Scores,
	}, nil
}

func (w *Worker) Name() string { return workerName }

// Inspect は EML から URL を抽出し deny リストと外部 API で検査する。
func (w *Worker) Inspect(ctx context.Context, m *domain.Mail) (*domain.InspectResult, error) {
	result := &domain.InspectResult{
		WorkerName: workerName,
		Details:    make(map[string]any),
	}

	urls := w.extractURLs(m.RawEML)
	result.Details["total_urls_checked"] = len(urls)

	// 表示テキストとリンク先の不一致検知（HTML アンカー）。URL 抽出結果とは独立に行う。
	mismatches := w.detectDisplayMismatch(m.RawEML)
	maxScore := 0
	if len(mismatches) > 0 {
		result.Details["display_mismatches"] = mismatches
		if w.scores.DisplayMismatch > maxScore {
			maxScore = w.scores.DisplayMismatch
		}
	}

	if len(urls) == 0 {
		if maxScore > 100 {
			maxScore = 100
		}
		result.Score = maxScore
		result.Detected = len(mismatches) > 0
		return result, nil
	}

	var denyHits, apiHits []string

	// 1. deny リスト照合（ローカル・高速）
	for _, u := range urls {
		if w.isDenied(u) {
			denyHits = append(denyHits, u)
			if w.scores.DenyListMatch > maxScore {
				maxScore = w.scores.DenyListMatch
			}
		}
	}

	// 2. 外部 API 照合（deny リストにない URL のみ）
	if w.reputation != nil {
		toCheck := exclude(urls, denyHits)
		if len(toCheck) > 0 {
			hits, err := w.reputation.Check(ctx, toCheck)
			if err != nil {
				// API エラーは Details に記録して続行（deny リスト結果は維持）
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
	result.Detected = len(denyHits) > 0 || len(apiHits) > 0 || len(mismatches) > 0

	return result, nil
}

// detectDisplayMismatch は HTML アンカーの表示テキストがリンク先と異なるドメインの
// URL を「表示」している場合を検知する（フィッシングの典型: 表示は正規サイト、
// リンク先は攻撃者サイト）。表示テキストがドメインを含まない場合は対象外。
func (w *Worker) detectDisplayMismatch(rawEML []byte) []string {
	env, err := enmime.ReadEnvelope(bytes.NewReader(rawEML))
	if err != nil {
		return nil
	}
	html := env.HTML
	if html == "" {
		return nil
	}

	var mismatches []string
	seen := make(map[string]bool)
	for _, m := range anchorPattern.FindAllStringSubmatch(html, -1) {
		if len(m) != 3 {
			continue
		}
		hrefDomain := hostOf(m[1])
		// 表示テキストからタグを除去し、そこに現れるドメインを取り出す
		displayText := strings.TrimSpace(tagStripper.ReplaceAllString(m[2], ""))
		displayDomain := domainFromDisplayText(displayText)
		if hrefDomain == "" || displayDomain == "" {
			continue
		}
		if !sameRegistrableDomain(displayDomain, hrefDomain) {
			key := displayDomain + "→" + hrefDomain
			if !seen[key] {
				seen[key] = true
				mismatches = append(mismatches, key)
			}
		}
	}
	return mismatches
}

// hostOf は URL のホスト部（小文字）を返す。
func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// displayDomainPattern は表示テキスト中の裸ドメイン / URL を捕捉する。
var displayDomainPattern = regexp.MustCompile(`(?i)(?:https?://)?([a-z0-9.-]+\.[a-z]{2,})`)

// domainFromDisplayText は表示テキストに現れる最初のドメインを返す（なければ空）。
func domainFromDisplayText(text string) string {
	m := displayDomainPattern.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	return strings.ToLower(strings.Trim(m[1], "."))
}

// sameRegistrableDomain は 2 つのホストが同じ登録可能ドメイン（末尾 2 ラベル）かを返す。
// 厳密な Public Suffix List 判定ではないが、サブドメイン差（www.example.com と example.com）を
// 同一扱いにする実用的な近似。
func sameRegistrableDomain(a, b string) bool {
	return registrable(a) == registrable(b)
}

func registrable(host string) string {
	labels := strings.Split(strings.ToLower(host), ".")
	if len(labels) <= 2 {
		return strings.Join(labels, ".")
	}
	return strings.Join(labels[len(labels)-2:], ".")
}

// extractURLs は EML からユニークな URL を最大 maxURLs 件抽出する。
// enmime パース失敗時は raw テキストから直接抽出する。
func (w *Worker) extractURLs(rawEML []byte) []string {
	env, err := enmime.ReadEnvelope(bytes.NewReader(rawEML))
	if err != nil {
		return w.extractFromRaw(string(rawEML))
	}

	seen := make(map[string]struct{})
	var result []string

	addURL := func(raw string) {
		u := strings.TrimRight(raw, ".,;:!?\"')")
		if u == "" {
			return
		}
		key := strings.ToLower(u)
		if _, ok := seen[key]; ok {
			return
		}
		if len(result) >= w.maxURLs {
			return
		}
		seen[key] = struct{}{}
		result = append(result, u)
	}

	for _, u := range urlInTextPattern.FindAllString(env.Text, -1) {
		addURL(u)
	}
	for _, m := range htmlAttrPattern.FindAllStringSubmatch(env.HTML, -1) {
		if len(m) == 2 {
			addURL(m[1])
		}
	}
	return result
}

func (w *Worker) extractFromRaw(text string) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, u := range urlInTextPattern.FindAllString(text, -1) {
		u = strings.TrimRight(u, ".,;:!?\"')")
		if u == "" {
			continue
		}
		key := strings.ToLower(u)
		if _, ok := seen[key]; ok {
			continue
		}
		if len(result) >= w.maxURLs {
			break
		}
		seen[key] = struct{}{}
		result = append(result, u)
	}
	return result
}

// isDenied はホスト名が deny リストに一致するか確認する（サブドメインも対象）。
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

// exclude は all スライスから remove に含まれる要素を除外して返す。
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
	if cfg.MaxURLs <= 0 {
		cfg.MaxURLs = defaultConfig().MaxURLs
	}
	if cfg.Scores == (ScoresConfig{}) {
		cfg.Scores = defaultConfig().Scores
	}
	return &cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		MaxURLs: 20,
		ReputationAPI: ReputationAPIConfig{
			Backend: "none",
		},
		Scores: ScoresConfig{
			DenyListMatch:    100,
			ReputationAPIHit: 90,
			DisplayMismatch:  70,
		},
	}
}
