package arcsealer

import (
	"crypto/rand"
	"crypto/rsa"
	"strings"
	"testing"
)

// テスト用の最小 EML（CRLF 行末）
const testEML = "From: sender@example.com\r\n" +
	"To: recipient@example.com\r\n" +
	"Subject: Test\r\n" +
	"Message-ID: <test@example.com>\r\n" +
	"Date: Mon, 01 Jan 2024 00:00:00 +0000\r\n" +
	"Content-Type: text/plain\r\n" +
	"Authentication-Results: mail.example.com;\r\n" +
	"  spf=pass smtp.mailfrom=sender@example.com;\r\n" +
	"  dkim=pass header.d=example.com\r\n" +
	"\r\n" +
	"Hello, World!\r\n"

func newTestWorker(t *testing.T) *Worker {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("RSA 鍵生成失敗: %v", err)
	}
	return &Worker{
		cfg: &Config{
			SigningDomain: "arc.example.com",
			Selector:      "test",
			HeaderKeys:    defaultHeaderKeys,
		},
		signer: key,
	}
}

func TestSealARC_AddsARCHeaders(t *testing.T) {
	w := newTestWorker(t)
	result, err := w.sealARC([]byte(testEML))
	if err != nil {
		t.Fatalf("sealARC 失敗: %v", err)
	}

	out := string(result)

	// 3 種類の ARC ヘッダーが付与されることを確認
	for _, header := range []string{
		"ARC-Seal:",
		"ARC-Message-Signature:",
		"ARC-Authentication-Results:",
	} {
		if !strings.Contains(out, header) {
			t.Errorf("ヘッダーが見つかりません: %s", header)
		}
	}
}

func TestSealARC_InstanceNumber(t *testing.T) {
	w := newTestWorker(t)

	// 1 回目: i=1
	result, err := w.sealARC([]byte(testEML))
	if err != nil {
		t.Fatalf("1 回目のシール失敗: %v", err)
	}
	if !strings.Contains(string(result), "i=1;") {
		t.Error("1 回目のシールで i=1 が見つかりません")
	}

	// 2 回目: i=2（既存 ARC セットに続けてシール）
	result2, err := w.sealARC(result)
	if err != nil {
		t.Fatalf("2 回目のシール失敗: %v", err)
	}
	if !strings.Contains(string(result2), "i=2;") {
		t.Error("2 回目のシールで i=2 が見つかりません")
	}
}

func TestSealARC_ChainValidation(t *testing.T) {
	w := newTestWorker(t)

	// 1 回目: cv=none
	result, err := w.sealARC([]byte(testEML))
	if err != nil {
		t.Fatalf("シール失敗: %v", err)
	}
	if !strings.Contains(string(result), "cv=none") {
		t.Error("1 回目のシールで cv=none が見つかりません")
	}

	// 2 回目: 既存チェーンあり。前段チェーンの暗号検証は未実装のため、
	// 未検証で cv=pass を主張せず安全側に cv=fail とする（B-22）。
	result2, err := w.sealARC(result)
	if err != nil {
		t.Fatalf("2 回目のシール失敗: %v", err)
	}
	if !strings.Contains(string(result2), "cv=fail") {
		t.Error("2 回目のシールで cv=fail が見つかりません（未検証チェーンに cv=pass を付けてはいけない）")
	}
	if strings.Contains(string(result2), "cv=pass") {
		t.Error("未検証の前段チェーンに cv=pass を付けてはいけない")
	}
}

func TestSealARC_OriginalMessagePreserved(t *testing.T) {
	w := newTestWorker(t)
	result, err := w.sealARC([]byte(testEML))
	if err != nil {
		t.Fatalf("sealARC 失敗: %v", err)
	}

	// 元のボディが保持されていることを確認
	if !strings.Contains(string(result), "Hello, World!") {
		t.Error("元のメールボディが保持されていません")
	}
	// 元の From ヘッダーが保持されていることを確認
	if !strings.Contains(string(result), "From: sender@example.com") {
		t.Error("元の From ヘッダーが保持されていません")
	}
}

