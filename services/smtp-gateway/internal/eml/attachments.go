package eml

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/jhillyerd/enmime"
)

// StripAttachments は EML から添付ファイルを除去した新しい EML を返す。
// exts が空なら全添付を除去し、指定があればその拡張子（先頭ドットなし）に一致する
// 添付のみ除去する。本文（text/html）とインラインパート・元ヘッダーは保持する。
// 添付が無い（または除去対象なし）場合は (元の EML, false, nil) を返す。
func StripAttachments(rawEML []byte, from string, to []string, subject string, date time.Time, exts []string) ([]byte, bool, error) {
	env, err := enmime.ReadEnvelope(bytes.NewReader(rawEML))
	if err != nil {
		return rawEML, false, fmt.Errorf("EML パース失敗: %w", err)
	}
	if len(env.Attachments) == 0 {
		return rawEML, false, nil
	}
	normExts := make(map[string]bool, len(exts))
	for _, e := range exts {
		e = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(e), "."))
		if e != "" {
			normExts[e] = true
		}
	}
	matched := false
	skip := func(p *enmime.Part) bool {
		if len(normExts) == 0 {
			matched = true
			return true // 全除去
		}
		name := strings.ToLower(p.FileName)
		for ext := range normExts {
			if strings.HasSuffix(name, "."+ext) {
				matched = true
				return true
			}
		}
		return false
	}
	out, err := Rebuild(env, RebuildInput{
		From: from, To: to, Subject: subject, Date: date,
		Text: env.Text, HTML: env.HTML, SkipPart: skip,
	})
	if err != nil {
		return rawEML, false, err
	}
	if !matched {
		return rawEML, false, nil
	}
	return out, true, nil
}
