package eml_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/jhillyerd/enmime"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/eml"
)

const sampleEML = "Received: from mx.example.com by gw.example.com; Mon, 1 Jan 2026 00:00:00 +0000\r\n" +
	"Authentication-Results: gw.example.com; spf=pass; dkim=pass; dmarc=pass\r\n" +
	"X-Custom-Header: keep-me\r\n" +
	"From: sender@example.com\r\n" +
	"To: rcpt@example.com\r\n" +
	"Subject: test\r\n" +
	"Date: Mon, 1 Jan 2026 00:00:00 +0000\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: text/html; charset=UTF-8\r\n" +
	"Content-Transfer-Encoding: quoted-printable\r\n" +
	"\r\n" +
	"<p>hello</p>\r\n"

func TestRebuild_PreservesAuditHeaders(t *testing.T) {
	env, err := enmime.ReadEnvelope(strings.NewReader(sampleEML))
	if err != nil {
		t.Fatal(err)
	}

	out, err := eml.Rebuild(env, eml.RebuildInput{
		From:    "sender@example.com",
		To:      []string{"rcpt@example.com"},
		Subject: "test",
		Date:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		HTML:    "<p>sanitized</p>",
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, h := range []string{"Received:", "Authentication-Results:", "X-Custom-Header:"} {
		if !bytes.Contains(out, []byte(h)) {
			t.Errorf("再構築後の EML に %s ヘッダーがありません", h)
		}
	}
}

func TestRebuild_DoesNotCopyOriginalCTEToRoot(t *testing.T) {
	env, err := enmime.ReadEnvelope(strings.NewReader(sampleEML))
	if err != nil {
		t.Fatal(err)
	}

	out, err := eml.Rebuild(env, eml.RebuildInput{
		From:    "sender@example.com",
		To:      []string{"rcpt@example.com"},
		Subject: "test",
		Date:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Text:    "plain",
		HTML:    "<p>sanitized</p>",
	})
	if err != nil {
		t.Fatal(err)
	}

	// ルートは multipart になるため、元の quoted-printable CTE を
	// ルートヘッダーへコピーしてはならない（RFC 2045 違反になる）。
	headerEnd := bytes.Index(out, []byte("\r\n\r\n"))
	if headerEnd == -1 {
		t.Fatal("ヘッダー/ボディ区切りが見つかりません")
	}
	rootHeader := strings.ToLower(string(out[:headerEnd]))
	if strings.Contains(rootHeader, "content-transfer-encoding: quoted-printable") {
		t.Errorf("multipart ルートに元の Content-Transfer-Encoding がコピーされています:\n%s", rootHeader)
	}

	// 再構築後の EML が正しくパースでき、本文が差し替わっていること
	env2, err := enmime.ReadEnvelope(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("再構築後の EML がパースできません: %v", err)
	}
	if env2.Text != "plain" {
		t.Errorf("Text = %q, want %q", env2.Text, "plain")
	}
	if !strings.Contains(env2.HTML, "sanitized") {
		t.Errorf("HTML = %q, want contains %q", env2.HTML, "sanitized")
	}
}