// LF のみの EML（/simulate への直接投入や、件名のみ書き換える Lua 変換ワーカーを
// 経由した場合は元の改行コードが保持される）でもシールできることを確認する。
func TestSealARC_LFOnlyEML(t *testing.T) {
	w := newTestWorker(t)
	lfEML := strings.ReplaceAll(testEML, "\r\n", "\n")

	result, err := w.sealARC([]byte(lfEML))
	if err != nil {
		t.Fatalf("LF のみの EML で sealARC 失敗: %v", err)
	}

	out := string(result)
	for _, header := range []string{
		"ARC-Seal:",
		"ARC-Message-Signature:",
		"ARC-Authentication-Results:",
	} {
		if !strings.Contains(out, header) {
			t.Errorf("ヘッダーが見つかりません: %s", header)
		}
	}
	if !strings.Contains(out, "Hello, World!") {
		t.Error("元のメールボディが保持されていません")
	}
}

// LF のみの EML と CRLF の EML で AMS のボディハッシュ（bh=）が一致することを確認する。
// relaxed 正規化は改行コードに依存しないため、署名は同一になるべき。
func TestSealARC_LFAndCRLFSameBodyHash(t *testing.T) {
	lfBody := []byte("Hello, World!\n")
	crlfBody := []byte("Hello, World!\r\n")
	if bodyHash(lfBody) != bodyHash(crlfBody) {
		t.Error("LF と CRLF のボディで bodyHash が一致しません")
	}
}

func TestSplitHeaderBody(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantHeader string
		wantBody   string
		wantOK     bool
	}{
		{
			name:       "CRLF",
			raw:        "From: a@b.com\r\nSubject: x\r\n\r\nbody\r\n",
			wantHeader: "From: a@b.com\r\nSubject: x\r\n",
			wantBody:   "body\r\n",
			wantOK:     true,
		},
		{
			name:       "LF",
			raw:        "From: a@b.com\nSubject: x\n\nbody\n",
			wantHeader: "From: a@b.com\nSubject: x\n",
			wantBody:   "body\n",
			wantOK:     true,
		},
		{
			name:   "区切りなし",
			raw:    "From: a@b.com\r\n",
			wantOK: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			header, body, ok := splitHeaderBody([]byte(tc.raw))
			if ok != tc.wantOK {
				t.Fatalf("ok: got %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if string(header) != tc.wantHeader {
				t.Errorf("header: got %q, want %q", header, tc.wantHeader)
			}
			if string(body) != tc.wantBody {
				t.Errorf("body: got %q, want %q", body, tc.wantBody)
			}
		})
	}
}

func TestRelaxedHeader(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			input: "From: Sender Name <sender@example.com>\r\n",
			want:  "from:Sender Name <sender@example.com>\r\n",
		},
		{
			input: "Subject: Hello  World\r\n",
			want:  "subject:Hello World\r\n",
		},
		{
			input: "ARC-Seal: i=1;\r\n  a=rsa-sha256;\r\n  d=example.com\r\n",
			want:  "arc-seal:i=1; a=rsa-sha256; d=example.com\r\n",
		},
	}
	for _, tc := range tests {
		got := relaxedHeader(tc.input)
		if got != tc.want {
			t.Errorf("relaxedHeader(%q)\n got:  %q\n want: %q", tc.input, got, tc.want)
		}
	}
}

func TestBodyHash_Deterministic(t *testing.T) {
	body := []byte("Hello, World!\r\n")
	h1 := bodyHash(body)
	h2 := bodyHash(body)
	if h1 != h2 {
		t.Error("bodyHash は同じ入力に対して同じ値を返す必要があります")
	}
}

func TestCountARCSets(t *testing.T) {
	eml := "ARC-Seal: i=1; a=rsa-sha256\r\n" +
		"ARC-Seal: i=2; a=rsa-sha256\r\n" +
		"From: a@b.com\r\n" +
		"\r\n"
	headers := parseHeaders([]byte(eml))
	n := countARCSets(headers)
	if n != 2 {
		t.Errorf("countARCSets: got %d, want 2", n)
	}
}
