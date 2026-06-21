//go:build e2e

package e2e_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"net/smtp"
	"os"
	"strings"
	"testing"
	"time"
)

// ─── エンドポイント URL ──────────────────────────────────────────────────────

func gatewayURL() string {
	if v := os.Getenv("MAILSHIELD_GATEWAY_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://localhost:8080"
}

func apiURL() string {
	if v := os.Getenv("MAILSHIELD_API_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://localhost:8090"
}

func mailpitURL() string {
	if v := os.Getenv("MAILSHIELD_MAILPIT_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://localhost:8025"
}

// smtpAddr は受信 SMTP（inbound Postfix）のアドレスを返す。
func smtpAddr() string {
	host := os.Getenv("MAILSHIELD_SMTP_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("MAILSHIELD_SMTP_PORT")
	if port == "" {
		port = "25"
	}
	return host + ":" + port
}

// ─── サービス到達確認（到達できない場合はテストをスキップ） ────────────────

func requireGateway(t *testing.T) {
	t.Helper()
	resp, err := http.Get(gatewayURL() + "/healthz")
	if err != nil {
		t.Skipf("smtp-gateway に到達できません (%s): %v", gatewayURL(), err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("smtp-gateway /healthz が %d を返しました", resp.StatusCode)
	}
}

func requireMailpit(t *testing.T) {
	t.Helper()
	resp, err := http.Get(mailpitURL() + "/livez")
	if err != nil {
		t.Skipf("Mailpit に到達できません (%s): %v", mailpitURL(), err)
	}
	resp.Body.Close()
}

func requireAPI(t *testing.T) {
	t.Helper()
	resp, err := http.Get(apiURL() + "/healthz")
	if err != nil {
		t.Skipf("api-server に到達できません (%s): %v", apiURL(), err)
	}
	resp.Body.Close()
}

// ─── シミュレーター型定義 ─────────────────────────────────────────────────

type SimulateResult struct {
	RouteName          string          `json:"route_name"`
	Direction          string          `json:"direction"`
	InspectResults     []InspectResult `json:"inspect_results"`
	OriginalSubject    string          `json:"original_subject"`
	TransformedSubject string          `json:"transformed_subject"`
	SubjectChanged     bool            `json:"subject_changed"`
	TransformedEML     string          `json:"transformed_eml"`
	TransformError     string          `json:"transform_error,omitempty"`
	Action             string          `json:"action"`
	MatchedRule        string          `json:"matched_rule"`
	ProcessingMS       int64           `json:"processing_ms"`
}

type InspectResult struct {
	Worker   string                 `json:"worker"`
	Detected bool                   `json:"detected"`
	Score    int                    `json:"score"`
	Details  map[string]interface{} `json:"details"`
}

// ─── シミュレーターヘルパー ───────────────────────────────────────────────

// simulateRaw は生の EML を /simulate エンドポイントに POST し、ステータスコードとボディを返す。
func simulateRaw(t *testing.T, eml []byte) (int, []byte) {
	t.Helper()
	resp, err := http.Post(gatewayURL()+"/simulate", "message/rfc822", bytes.NewReader(eml))
	if err != nil {
		t.Fatalf("POST /simulate 失敗: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// simulate は EML を /simulate に送り、SimulateResult を返す。200 以外はテスト失敗。
func simulate(t *testing.T, eml []byte) *SimulateResult {
	t.Helper()
	code, body := simulateRaw(t, eml)
	if code != http.StatusOK {
		t.Fatalf("simulate returned %d: %s", code, string(body))
	}
	var r SimulateResult
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("SimulateResult のデコード失敗: %v", err)
	}
	return &r
}

// simulateFile は testdata/emls/ 以下のファイルを読んで simulate を呼ぶ。
func simulateFile(t *testing.T, path string) *SimulateResult {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("EML ファイル読み込み失敗 %s: %v", path, err)
	}
	return simulate(t, data)
}

// findWorkerResult は inspect_results の中から指定ワーカー名の結果を探す。
func findWorkerResult(r *SimulateResult, name string) *InspectResult {
	for i := range r.InspectResults {
		if r.InspectResults[i].Worker == name {
			return &r.InspectResults[i]
		}
	}
	return nil
}

// ─── Mailpit ヘルパー ─────────────────────────────────────────────────────

type mailpitMessage struct {
	ID      string `json:"ID"`
	Subject string `json:"Subject"`
	From    struct {
		Name    string `json:"Name"`
		Address string `json:"Address"`
	} `json:"From"`
	To []struct {
		Name    string `json:"Name"`
		Address string `json:"Address"`
	} `json:"To"`
	Snippet string `json:"Snippet"`
}

type mailpitListResponse struct {
	Messages []mailpitMessage `json:"messages"`
	Total    int              `json:"total"`
}

// clearMailpit は Mailpit の全メッセージを削除してテストを独立させる。
func clearMailpit(t *testing.T) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodDelete, mailpitURL()+"/api/v1/messages", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("clearMailpit: %v", err)
	}
	resp.Body.Close()
}

// waitForMailpit は subject に一致するメッセージが Mailpit に届くまで timeout 待つ。
func waitForMailpit(t *testing.T, subject string, timeout time.Duration) *mailpitMessage {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(mailpitURL() + "/api/v1/messages")
		if err == nil {
			var result mailpitListResponse
			if json.NewDecoder(resp.Body).Decode(&result) == nil {
				for i := range result.Messages {
					if result.Messages[i].Subject == subject {
						resp.Body.Close()
						return &result.Messages[i]
					}
				}
			}
			resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	return nil
}

// ─── SMTP 送信ヘルパー ────────────────────────────────────────────────────

// randomID はテストごとにユニークなメール件名を生成するための短いランダム文字列を返す。
func randomID() string {
	return fmt.Sprintf("%08x", rand.Int63())
}

// sendSMTP は net/smtp で平文メールを送信する。認証なし（開発環境のオープンリレー想定）。
func sendSMTP(from, to, subject, body string) error {
	msg := []byte(
		"From: " + from + "\r\n" +
			"To: " + to + "\r\n" +
			"Subject: " + subject + "\r\n" +
			"MIME-Version: 1.0\r\n" +
			"Content-Type: text/plain; charset=UTF-8\r\n" +
			"\r\n" +
			body + "\r\n",
	)
	return smtp.SendMail(smtpAddr(), nil, from, []string{to}, msg)
}

// ─── api-server ヘルパー ─────────────────────────────────────────────────

func apiAdminEmail() string {
	if v := os.Getenv("MAILSHIELD_ADMIN_EMAIL"); v != "" {
		return v
	}
	return "admin@internal.test"
}

func apiAdminPassword() string {
	if v := os.Getenv("MAILSHIELD_ADMIN_PASSWORD"); v != "" {
		return v
	}
	return "password"
}

// apiLogin はデフォルト管理者でログインしセッション Cookie 文字列を返す。
func apiLogin(t *testing.T) string {
	t.Helper()
	payload, _ := json.Marshal(map[string]string{
		"email":    apiAdminEmail(),
		"password": apiAdminPassword(),
	})
	resp, err := http.Post(
		apiURL()+"/api/v1/auth/login",
		"application/json",
		bytes.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("apiLogin: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("apiLogin %d: %s", resp.StatusCode, body)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "mailshield_session" {
			return c.Name + "=" + c.Value
		}
	}
	t.Fatal("ログインレスポンスにセッション Cookie がありません")
	return ""
}

func apiGet(t *testing.T, path, cookie string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, apiURL()+path, nil)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func apiPost(t *testing.T, path, cookie, contentType string, payload []byte) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, apiURL()+path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", contentType)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// ─── MIME デコードヘルパー ────────────────────────────────────────────────
// decodedEMLBodies は transformed_eml の全 MIME パートをデコードして結合した文字列を返す。
// base64 / quoted-printable エンコードされた本文でも文字列アサーションが行えるようにする。
func decodedEMLBodies(t *testing.T, rawEML string) string {
	t.Helper()
	if rawEML == "" {
		return ""
	}
	msg, err := mail.ReadMessage(strings.NewReader(rawEML))
	if err != nil {
		t.Logf("EML parse 警告: %v", err)
		return rawEML
	}
	ct := msg.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		body, _ := io.ReadAll(msg.Body)
		return decodePart(t, body, msg.Header.Get("Content-Transfer-Encoding"))
	}
	var sb strings.Builder
	mr := multipart.NewReader(msg.Body, params["boundary"])
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		body, _ := io.ReadAll(part)
		sb.WriteString(decodePart(t, body, part.Header.Get("Content-Transfer-Encoding")))
		sb.WriteByte('\n')
		part.Close()
	}
	return sb.String()
}

func decodePart(t *testing.T, body []byte, cte string) string {
	t.Helper()
	switch strings.ToLower(strings.TrimSpace(cte)) {
	case "base64":
		cleaned := strings.ReplaceAll(strings.ReplaceAll(string(body), "\r\n", ""), "\n", "")
		decoded, err := base64.StdEncoding.DecodeString(cleaned)
		if err != nil {
			t.Logf("base64 decode 警告: %v", err)
			return string(body)
		}
		return string(decoded)
	case "quoted-printable":
		r := quotedprintable.NewReader(bytes.NewReader(body))
		decoded, _ := io.ReadAll(r)
		return string(decoded)
	default:
		return string(body)
	}
}
