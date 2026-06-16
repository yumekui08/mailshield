package filesep

import (
	"bytes"
	"fmt"
	"os"
	"text/template"
	"time"
)

// TemplateData はテンプレートに渡すデータを保持する。
type TemplateData struct {
	Subject     string
	ReceivedAt  string
	ExpiryHours int
	DownloadURL string // メッセージ単位の共通ダウンロードリンク
	Attachments []AttachmentInfo
}

// AttachmentInfo は1つの添付ファイルの情報を保持する。
type AttachmentInfo struct {
	Name   string
	SizeKB float64
}

func buildTemplateData(subject string, receivedAt time.Time, expiryHours int, downloadURL string, attachments []AttachmentInfo) TemplateData {
	return TemplateData{
		Subject:     subject,
		ReceivedAt:  receivedAt.Format("2006-01-02 15:04:05 UTC"),
		ExpiryHours: expiryHours,
		DownloadURL: downloadURL,
		Attachments: attachments,
	}
}

// renderTemplate はファイルパスのテンプレートを読み込んでレンダリングする。
// templatePath が空の場合はデフォルトテンプレートを使う。
func renderTemplate(templatePath string, defaultTmpl string, data TemplateData) (string, error) {
	src := defaultTmpl
	if templatePath != "" {
		raw, err := os.ReadFile(templatePath)
		if err != nil {
			return "", fmt.Errorf("テンプレートファイル読み込み失敗 (%s): %w", templatePath, err)
		}
		src = string(raw)
	}

	tmpl, err := template.New("filesep").Parse(src)
	if err != nil {
		return "", fmt.Errorf("テンプレートパース失敗: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("テンプレートレンダリング失敗: %w", err)
	}
	return buf.String(), nil
}

const defaultInlineTemplate = `
【添付ファイルのお知らせ】
セキュリティポリシーにより、以下の添付ファイルを分離しました。
{{.ExpiryHours}}時間以内に下記リンクからダウンロードしてください。
{{range .Attachments}}
- {{.Name}} ({{printf "%.1f" .SizeKB}} KB)
{{end}}
ダウンロードリンク: {{.DownloadURL}}

---
`

const defaultSeparateTemplate = `元のメール「{{.Subject}}」（{{.ReceivedAt}}）の添付ファイルを分離しました。
{{.ExpiryHours}}時間以内に下記リンクからダウンロードしてください。
{{range .Attachments}}
- {{.Name}} ({{printf "%.1f" .SizeKB}} KB)
{{end}}
ダウンロードリンク: {{.DownloadURL}}`
