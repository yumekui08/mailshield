// Package officefile は添付ファイルのバイナリ形式判定ユーティリティを提供する。
// attachment-inspector（検査）と macro-strip（無害化）で共用する。
//
// 判定はすべてバイト列に対して行い、ファイルの展開・実行は行わない。
package officefile

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"unicode/utf16"
)

// ─── magic bytes 判定 ─────────────────────────────────────────────────────────

// IsExecutable はバイト列が実行ファイル形式（PE / ELF / Mach-O / スクリプト）かを返す。
func IsExecutable(content []byte) bool {
	if len(content) < 4 {
		return false
	}
	// PE (Windows): "MZ"
	if content[0] == 'M' && content[1] == 'Z' {
		return true
	}
	// ELF (Linux): 0x7F "ELF"
	if bytes.HasPrefix(content, []byte{0x7F, 'E', 'L', 'F'}) {
		return true
	}
	// Mach-O (macOS): FEEDFACE / FEEDFACF / CAFEBABE（universal）とリトルエンディアン形
	magic := binary.BigEndian.Uint32(content[:4])
	switch magic {
	case 0xFEEDFACE, 0xFEEDFACF, 0xCAFEBABE, 0xCEFAEDFE, 0xCFFAEDFE:
		return true
	}
	return false
}

// IsZip はバイト列が ZIP アーカイブ（OOXML 含む）かを返す。
func IsZip(content []byte) bool {
	return bytes.HasPrefix(content, []byte("PK\x03\x04"))
}

// IsOLE はバイト列が OLE 複合ファイル（旧 Office 形式 .doc/.xls/.ppt、
// および暗号化 OOXML のコンテナ）かを返す。
func IsOLE(content []byte) bool {
	return bytes.HasPrefix(content, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1})
}

// ─── マクロ判定 ───────────────────────────────────────────────────────────────

// vbaEntrySuffixes は OOXML ZIP 内で VBA マクロを構成するエントリのサフィックス。
var vbaEntrySuffixes = []string{"vbaproject.bin", "vbadata.xml", "vbaprojectsignature.bin"}

// isVBAEntry は ZIP エントリ名が VBA マクロ関連パートかを返す（大文字小文字を無視）。
func isVBAEntry(name string) bool {
	lower := strings.ToLower(name)
	for _, suffix := range vbaEntrySuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

// OOXMLHasMacro は ZIP（OOXML）内に VBA マクロパートが含まれるかを返す。
// ZIP として読めない場合は false。
func OOXMLHasMacro(content []byte) bool {
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return false
	}
	for _, f := range zr.File {
		if isVBAEntry(f.Name) {
			return true
		}
	}
	return false
}

// OLEHasMacro は OLE 複合ファイルに VBA マクロが含まれるかをヒューリスティックに判定する。
// OLE のストリーム名は UTF-16LE で格納されるため、"VBA" / "Macros" ストリーム名の
// UTF-16LE 表現を検索する。完全な OLE パーサーではないため誤検知・見逃しがあり得る
// （検知漏れが許容できない場合はポリシーで旧 Office 形式自体を制御すること）。
func OLEHasMacro(content []byte) bool {
	if !IsOLE(content) {
		return false
	}
	for _, name := range []string{"VBA", "Macros", "_VBA_PROJECT"} {
		if bytes.Contains(content, utf16le(name)) {
			return true
		}
	}
	return false
}

// ─── 暗号化判定 ───────────────────────────────────────────────────────────────

// ZipHasEncryptedEntry は ZIP 内に暗号化されたエントリ（パスワード付き ZIP）が
// あるかを返す。ZIP として読めない場合は false。
func ZipHasEncryptedEntry(content []byte) bool {
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return false
	}
	for _, f := range zr.File {
		// 汎用フラグの bit 0 が暗号化フラグ（APPNOTE 4.4.4）
		if f.Flags&0x1 != 0 {
			return true
		}
	}
	return false
}

