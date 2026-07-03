// Package eml は EML（RFC 5322 メッセージ）の再構築ユーティリティを提供する。
// 変換ワーカー（sanitize / url-rewrite / disclaimer / filesep）が本文を差し替えた
// 新しい EML を組み立てる際の共通処理を集約する。
package eml

import (
	"bytes"
	"fmt"
	"net/textproto"
	"time"

	"github.com/jhillyerd/enmime"
)

// builderManagedHeaders は enmime.Builder が自身で管理するヘッダー。
// 元メッセージからコピーすると二重定義や MIME 構造の破壊
// （例: multipart ルートへの Content-Transfer-Encoding: quoted-printable 付与）
// を起こすため、コピー対象から除外する。
var builderManagedHeaders = map[string]bool{
	"From":                      true,
	"To":                        true,
	"Subject":                   true,
	"Date":                      true,
	"Mime-Version":              true,
	"Content-Type":              true,
	"Content-Transfer-Encoding": true,
	"Content-Disposition":       true,
}

// RebuildInput は Rebuild に渡す再構築パラメータ。
type RebuildInput struct {
	From    string
	To      []string
	Subject string
	Date    time.Time
	// Text / HTML は差し替え後の本文。空文字列のパートは生成しない。
	Text string
	HTML string
	// SkipPart が true を返す添付・インラインパートは新しい EML に含めない。
	// nil の場合はすべて保持する。
	SkipPart func(*enmime.Part) bool
}

// Rebuild は env の本文を差し替えた新しい EML バイト列を構築する。
//
// enmime.Builder が管理する封筒・MIME 構造ヘッダー以外の元ヘッダー
// （Received トレース・Authentication-Results・DKIM-Signature・X-* 等）は
// すべて新しい EML に引き継がれる。これにより監査・認証情報が変換で失われない。
func Rebuild(env *enmime.Envelope, in RebuildInput) ([]byte, error) {
	b := enmime.Builder().
		From("", in.From).
		Subject(in.Subject).
		Date(in.Date)
	for _, to := range in.To {
		b = b.To("", to)
	}
	if in.Text != "" {
		b = b.Text([]byte(in.Text))
	}
	if in.HTML != "" {
		b = b.HTML([]byte(in.HTML))
	}

	skip := in.SkipPart
	if skip == nil {
		skip = func(*enmime.Part) bool { return false }
	}
	for _, att := range env.Attachments {
		if !skip(att) {
			b = b.AddAttachment(att.Content, att.ContentType, att.FileName)
		}
	}
	for _, inline := range env.Inlines {
		if !skip(inline) {
			b = b.AddInline(inline.Content, inline.ContentType, inline.FileName, inline.ContentID)
		}
	}

	root, err := b.Build()
	if err != nil {
		return nil, fmt.Errorf("EML 再構築失敗: %w", err)
	}

	for _, key := range env.GetHeaderKeys() {
		if builderManagedHeaders[textproto.CanonicalMIMEHeaderKey(key)] {
			continue
		}
		root.Header.Del(key)
		for _, val := range env.GetHeaderValues(key) {
			root.Header.Add(key, val)
		}
	}

	var buf bytes.Buffer
	if err := root.Encode(&buf); err != nil {
		return nil, fmt.Errorf("EML エンコード失敗: %w", err)
	}
	return buf.Bytes(), nil
}
