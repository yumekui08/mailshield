package urlcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const defaultSafeBrowsingEndpoint = "https://safebrowsing.googleapis.com/v4/threatMatches:find"
const defaultSafeBrowsingClientID = "mailshield"
const defaultSafeBrowsingClientVersion = "1.0"

// safeBrowsingChecker は Google Safe Browsing API v4 を使う reputationChecker 実装。
// 非商用利用向け。バッチ API のため複数 URL を1リクエストで検査できる。
type safeBrowsingChecker struct {
	apiKey        string
	endpoint      string
	client        *http.Client
	clientID      string
	clientVersion string
}

func newSafeBrowsingChecker(apiKey, endpoint string, timeoutSeconds int, clientID, clientVersion string) *safeBrowsingChecker {
	if endpoint == "" {
		endpoint = defaultSafeBrowsingEndpoint
	}
	if timeoutSeconds == 0 {
		timeoutSeconds = 10
	}
	if clientID == "" {
		clientID = defaultSafeBrowsingClientID
	}
	if clientVersion == "" {
		clientVersion = defaultSafeBrowsingClientVersion
	}
	return &safeBrowsingChecker{
		apiKey:        apiKey,
		endpoint:      endpoint,
		client:        &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second},
		clientID:      clientID,
		clientVersion: clientVersion,
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
	body.Client.ClientID = c.clientID
	body.Client.ClientVersion = c.clientVersion
	body.ThreatInfo.ThreatTypes = []string{"MALWARE", "SOCIAL_ENGINEERING", "UNWANTED_SOFTWARE"}
	body.ThreatInfo.PlatformTypes = []string{"ANY_PLATFORM"}
	body.ThreatInfo.ThreatEntryTypes = []string{"URL"}
	body.ThreatInfo.ThreatEntries = entries

	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("Safe Browsing リクエスト JSON 作成失敗: %w", err)
	}

	endpoint := c.endpoint + "?key=" + c.apiKey
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
