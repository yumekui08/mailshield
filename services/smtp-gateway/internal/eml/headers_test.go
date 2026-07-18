package eml

import (
	"strings"
	"testing"
)

const crlfEML = "From: a@example.com\r\n" +
	"To: b@example.com\r\n" +
	"Subject: Hello World\r\n" +
	"\r\n" +
	"body line 1\r\nbody line 2\r\n"

func TestAddHeaderTop_CRLF(t *testing.T) {
	out := string(AddHeaderTop([]byte(crlfEML), "X-MailShield-Origin", "external"))
	if !strings.HasPrefix(out, "X-MailShield-Origin: external\r\n") {
		t.Errorf("先頭に新ヘッダーが無い:\n%q", out[:60])
	}
	if !strings.Contains(out, "\r\n\r\nbody line 1") {
		t.Error("ヘッダー/ボディ区切りが壊れた")
	}
	if !strings.Contains(out, "Subject: Hello World") {
		t.Error("既存ヘッダーが失われた")
	}
}

func TestPrependSubjectPrefix_CRLF(t *testing.T) {
	out := string(PrependSubjectPrefix([]byte(crlfEML), "[EXTERNAL] "))
	if !strings.Contains(out, "Subject: [EXTERNAL] Hello World\r\n") {
		t.Errorf("件名プレフィックスが正しくない:\n%s", out)
	}
	if !strings.Contains(out, "\r\n\r\nbody line 1\r\nbody line 2") {
		t.Error("ボディが変わってしまった")
	}
}

func TestPrependSubjectPrefix_NoSubject(t *testing.T) {
	eml := "From: a@example.com\r\n\r\nbody\r\n"
	out := string(PrependSubjectPrefix([]byte(eml), "[EXTERNAL] "))
	if !strings.Contains(out, "Subject: [EXTERNAL] \r\n") {
		t.Errorf("Subject が追加されていない:\n%s", out)
	}
}

func TestPrependSubjectPrefix_LF(t *testing.T) {
	eml := "From: a@example.com\nSubject: Test\n\nbody\n"
	out := string(PrependSubjectPrefix([]byte(eml), "[EXTERNAL] "))
	if !strings.Contains(out, "Subject: [EXTERNAL] Test\n") {
		t.Errorf("LF 終端で件名プレフィックスが正しくない:\n%s", out)
	}
	if strings.Contains(out, "\r\n") {
		t.Error("LF EML に CRLF が混入した")
	}
}

func TestRemoveHeader_WithFolding(t *testing.T) {
	eml := "From: a@example.com\r\n" +
		"X-Long: value part 1\r\n continuation\r\n" +
		"Subject: keep me\r\n" +
		"\r\nbody\r\n"
	out := string(RemoveHeader([]byte(eml), "X-Long"))
	if strings.Contains(out, "X-Long") || strings.Contains(out, "continuation") {
		t.Errorf("折り畳みヘッダーが完全に削除されていない:\n%s", out)
	}
	if !strings.Contains(out, "Subject: keep me") || !strings.Contains(out, "From: a@example.com") {
		t.Error("残すべきヘッダーが消えた")
	}
}

func TestPrependSubjectPrefix_EncodedSubjectPreserved(t *testing.T) {
	eml := "Subject: =?UTF-8?B?44GT44KT44Gr44Gh44Gv?=\r\n\r\nbody\r\n"
	out := string(PrependSubjectPrefix([]byte(eml), "[EXTERNAL] "))
	if !strings.Contains(out, "Subject: [EXTERNAL] =?UTF-8?B?44GT44KT44Gr44Gh44Gv?=") {
		t.Errorf("エンコード済み件名の前に prefix が付いていない:\n%s", out)
	}
}