// OLEIsEncryptedOffice は OLE 複合ファイルが暗号化 Office 文書
// （パスワード付き OOXML は "EncryptedPackage" ストリームを持つ OLE コンテナになる）
// かをヒューリスティックに判定する。
func OLEIsEncryptedOffice(content []byte) bool {
	if !IsOLE(content) {
		return false
	}
	return bytes.Contains(content, utf16le("EncryptedPackage")) ||
		bytes.Contains(content, utf16le("EncryptionInfo"))
}

// ─── ZIP エントリ列挙 ─────────────────────────────────────────────────────────

// ZipEntryNames は ZIP 内のエントリ名一覧を返す（アーカイブ内の危険拡張子検査用）。
// ZIP として読めない場合は nil。
func ZipEntryNames(content []byte) []string {
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	return names
}

// ─── VBA 除去（macro-strip 用） ───────────────────────────────────────────────

// StripOOXMLMacro は OOXML（ZIP）から VBA マクロパートを除去した新しいバイト列を返す。
//   - vbaProject.bin / vbaData.xml / vbaProjectSignature.bin エントリを削除
//   - [Content_Types].xml から該当パートの Override 宣言を削除
//   - *.rels から該当パートへの Relationship を削除
//
// マクロが存在しない場合は (nil, false) を返す。
func StripOOXMLMacro(content []byte) ([]byte, bool) {
	zr, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, false
	}

	hasMacro := false
	for _, f := range zr.File {
		if isVBAEntry(f.Name) {
			hasMacro = true
			break
		}
	}
	if !hasMacro {
		return nil, false
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		if isVBAEntry(f.Name) {
			continue // マクロパートを除去
		}
		rc, err := f.Open()
		if err != nil {
			return nil, false
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, false
		}

		lower := strings.ToLower(f.Name)
		if lower == "[content_types].xml" {
			data = removeXMLElements(data, "Override", "vbaProject.bin", "vbaData.xml")
		} else if strings.HasSuffix(lower, ".rels") {
			data = removeXMLElements(data, "Relationship", "vbaProject.bin", "vbaData.xml")
		}

		w, err := zw.CreateHeader(&zip.FileHeader{Name: f.Name, Method: zip.Deflate})
		if err != nil {
			return nil, false
		}
		if _, err := w.Write(data); err != nil {
			return nil, false
		}
	}
	if err := zw.Close(); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

// removeXMLElements は自己完結タグ `<elem ... />` のうち、属性値に needle の
// いずれかを含むものを取り除く。OOXML の [Content_Types].xml / .rels は
// 要素構造が固定的なため、この限定的な文字列処理で安全に除去できる。
func removeXMLElements(data []byte, elem string, needles ...string) []byte {
	s := string(data)
	for {
		start := indexElementWithNeedle(s, elem, needles)
		if start < 0 {
			break
		}
		end := strings.Index(s[start:], "/>")
		if end < 0 {
			break
		}
		s = s[:start] + s[start+end+2:]
	}
	return []byte(s)
}

// indexElementWithNeedle は `<elem` で始まり needle を含む自己完結タグの開始位置を返す。
func indexElementWithNeedle(s, elem string, needles []string) int {
	offset := 0
	for {
		i := strings.Index(s[offset:], "<"+elem)
		if i < 0 {
			return -1
		}
		start := offset + i
		end := strings.Index(s[start:], "/>")
		if end < 0 {
			return -1
		}
		tag := s[start : start+end+2]
		for _, n := range needles {
			if strings.Contains(strings.ToLower(tag), strings.ToLower(n)) {
				return start
			}
		}
		offset = start + end + 2
	}
}

// utf16le は文字列の UTF-16LE バイト表現を返す（OLE ストリーム名検索用）。
func utf16le(s string) []byte {
	codes := utf16.Encode([]rune(s))
	b := make([]byte, len(codes)*2)
	for i, c := range codes {
		binary.LittleEndian.PutUint16(b[i*2:], c)
	}
	return b
}
