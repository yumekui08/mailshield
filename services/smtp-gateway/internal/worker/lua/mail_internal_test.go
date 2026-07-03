package lua

import (
	"strings"
	"testing"
)

func TestEncodeSubjectIfNeeded(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		wantSame   bool // ASCII のみ → 変換なし
		wantPrefix string
	}{
		{"ASCIIのみは変換しない", "Hello World", true, ""},
		{"日本語はRFC2047エンコードされる", "[迷惑メール注意] test", false, "=?UTF-8?b?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := encodeSubjectIfNeeded(tt.in)
			if tt.wantSame {
				if got != tt.in {
					t.Errorf("encodeSubjectIfNeeded(%q) = %q, 変換されないべき", tt.in, got)
				}
				return
			}
			if got == tt.in {
				t.Errorf("encodeSubjectIfNeeded(%q) が変換されていません", tt.in)
			}
			if !strings.HasPrefix(strings.ToLower(got), strings.ToLower(tt.wantPrefix)) {
				t.Errorf("encodeSubjectIfNeeded(%q) = %q, %q で始まるべき", tt.in, got, tt.wantPrefix)
			}
		})
	}
}

func TestRewriteSubjectInEML_NonASCIIEncoded(t *testing.T) {
	eml := []byte("From: a@example.com\r\nSubject: original\r\nTo: b@example.com\r\n\r\nBody\r\n")
	newSubject := "[迷惑メール注意] original"

	rewritten := rewriteSubjectInEML(eml, encodeSubjectIfNeeded(newSubject))

	// ヘッダー部に生の UTF-8 が書き込まれていないこと
	headerEnd := strings.Index(string(rewritten), "\r\n\r\n")
	if headerEnd == -1 {
		t.Fatal("ヘッダー/ボディ区切りが見つかりません")
	}
	header := string(rewritten[:headerEnd])
	for _, b := range []byte(header) {
		if b >= 0x80 {
			t.Fatalf("ヘッダーに非 ASCII バイトが含まれています: %q", header)
		}
	}
	if !strings.Contains(header, "=?UTF-8?") {
		t.Errorf("Subject が RFC 2047 エンコードされていません: %q", header)
	}
}
