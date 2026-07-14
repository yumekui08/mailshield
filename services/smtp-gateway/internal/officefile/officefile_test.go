package officefile

import (
	"archive/zip"
	"bytes"
	"testing"
)

// buildZip はテスト用 ZIP を組み立てる。
func buildZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// buildEncryptedZip は暗号化フラグ付き ZIP を組み立てる
// （stdlib は暗号化 ZIP を書けないため、通常 ZIP の汎用フラグビット 0 を立てる）。
func buildEncryptedZip(t *testing.T) []byte {
	t.Helper()
	data := buildZip(t, map[string]string{"secret.txt": "x"})
	// ローカルファイルヘッダー（PK\x03\x04 の +6）と
	// セントラルディレクトリ（PK\x01\x02 の +8）の汎用フラグに bit0 を立てる
	for i := 0; i+7 < len(data); i++ {
		if data[i] == 'P' && data[i+1] == 'K' && data[i+2] == 0x03 && data[i+3] == 0x04 {
			data[i+6] |= 0x01
		}
		if data[i] == 'P' && data[i+1] == 'K' && data[i+2] == 0x01 && data[i+3] == 0x02 {
			data[i+8] |= 0x01
		}
	}
	return data
}

func TestIsExecutable(t *testing.T) {
	cases := []struct {
		name    string
		content []byte
		want    bool
	}{
		{"PE", []byte("MZ\x90\x00xxxx"), true},
		{"ELF", []byte{0x7F, 'E', 'L', 'F', 2, 1, 1}, true},
		{"Mach-O", []byte{0xFE, 0xED, 0xFA, 0xCE, 0, 0}, true},
		{"PDF", []byte("%PDF-1.7"), false},
		{"テキスト", []byte("hello world"), false},
		{"短すぎ", []byte("MZ"), false},
	}
	for _, c := range cases {
		if got := IsExecutable(c.content); got != c.want {
			t.Errorf("%s: IsExecutable = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestOOXMLHasMacro(t *testing.T) {
	withMacro := buildZip(t, map[string]string{
		"[Content_Types].xml": "<Types/>",
		"word/document.xml":   "<doc/>",
		"word/vbaProject.bin": "macro-bytes",
	})
	withoutMacro := buildZip(t, map[string]string{
		"[Content_Types].xml": "<Types/>",
		"word/document.xml":   "<doc/>",
	})
	if !OOXMLHasMacro(withMacro) {
		t.Error("vbaProject.bin 入りはマクロありと判定すべき")
	}
	if OOXMLHasMacro(withoutMacro) {
		t.Error("マクロなし OOXML を誤検知")
	}
	if OOXMLHasMacro([]byte("not a zip")) {
		t.Error("非 ZIP を誤検知")
	}
}

func TestOLEHasMacro(t *testing.T) {
	ole := append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, make([]byte, 64)...)
	oleWithVBA := append(append([]byte{}, ole...), utf16le("_VBA_PROJECT")...)

	if OLEHasMacro(ole) {
		t.Error("VBA ストリームなし OLE を誤検知")
	}
	if !OLEHasMacro(oleWithVBA) {
		t.Error("_VBA_PROJECT 入り OLE はマクロありと判定すべき")
	}
	if OLEHasMacro([]byte("plain text")) {
		t.Error("非 OLE を誤検知")
	}
}

func TestZipHasEncryptedEntry(t *testing.T) {
	if ZipHasEncryptedEntry(buildZip(t, map[string]string{"a.txt": "x"})) {
		t.Error("平文 ZIP を誤検知")
	}
	if !ZipHasEncryptedEntry(buildEncryptedZip(t)) {
		t.Error("暗号化フラグ付き ZIP を検知できない")
	}
}

func TestOLEIsEncryptedOffice(t *testing.T) {
	ole := append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, make([]byte, 32)...)
	encrypted := append(append([]byte{}, ole...), utf16le("EncryptedPackage")...)
	if OLEIsEncryptedOffice(ole) {
		t.Error("通常 OLE を誤検知")
	}
	if !OLEIsEncryptedOffice(encrypted) {
		t.Error("EncryptedPackage 入り OLE を検知できない")
	}
}

func TestZipEntryNames(t *testing.T) {
	names := ZipEntryNames(buildZip(t, map[string]string{"a.txt": "x", "dir/b.exe": "y"}))
	if len(names) != 2 {
		t.Fatalf("エントリ数 = %d, want 2", len(names))
	}
	if ZipEntryNames([]byte("junk")) != nil {
		t.Error("非 ZIP は nil を返すべき")
	}
}

func TestStripOOXMLMacro(t *testing.T) {
	src := buildZip(t, map[string]string{
		"[Content_Types].xml":          `<Types><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/vbaProject.bin" ContentType="application/vnd.ms-office.vbaProject"/><Override PartName="/word/document.xml" ContentType="application/vnd.ms-word.document.macroEnabled.main+xml"/></Types>`,
		"word/document.xml":            "<doc/>",
		"word/vbaProject.bin":          "macro-bytes",
		"word/_rels/document.xml.rels": `<Relationships><Relationship Id="rId1" Type="http://schemas.microsoft.com/office/2006/relationships/vbaProject" Target="vbaProject.bin"/><Relationship Id="rId2" Type="http://x/styles" Target="styles.xml"/></Relationships>`,
	})

	stripped, ok := StripOOXMLMacro(src)
	if !ok {
		t.Fatal("マクロ入り OOXML の除去が実行されなかった")
	}

	zr, err := zip.NewReader(bytes.NewReader(stripped), int64(len(stripped)))
	if err != nil {
		t.Fatalf("除去後の ZIP が読めない: %v", err)
	}
	var contentTypes, rels string
	for _, f := range zr.File {
		if f.Name == "word/vbaProject.bin" {
			t.Error("vbaProject.bin が残っている")
		}
		rc, _ := f.Open()
		data := make([]byte, f.UncompressedSize64)
		rc.Read(data)
		rc.Close()
		switch f.Name {
		case "[Content_Types].xml":
			contentTypes = string(data)
		case "word/_rels/document.xml.rels":
			rels = string(data)
		}
	}
	if bytes.Contains([]byte(contentTypes), []byte("vbaProject")) {
		t.Errorf("[Content_Types].xml に vbaProject の Override が残っている: %s", contentTypes)
	}
	if !bytes.Contains([]byte(contentTypes), []byte("document.xml")) {
		t.Errorf("無関係の Override が消えている: %s", contentTypes)
	}
	if bytes.Contains([]byte(rels), []byte("vbaProject")) {
		t.Errorf(".rels に vbaProject の Relationship が残っている: %s", rels)
	}
	if !bytes.Contains([]byte(rels), []byte("styles.xml")) {
		t.Errorf("無関係の Relationship が消えている: %s", rels)
	}

	// マクロなしは (nil, false)
	if _, ok := StripOOXMLMacro(buildZip(t, map[string]string{"a.xml": "<x/>"})); ok {
		t.Error("マクロなし OOXML で除去が実行された")
	}
}
