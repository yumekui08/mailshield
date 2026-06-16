package urlcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const safeBrowsingEndpoint = "https://safebrowsing.googleapis.com/v4/threatMatches:find"

// safeBrowsingChecker は Google Safe Browsing API v4 を使う reputationChecker 実装。
// 非商用利用向け。バッチ API のため複数 URL を1リクエストで検査できる。
type safeBrowsingChecker struct {
	apiKey string
	client *http.Client
}

func newSafeBrowsingChecker(apiKey string) *safeBrowsingChecker {
	return &safeBrowsingChecker{
		apiKey: apiKey,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Check は Safe Browsing API に URL をバッチ送信し、脅威が検出された URL を返す。
func (c *safeBrowsingChecker) Check(ctx context.Context, urls []string) ([]string, error) {
	type threatEntry struct {
		URL string `json:"url"`
	}
	type reqBody struct {
		Client struct {
			ClientID      string `json:"clientId"`
			ClientVersion string `json:"clientVersion"`
		} `json:"client"`
		ThreatInfo struct {
			ThreatTypes      []string      `json:"threatTypes"`
			PlatformTypes    []string      `json:"platformTypes"`
			ThreatEntryTypes []string      `json:"threatEntryTypes"`
			ThreatEntries    []threatEntry `json:"threatEntries"`
		} `json:"threatInfo"`
	}

	entries := make([]threatEntry, len(urls))
	for i, u := range urls {
		entries[i] = threatEntry{URL: u}
	}

	var body reqBody
	body.Client.ClientID = "mailshield"
	body.Client.ClientVersion = "1.0"
	body.ThreatInfo.ThreatTypes = []string{"MALWARE", "SOCIAL_ENGINEERING", "UNWANTED_SOFTWARE"}
	body.ThreatInfo.PlatformTypes = []string{"ANY_PLATFORM"}
	body.ThreatInfo.ThreatEntryTypes = []string{"URL"}
	body.ThreatInfo.ThreatEntries = entries

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("Safe Browsing リクエスト JSON 作成失敗: %w", err)
	}

	endpoint := safeBrowsingEndpoint + "?key=" + c.apiKey
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("Safe Browsing HTTP リクエスト作成失敗: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Safe Browsing API 呼び出し失敗: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Safe Browsing API 異常レスポンス: status=%d", resp.StatusCode)
	}

	var result struct {
		Matches []struct {
			Threat struct {
				URL string `json:"url"`
			} `json:"threat"`
		} `json:"matches"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("Safe Browsing レスポンスパース失敗: %w", err)
	}

	hits := make([]string, 0, len(result.Matches))
	for _, m := range result.Matches {
		hits = append(hits, m.Threat.URL)
	}
	return hits, nil
}
