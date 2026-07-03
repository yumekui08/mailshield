// Package lua は Lua スクリプトで実装されたワーカーをロードし、
// domain.InspectWorker / domain.TransformWorker インターフェースとして提供する。
package lua

import (
	"bytes"
	"fmt"
	"mime"
	"strings"

	glua "github.com/yuin/gopher-lua"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

// mailToTable は domain.Mail を Lua テーブルに変換する。
func mailToTable(L *glua.LState, m *domain.Mail) *glua.LTable {
	t := L.NewTable()
	L.SetField(t, "message_id", glua.LString(m.MessageID))
	L.SetField(t, "subject", glua.LString(m.Subject))
	L.SetField(t, "from", glua.LString(m.FromAddress))
	L.SetField(t, "size_bytes", glua.LNumber(m.SizeBytes))
	L.SetField(t, "has_attachment", glua.LBool(m.HasAttachment))
	L.SetField(t, "rspamd_score", glua.LNumber(m.RspamdScore))

	toList := L.NewTable()
	for i, addr := range m.ToAddresses {
		toList.RawSetInt(i+1, glua.LString(addr))
	}
	L.SetField(t, "to", toList)

	auth := L.NewTable()
	L.SetField(auth, "spf", glua.LString(m.AuthResults.SPF))
	L.SetField(auth, "dkim", glua.LString(m.AuthResults.DKIM))
	L.SetField(auth, "dmarc", glua.LString(m.AuthResults.DMARC))
	L.SetField(t, "auth_results", auth)

	return t
}

// applyTransformResult は Lua の transform() が返したテーブルの変更を domain.Mail に適用する。
// 現在は subject フィールドのみ対応。subject が変わった場合は RawEML も書き換える。
func applyTransformResult(original *domain.Mail, result *glua.LTable) *domain.Mail {
	modified := *original

	if s, ok := result.RawGetString("subject").(glua.LString); ok {
		newSubject := string(s)
		if newSubject != original.Subject {
			modified.Subject = newSubject
			modified.RawEML = rewriteSubjectInEML(original.RawEML, encodeSubjectIfNeeded(newSubject))
		}
	}

	return &modified
}

// encodeSubjectIfNeeded は非 ASCII 文字を含む件名を RFC 2047（B エンコーディング・UTF-8）
// でエンコードする。ASCII のみの件名はそのまま返す。
// ヘッダーに生の UTF-8 を書き込むと SMTPUTF8 非対応の MTA で問題を起こすため必要。
func encodeSubjectIfNeeded(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return mime.BEncoding.Encode("UTF-8", s)
		}
	}
	return s
}

// rewriteSubjectInEML は EML バイト列の Subject ヘッダーを新しい値に置き換える。
// CRLF/LF の改行形式を保持し、RFC 2822 の折り畳みヘッダー継続行も正しく処理する。
func rewriteSubjectInEML(eml []byte, newSubject string) []byte {
	lines := bytes.Split(eml, []byte("\n"))
	var result [][]byte
	inHeader := true
	replacingSubject := false

	for _, line := range lines {
		if inHeader {
			if len(bytes.TrimSpace(line)) == 0 {
				inHeader = false
				replacingSubject = false
				result = append(result, line)
				continue
			}
			// 折り畳みヘッダーの継続行（先頭が空白またはタブ）
			if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
				if replacingSubject {
					// 置換済み Subject の継続行はスキップする
					continue
				}
				result = append(result, line)
				continue
			}
			// 新しいヘッダー行が始まったのでフラグをリセット
			replacingSubject = false
			if strings.HasPrefix(strings.ToLower(string(line)), "subject:") {
				// 元の行末に \r があれば置換行にも付加して改行形式を統一する
				cr := []byte{}
				if len(line) > 0 && line[len(line)-1] == '\r' {
					cr = []byte{'\r'}
				}
				result = append(result, append([]byte("Subject: "+newSubject), cr...))
				replacingSubject = true
				continue
			}
		}
		result = append(result, line)
	}
	return bytes.Join(result, []byte("\n"))
}

// luaToAny は Lua 値を Go の any に変換する。
func luaToAny(v glua.LValue) any {
	switch v := v.(type) {
	case glua.LString:
		return string(v)
	case glua.LNumber:
		return float64(v)
	case glua.LBool:
		return bool(v)
	default:
		return v.String()
	}
}

// configToTable は YAML からデシリアライズされた map[string]any を Lua テーブルに変換する。
// ネストしたマップやスライスも再帰的に変換する。
func configToTable(L *glua.LState, cfg map[string]any) *glua.LTable {
	t := L.NewTable()
	for k, v := range cfg {
		L.SetField(t, k, anyToLuaValue(L, v))
	}
	return t
}

// anyToLuaValue は Go の any（yaml.v3 がデシリアライズした値）を Lua 値に変換する。
func anyToLuaValue(L *glua.LState, v any) glua.LValue {
	if v == nil {
		return glua.LNil
	}
	switch v := v.(type) {
	case bool:
		return glua.LBool(v)
	case int:
		return glua.LNumber(v)
	case int64:
		return glua.LNumber(v)
	case float64:
		return glua.LNumber(v)
	case string:
		return glua.LString(v)
	case []any:
		t := L.NewTable()
		for i, elem := range v {
			t.RawSetInt(i+1, anyToLuaValue(L, elem))
		}
		return t
	case map[string]any:
		return configToTable(L, v)
	default:
		return glua.LString(fmt.Sprintf("%v", v))
	}
}
