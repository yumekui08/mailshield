package eml

import (
	"bytes"
	"fmt"
	"strings"
)

// ヘッダーの外科的編集ユーティリティ。
// enmime による完全な再構築（Rebuild）を避け、ヘッダーブロックだけをバイト列上で
// 直接編集する。ポリシーの非終端アクション（add_header / add_subject_prefix /
// remove_header）で使う。ボディは一切変更しない。

// splitHeaderAndBody は EML をヘッダー部（末尾の空行を含まない）とボディ部に分割し、
// 使用されている行終端（"\r\n" または "\n"）を返す。
// SMTP 経由の EML は CRLF、/simulate には LF のみの EML も投入されるため両対応。
func splitHeaderAndBody(raw []byte) (header, body []byte, term string, ok bool) {
	if i := bytes.Index(raw, []byte("\r\n\r\n")); i >= 0 {
		return raw[:i], raw[i+4:], "\r\n", true
	}
	if i := bytes.Index(raw, []byte("\n\n")); i >= 0 {
		return raw[:i], raw[i+2:], "\n", true
	}
	// ボディなし（ヘッダーのみ）。終端は含有物から推定する。
	term = "\n"
	if bytes.Contains(raw, []byte("\r\n")) {
		term = "\r\n"
	}
	return raw, nil, term, false
}

// reassemble はヘッダー部・ボディ部・終端から EML を再構成する。
func reassemble(header, body []byte, term string, hadBody bool) []byte {
	var buf bytes.Buffer
	buf.Write(header)
	buf.WriteString(term) // ヘッダー最終行の終端
	buf.WriteString(term) // ヘッダー/ボディ区切りの空行
	if hadBody {
		buf.Write(body)
	}
	return buf.Bytes()
}

// AddHeaderTop はヘッダーブロックの先頭に "Name: Value" を1行挿入する。
// 折り畳みは行わない（呼び出し側で値を1行に収めること）。
func AddHeaderTop(raw []byte, name, value string) []byte {
	header, body, term, hadBody := splitHeaderAndBody(raw)
	newLine := []byte(fmt.Sprintf("%s: %s%s", name, value, term))
	newHeader := append(newLine, header...)
	return reassemble(newHeader, body, term, hadBody)
}

// RemoveHeader は指定名のヘッダー（折り畳み継続行を含む）をすべて削除する（大文字小文字無視）。
func RemoveHeader(raw []byte, name string) []byte {
	header, body, term, hadBody := splitHeaderAndBody(raw)
	lines := splitLines(header)
	prefix := strings.ToLower(name) + ":"

	var kept []string
	skipping := false
	for _, line := range lines {
		if skipping {
			// 折り畳み継続行（先頭が空白）はスキップ対象の一部
			if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
				continue
			}
			skipping = false
		}
		if strings.HasPrefix(strings.ToLower(line), prefix) {
			skipping = true
			continue
		}
		kept = append(kept, line)
	}
	newHeader := []byte(strings.Join(kept, term))
	return reassemble(newHeader, body, term, hadBody)
}

// PrependSubjectPrefix は Subject ヘッダー値の先頭に prefix を付加する。
// Subject ヘッダーが無い場合は prefix を値とする Subject 行を追加する。
// prefix は表示可能な文字列（通常 ASCII の "[EXTERNAL] " 等）を想定し、
// 既存値が RFC 2047 エンコード済みでもその前に連結して問題なく表示される。
func PrependSubjectPrefix(raw []byte, prefix string) []byte {
	header, body, term, hadBody := splitHeaderAndBody(raw)
	lines := splitLines(header)

	found := false
	for i, line := range lines {
		if strings.HasPrefix(strings.ToLower(line), "subject:") {
			colon := strings.IndexByte(line, ':')
			existing := line[colon+1:]
			// "Subject:" の直後の空白1つは保持しつつ prefix を差し込む
			trimmedLeading := strings.TrimLeft(existing, " \t")
			lines[i] = line[:colon+1] + " " + prefix + trimmedLeading
			found = true
			break
		}
	}
	if !found {
		lines = append([]string{"Subject: " + prefix}, lines...)
	}
	newHeader := []byte(strings.Join(lines, term))
	return reassemble(newHeader, body, term, hadBody)
}

// splitLines は CRLF / LF いずれの終端でも行に分割する（終端文字は含まない）。
func splitLines(b []byte) []string {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	return strings.Split(s, "\n")
}
