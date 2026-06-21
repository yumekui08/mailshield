//go:build e2e

// api_test.go は api-server の REST API エンドポイントをテストする。
//
// 前提:
//   - `make api-up` で api-server が localhost:8090 で起動済み
//   - infra/mariadb/init/002_seed.sql のシードデータが適用済み
//     （admin@internal.test / password でログイン可能）
//
// 実行方法:
//
//	cd tests/e2e && go test -v -tags e2e -run TestAPI ./...
package e2e_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// TestAPI_Health は /healthz エンドポイントが 200 を返すことを確認する。
func TestAPI_Health(t *testing.T) {
	requireAPI(t)
	code, _ := apiGet(t, "/healthz", "")
	if code != http.StatusOK {
		t.Errorf("GET /healthz: want 200, got %d", code)
	}
}

// TestAPI_Stats_RequiresAuth は認証なしで /api/v1/stats にアクセスすると
// 401 が返ることを確認する。
func TestAPI_Stats_RequiresAuth(t *testing.T) {
	requireAPI(t)
	code, _ := apiGet(t, "/api/v1/stats", "")
	if code != http.StatusUnauthorized {
		t.Errorf("認証なし GET /api/v1/stats: want 401, got %d", code)
	}
}

// TestAPI_Login_Standalone はスタンドアロン認証でログインして
// セッション Cookie を取得できることを確認する。
func TestAPI_Login_Standalone(t *testing.T) {
	requireAPI(t)
	cookie := apiLogin(t)
	if cookie == "" {
		t.Fatal("ログインでセッション Cookie が取得できませんでした")
	}
	// 取得した Cookie でアクセスできることを確認
	code, _ := apiGet(t, "/api/v1/stats", cookie)
	if code != http.StatusOK {
		t.Errorf("ログイン後 GET /api/v1/stats: want 200, got %d", code)
	}
}

// TestAPI_Messages_List はメッセージ一覧が items フィールドを持って返ることを確認する。
// メールが 0 件でも items が空配列として含まれる必要がある。
func TestAPI_Messages_List(t *testing.T) {
	requireAPI(t)
	cookie := apiLogin(t)

	code, body := apiGet(t, "/api/v1/messages", cookie)
	if code != http.StatusOK {
		t.Fatalf("GET /api/v1/messages: want 200, got %d: %s", code, body)
	}

	var result struct {
		Items []json.RawMessage `json:"items"`
		Total int               `json:"total"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("レスポンスのデコード失敗: %v", err)
	}
	if result.Items == nil {
		t.Error("items フィールドが null です（空配列が期待されます）")
	}
}

// TestAPI_Quarantine_List は隔離メール一覧が items フィールドを持って返ることを確認する。
func TestAPI_Quarantine_List(t *testing.T) {
	requireAPI(t)
	cookie := apiLogin(t)

	code, body := apiGet(t, "/api/v1/quarantine", cookie)
	if code != http.StatusOK {
		t.Fatalf("GET /api/v1/quarantine: want 200, got %d: %s", code, body)
	}

	var result struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("レスポンスのデコード失敗: %v", err)
	}
	if result.Items == nil {
		t.Error("items フィールドが null です（空配列が期待されます）")
	}
}

// TestAPI_Simulate_ProxiesToGateway は api-server の /api/v1/simulate が
// smtp-gateway の /simulate に正しくプロキシすることを確認する。
func TestAPI_Simulate_ProxiesToGateway(t *testing.T) {
	requireAPI(t)
	requireGateway(t)
	cookie := apiLogin(t)

	eml, err := io.ReadAll(bytes.NewBufferString(
		"From: sender@external.test\r\n" +
			"To: user@example.com\r\n" +
			"Subject: API proxy test\r\n" +
			"\r\n" +
			"Test body.\r\n",
	))
	if err != nil {
		t.Fatal(err)
	}

	code, body := apiPost(t, "/api/v1/simulate", cookie, "message/rfc822", eml)
	if code != http.StatusOK {
		t.Fatalf("POST /api/v1/simulate: want 200, got %d: %s", code, body)
	}

	var result struct {
		Action    string `json:"action"`
		RouteName string `json:"route_name"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("シミュレーターレスポンスのデコード失敗: %v", err)
	}
	if result.Action == "" {
		t.Error("simulate レスポンスに action フィールドがありません")
	}
	if result.RouteName == "" {
		t.Error("simulate レスポンスに route_name フィールドがありません")
	}
}

// TestAPI_AuditLogs_AdminOnly は監査ログ API が admin のみアクセス可能であることを確認する。
func TestAPI_AuditLogs_AdminOnly(t *testing.T) {
	requireAPI(t)
	// 認証なしアクセスは 401
	code, _ := apiGet(t, "/api/v1/audit-logs", "")
	if code != http.StatusUnauthorized {
		t.Errorf("認証なし GET /api/v1/audit-logs: want 401, got %d", code)
	}
	// admin ログインで 200
	cookie := apiLogin(t)
	code, body := apiGet(t, "/api/v1/audit-logs", cookie)
	if code != http.StatusOK {
		t.Errorf("admin GET /api/v1/audit-logs: want 200, got %d: %s", code, body)
	}
}

// TestAPI_Login_WrongPassword は誤ったパスワードでログインすると 401 が返ることを確認する。
func TestAPI_Login_WrongPassword(t *testing.T) {
	requireAPI(t)
	payload, _ := json.Marshal(map[string]string{
		"email":    apiAdminEmail(),
		"password": "wrong-password",
	})
	resp, err := http.Post(
		apiURL()+"/api/v1/auth/login",
		"application/json",
		bytes.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("ログインリクエスト失敗: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("誤パスワードログイン: want 401, got %d", resp.StatusCode)
	}
}
