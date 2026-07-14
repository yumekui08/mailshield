package macrostrip

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/jhillyerd/enmime"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/officefile"
)

func buildOOXML(t *testing.T, withMacro bool) []byte {
	t.Helper()
	entries := map[string]string{
		"[Content_Types].xml": `<Types><Override PartName="/word/document.xml" ContentType="application/vnd.ms-word.document.macroEnabled.main+xml"/><Override PartName="/word/vbaProject.bin" ContentType="application/vnd.ms-office.vbaProject"/></Types>`,
		"word/document.xml":   "<doc>hello</doc>",
	}
	if withMacro {
		entries["word/vbaProject.bin"] = "MACRO-BYTES"
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, _ := zw.Create(name)
		w.Write([]byte(content))
	}
	zw.Close()
	return buf.Bytes()
}

func buildEMLWithAttachment(filename string, content []byte) []byte {
	b64 := base64.StdEncoding.EncodeToString(content)
	var wrapped strings.Builder
	for i := 0; i < len(b64); i += 76 {
		end := i + 76
		if end > len(b64) {
			end = len(b64)
		}
		wrapped.WriteString(b64[i:end] + "\r\n")
	}
	return []byte(fmt.Sprintf(`From: sender@external.test
To: user@internal.test
Subject: doc
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="B"

--B
Content-Type: text/plain

body text
--B
Content-Type: application/vnd.openxmlformats-officedocument.wordprocessingml.document; name="%s"
Content-Transfer-Encoding: base64
Content-Disposition: attachment; filename="%s"

%s
--B--
`, filename, filename, wrapped.String()))
}

func transform(t *testing.T, eml []byte) *domain.Mail {
	t.Helper()
	w, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	out, err := w.Transform(context.Background(), &domain.Mail{
		RawEML:      eml,
		FromAddress: "sender@external.test",
		ToAddresses: []string{"user@internal.test"},
		Subject:     "doc",
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func attachmentContent(t *testing.T, eml []byte, filename string) []byte {
	t.Helper()
	env, err := enmime.ReadEnvelope(bytes.NewReader(eml))
	if err != nil {
		t.Fatal(err)
	}
	for _, att := range env.Attachments {
		if att.FileName == filename {
			return att.Content
		}
	}
	return nil
}

func TestMacroStripped(t *testing.T) {
	src := buildEMLWithAttachment("report.docm", buildOOXML(t, true))
	out := transform(t, src)

	content := attachmentContent(t, out.RawEML, "report.docm")
	if content == nil {
		t.Fatal("添付が失われた")
	}
	if officefile.OOXMLHasMacro(content) {
		t.Error("マクロが除去されていない")
	}
	// 文書本体は残る
	names := officefile.ZipEntryNames(content)
	found := false
	for _, n := range names {
		if n == "word/document.xml" {
			found = true
		}
	}
	if !found {
		t.Error("文書本体 word/document.xml が失われた")
	}
}

func TestNoMacro_Unchanged(t *testing.T) {
	src := buildEMLWithAttachment("clean.docx", buildOOXML(t, false))
	out := transform(t, src)
	// マクロがなければ EML 再構築せず元をそのまま返す
	if !bytes.Equal(out.RawEML, src) {
		t.Error("マクロなし添付で EML が変更された")
	}
}

func TestNoAttachment_Unchanged(t *testing.T) {
	src := []byte("From: a@b.test\r\nTo: c@d.test\r\nSubject: x\r\n\r\nbody\r\n")
	out := transform(t, src)
	if !bytes.Equal(out.RawEML, src) {
		t.Error("添付なしで EML が変更された")
	}
}
