package qrcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const webRiskEndpoint = "https://webrisk.googleapis.com/v1/uris:search"

// webRiskChecker は Google Web Risk API v1 を使う reputationChecker 実装。
// 商用利用向け（Google Cloud 従量課金）。URL を1件ずつ GET で検査する。
type webRiskChecker struct {
	apiKey string
	client *http.Client
}

func newWebRiskChecker(apiKey string) *webRiskChecker {
	return &webRiskChecker{
		apiKey: apiKey,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

var webRiskThreatTypes = []string{"MALWARE", "SOCIAL_ENGINEERING", "UNWANTED_SOFTWARE"}

func (c *webRiskChecker) Check(ctx context.Context, urls []string) ([]string, error) {
	var hits []string
	for _, u := range urls {
		hit, err := c.checkOne(ctx, u)
		if err != nil {
			return hits, fmt.Errorf("Web Risk API 呼び出し失敗 (url=%s): %w", u, err)
		}
		if hit {
			hits = append(hits, u)
		}
	}
	return hits, nil
}

func (c *webRiskChecker) checkOne(ctx context.Context, rawURL string) (bool, error) {
	params := url.Values{}
	params.Set("uri", rawURL)
	params.Set("key", c.apiKey)
	for _, t := range webRiskThreatTypes {
		params.Add("threatTypes", t)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, webRiskEndpoint+"?"+params.Encode(), nil)
	if err != nil {
		return false, fmt.Errorf("HTTP リクエスト作成失敗: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return false, fmt.Errorf("HTTP 呼び出し失敗: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("Web Risk API 異常レスポンス: status=%d", resp.StatusCode)
	}

	var result struct {
		Threat *struct {
			ThreatTypes []string `json:"threatTypes"`
		} `json:"threat"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return false, fmt.Errorf("レスポンスパース失敗: %w", err)
	}

	return result.Threat != nil && len(result.Threat.ThreatTypes) > 0, nil
}
