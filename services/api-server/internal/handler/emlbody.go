package handler

import (
	"bytes"

	"github.com/jhillyerd/enmime"
)

// emlBody は EML から抽出した本文（Web UI 表示用）。
type emlBody struct {
	Text string
	HTML string
}

// extractEMLBody は EML バイト列をパースし、テキスト本文と HTML 本文を返す。
// パースに失敗した場合は raw を丸ごとテキスト本文として返す（表示できないより良い）。
// HTML はそのまま返す（呼び出し側でサンドボックス iframe に入れて描画すること）。
func extractEMLBody(raw []byte) emlBody {
	env, err := enmime.ReadEnvelope(bytes.NewReader(raw))
	if err != nil {
		return emlBody{Text: string(raw)}
	}
	return emlBody{Text: env.Text, HTML: env.HTML}
}
