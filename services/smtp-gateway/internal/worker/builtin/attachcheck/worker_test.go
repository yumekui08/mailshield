package attachcheck

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

// buildEMLWithAttachment は単一添付の EML を組み立てる。
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
Subject: test
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="BOUND"

--BOUND
Content-Type: text/plain

body
--BOUND
Content-Type: application/octet-stream; name="%s"
Content-Transfer-Encoding: base64
Content-Disposition: attachment; filename="%s"

%s
--BOUND--
`, filename, filename, wrapped.String()))
}

func buildZipBytes(t *testing.T, entries map[string]string, encrypt bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, _ := zw.Create(name)
		w.Write([]byte(content))
	}
	zw.Close()
	data := buf.Bytes()
	if encrypt {
		for i := 0; i+8 < len(data); i++ {
			if data[i] == 'P' && data[i+1] == 'K' && data[i+2] == 0x03 && data[i+3] == 0x04 {
				data[i+6] |= 0x01
			}
			if data[i] == 'P' && data[i+1] == 'K' && data[i+2] == 0x01 && data[i+3] == 0x02 {
				data[i+8] |= 0x01
			}
		}
	}
	return data
}

func inspect(t *testing.T, filename string, content []byte) *domain.InspectResult {
	t.Helper()
	w, err := New(t.TempDir()) // 設定なし → デフォルト
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.Inspect(context.Background(), &domain.Mail{
		RawEML: buildEMLWithAttachment(filename, content),
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func hasReason(res *domain.InspectResult, prefix string) bool {
	reasons, _ := res.Details["reasons"].([]string)
	for _, r := range reasons {
		if strings.HasPrefix(r, prefix) {
			return true
		}
	}
	return false
}

func TestExecutableDisguisedAsPDF(t *testing.T) {
	// 拡張子は .pdf だが中身は PE 実行ファイル
	res := inspect(t, "invoice.pdf", []byte("MZ\x90\x00this is actually an exe"))
	if !res.Detected {
		t.Errorf("実行ファイル偽装を検知すべき (score=%d)", res.Score)
	}
	if !hasReason(res, "executable") {
		t.Error("executable の理由がない")
	}
	if !hasReason(res, "extension_mismatch") {
		t.Error("拡張子不一致の理由がない")
	}
}

func TestMultipleExtension(t *testing.T) {
	res := inspect(t, "report.pdf.exe", []byte("harmless bytes"))
	if !res.Detected {
		t.Errorf("多重拡張子を検知すべき (score=%d)", res.Score)
	}
	if !hasReason(res, "multiple_extension") {
		t.Error("multiple_extension の理由がない")
	}
	if !hasReason(res, "banned_extension") {
		t.Error("banned_extension の理由がない (.exe)")
	}
}

func TestBannedExtension(t *testing.T) {
	res := inspect(t, "setup.exe", []byte("plain"))
	if !hasReason(res, "banned_extension") {
		t.Errorf("禁止拡張子を検知すべき (score=%d)", res.Score)
	}
}

func TestEncryptedZip(t *testing.T) {
	zipBytes := buildZipBytes(t, map[string]string{"secret.docx": "data"}, true)
	res := inspect(t, "documents.zip", zipBytes)
	if !hasReason(res, "encrypted_zip") {
		t.Errorf("暗号化 ZIP を検知すべき (score=%d, reasons=%v)", res.Score, res.Details["reasons"])
	}
}

func TestOOXMLMacro(t *testing.T) {
	ooxml := buildZipBytes(t, map[string]string{
		"[Content_Types].xml": "<Types/>",
		"word/document.xml":   "<doc/>",
		"word/vbaProject.bin": "macro",
	}, false)
	res := inspect(t, "quarterly.docm", ooxml)
	if !hasReason(res, "ooxml_macro") {
		t.Errorf("OOXML マクロを検知すべき (score=%d, reasons=%v)", res.Score, res.Details["reasons"])
	}
}

func TestBannedInArchive(t *testing.T) {
	zipBytes := buildZipBytes(t, map[string]string{
		"readme.txt":  "hi",
		"payload.exe": "MZ",
	}, false)
	res := inspect(t, "bundle.zip", zipBytes)
	if !hasReason(res, "banned_in_archive") {
		t.Errorf("アーカイブ内実行ファイルを検知すべき (score=%d, reasons=%v)", res.Score, res.Details["reasons"])
	}
}

func TestCleanAttachment(t *testing.T) {
	res := inspect(t, "report.pdf", []byte("%PDF-1.7\nclean content"))
	if res.Detected {
		t.Errorf("正常な PDF を誤検知 (score=%d, reasons=%v)", res.Score, res.Details["reasons"])
	}
	if res.Score != 0 {
		t.Errorf("正常な添付のスコアは 0 であるべき: %d", res.Score)
	}
}

func TestNoAttachment(t *testing.T) {
	w, _ := New(t.TempDir())
	res, err := w.Inspect(context.Background(), &domain.Mail{
		RawEML: []byte("From: a@b.test\r\nTo: c@d.test\r\nSubject: no attach\r\n\r\nbody\r\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Detected || res.Score != 0 {
		t.Errorf("添付なしは検知されないべき (score=%d)", res.Score)
	}
}
