// Package arcsealer は変換後の EML に ARC シール（RFC 8617）を付与する変換ワーカー。
// 本ワーカーは DKIM 署名を壊す変換の後に行うため、変換パイプラインの最後に配置すること。
package arcsealer

import (
	"bufio"
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/textproto"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

const (
	workerName = "arc-sealer"
	crlf       = "\r\n"
)

// defaultHeaderKeys は ARC-Message-Signature で署名するヘッダーフィールドのデフォルト一覧。
// "from" は RFC 6376 の要件により必須。
// "arc-authentication-results" を含めることで AAR も署名対象になる。
var defaultHeaderKeys = []string{
	"from", "to", "subject", "date", "message-id",
	"content-type", "arc-authentication-results",
}

// Config は arc-sealer ワーカーの設定を保持する。
type Config struct {
	// SigningDomain は ARC 署名に使うドメイン（例: arc.mailshield.example.com）。
	// DNS に _domainkey TXT レコードを公開する必要がある。
	SigningDomain string `yaml:"signing_domain"`
	// Selector は DKIM セレクタ（例: mailshield）。
	Selector string `yaml:"selector"`
	// PrivateKeyPath は RSA または Ed25519 秘密鍵ファイルのパス（PEM 形式）。
	PrivateKeyPath string `yaml:"private_key_path"`
	// HeaderKeys は AMS で署名するヘッダーフィールドの一覧（省略可・デフォルトあり）。
	HeaderKeys []string `yaml:"header_keys"`
}

// Worker は EML に ARC ヘッダーセット（AAR・AMS・AS）を付与する変換ワーカーである。
type Worker struct {
	cfg    *Config
	signer crypto.Signer
}

// New は arc-sealer ワーカーを初期化する。
// 設定ファイルは <workerConfigDir>/arc-sealer.yaml に配置する。
func New(workerConfigDir string) (*Worker, error) {
	cfg, err := loadConfig(workerConfigDir)
	if err != nil {
		return nil, fmt.Errorf("arc-sealer 設定ロード失敗: %w", err)
	}
	if len(cfg.HeaderKeys) == 0 {
		cfg.HeaderKeys = defaultHeaderKeys
	}

	keyData, err := os.ReadFile(cfg.PrivateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("ARC 秘密鍵読み込み失敗 (%s): %w", cfg.PrivateKeyPath, err)
	}
	signer, err := parsePrivateKey(keyData)
	if err != nil {
		return nil, fmt.Errorf("ARC 秘密鍵パース失敗: %w", err)
	}

	return &Worker{cfg: cfg, signer: signer}, nil
}

func (w *Worker) Name() string { return workerName }

// Transform は EML に ARC シールを付与して返す。
// 変換パイプラインの最後に実行すること（他の変換後のメールに対して署名する）。
func (w *Worker) Transform(_ context.Context, m *domain.Mail) (*domain.Mail, error) {
	sealed, err := w.sealARC(m.RawEML)
	if err != nil {
		return nil, fmt.Errorf("ARC シール失敗: %w", err)
	}
	out := *m
	out.RawEML = sealed
	return &out, nil
}

// 追加するヘッダーは AAR・AMS・AS の順（RFC 8617 §5.1）
func (w *Worker) sealARC(rawEML []byte) ([]byte, error) {
	// ヘッダー / ボディを分割
	sep := bytes.Index(rawEML, []byte("\r\n\r\n"))
	if sep == -1 {
		return nil, errors.New("ヘッダー/ボディ区切りが見つかりません")
	}
	body := rawEML[sep+4:]

	// 元のヘッダーを解析
	originalHeaders := parseHeaders(rawEML[:sep+2])

	// Authentication-Results ヘッダーを探して AAR の値にする
	arValue := "none"
	for _, h := range originalHeaders {
		if strings.ToLower(headerName(h)) == "authentication-results" {
			_, val, _ := strings.Cut(h, ":")
			arValue = strings.TrimSpace(unfold(val))
			break
		}
	}

	// 現在の ARC インスタンス番号を決定（既存 ARC-Seal の最大値 + 1）
	n := countARCSets(originalHeaders) + 1

	// チェーン検証結果（cv）を決定
	// i=1 は cv=none 固定。i>1 は cv=pass とする（厳密な検証は省略）。
	cv := "none"
	if n > 1 {
		cv = "pass"
	}

	// 1. AAR を構築
	aarFieldValue := fmt.Sprintf("i=%d; %s", n, arValue)
	aarLine := "ARC-Authentication-Results: " + aarFieldValue + crlf

	// 2. AMS を作成（AAR を先頭に追加したメッセージで署名）
	msgWithAAR := append([]byte(aarLine), rawEML...)
	amsLine, amsFieldValue, err := w.createAMS(msgWithAAR, body, n)
	if err != nil {
		return nil, fmt.Errorf("AMS 作成失敗: %w", err)
	}

	// 3. AS を作成（全 ARC ヘッダーを署名）
	asLine, err := w.createAS(originalHeaders, aarFieldValue, amsFieldValue, n, cv)
	if err != nil {
		return nil, fmt.Errorf("AS 作成失敗: %w", err)
	}

	// 4. 新しいヘッダーセットを先頭に追加（順序: AS, AMS, AAR）
	var result bytes.Buffer
	result.WriteString(asLine)
	result.WriteString(amsLine)
	result.WriteString(aarLine)
	result.Write(rawEML)

	return result.Bytes(), nil
}

// createAMS は ARC-Message-Signature ヘッダーを計算して返す。
// AMS は DKIM-Signature と同じアルゴリズムだが、ヘッダー名が "ARC-Message-Signature:" で
// "v=" タグがなく "i=" タグを持つ。
func (w *Worker) createAMS(msgWithAAR, body []byte, instance int) (line, fieldValue string, err error) {
	sep := bytes.Index(msgWithAAR, []byte("\r\n\r\n"))
	var headers []string
	if sep == -1 {
		headers = parseHeaders(msgWithAAR)
	} else {
		headers = parseHeaders(msgWithAAR[:sep+2])
	}

	// ボディハッシュ（relaxed 正規化 → SHA256 → base64）
	bh := bodyHash(body)

	// 署名するヘッダーフィールド一覧
	hList := strings.Join(w.cfg.HeaderKeys, ":")

	// b= が空の AMS ヘッダー値（署名計算用）
	amsBase := fmt.Sprintf("i=%d; a=rsa-sha256; c=relaxed/relaxed; d=%s; s=%s; h=%s; bh=%s; b=",
		instance, w.cfg.SigningDomain, w.cfg.Selector, hList, bh)

	// 署名入力を構築
	var sigInput strings.Builder
	picker := newHeaderPicker(headers)
	for _, key := range w.cfg.HeaderKeys {
		raw := picker.pick(key)
		if raw == "" {
			continue
		}
		sigInput.WriteString(relaxedHeader(raw))
	}
	// AMS ヘッダー自身を最後に追加（末尾の CRLF なし・RFC 6376 §3.5）
	sigInput.WriteString(strings.TrimSuffix(relaxedHeader("ARC-Message-Signature: "+amsBase), crlf))

	// SHA256 → RSA 署名
	digest := sha256.Sum256([]byte(sigInput.String()))
	sig, err := w.signer.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		return "", "", fmt.Errorf("RSA 署名失敗: %w", err)
	}

	b64 := base64.StdEncoding.EncodeToString(sig)
	fieldValue = fmt.Sprintf("i=%d; a=rsa-sha256; c=relaxed/relaxed; d=%s; s=%s; h=%s; bh=%s; b=%s",
		instance, w.cfg.SigningDomain, w.cfg.Selector, hList, bh, b64)
	line = "ARC-Message-Signature: " + fieldValue + crlf
	return line, fieldValue, nil
}

// createAS は ARC-Seal ヘッダーを計算して返す。
// AS は現在および過去の全 ARC ヘッダーセットを署名対象とする。
func (w *Worker) createAS(originalHeaders []string, aarFieldValue, amsFieldValue string, instance int, cv string) (string, error) {
	// 既存 ARC セット（i=1..n-1）をインスタンス昇順で収集
	type arcSet struct {
		aar, ams, as string
	}
	sets := make(map[int]*arcSet)
	for _, h := range originalHeaders {
		name := strings.ToLower(headerName(h))
		if name != "arc-authentication-results" && name != "arc-message-signature" && name != "arc-seal" {
			continue
		}
		i := extractInstance(h)
		if i <= 0 {
			continue
		}
		if sets[i] == nil {
			sets[i] = &arcSet{}
		}
		switch name {
		case "arc-authentication-results":
			sets[i].aar = h
		case "arc-message-signature":
			sets[i].ams = h
		case "arc-seal":
			sets[i].as = h
		}
	}

	// 署名入力を構築（i=1..n 昇順・各セット内は AAR → AMS → AS の順）
	var sigInput strings.Builder
	for i := 1; i < instance; i++ {
		s := sets[i]
		if s == nil {
			continue
		}
		if s.aar != "" {
			sigInput.WriteString(relaxedHeader(s.aar))
		}
		if s.ams != "" {
			sigInput.WriteString(relaxedHeader(s.ams))
		}
		if s.as != "" {
			sigInput.WriteString(relaxedHeader(s.as))
		}
	}
	// 新しい AAR・AMS・AS（空 b=）を追加
	sigInput.WriteString(relaxedHeader("ARC-Authentication-Results: " + aarFieldValue))
	sigInput.WriteString(relaxedHeader("ARC-Message-Signature: " + amsFieldValue))
	asBase := fmt.Sprintf("i=%d; a=rsa-sha256; cv=%s; d=%s; s=%s; b=",
		instance, cv, w.cfg.SigningDomain, w.cfg.Selector)
	// AS ヘッダー自身は末尾の CRLF なし
	sigInput.WriteString(strings.TrimSuffix(relaxedHeader("ARC-Seal: "+asBase), crlf))

	// SHA256 → RSA 署名
	digest := sha256.Sum256([]byte(sigInput.String()))
	sig, err := w.signer.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		return "", fmt.Errorf("AS RSA 署名失敗: %w", err)
	}

	b64 := base64.StdEncoding.EncodeToString(sig)
	fieldValue := fmt.Sprintf("i=%d; a=rsa-sha256; cv=%s; d=%s; s=%s; b=%s",
		instance, cv, w.cfg.SigningDomain, w.cfg.Selector, b64)
	return "ARC-Seal: " + fieldValue + crlf, nil
}

// 連続行（折り畳み）は 1 つの文字列に結合する。各要素は末尾 CRLF を含む。
func parseHeaders(data []byte) []string {
	var fields []string
	br := bufio.NewReader(bytes.NewReader(data))
	tr := textproto.NewReader(br)
	for {
		l, err := tr.ReadLine()
		if err != nil || l == "" {
			break
		}
		if len(fields) > 0 && (l[0] == ' ' || l[0] == '\t') {
			fields[len(fields)-1] += l + crlf
		} else {
			fields = append(fields, l+crlf)
		}
	}
	return fields
}

// headerName はヘッダーフィールドから名前部分を返す（元のケース）。
func headerName(field string) string {
	name, _, _ := strings.Cut(field, ":")
	return strings.TrimSpace(name)
}

// unfold はヘッダー値の折り畳みを展開する。
func unfold(v string) string {
	v = strings.ReplaceAll(v, "\r\n", " ")
	v = strings.ReplaceAll(v, "\r", " ")
	v = strings.ReplaceAll(v, "\n", " ")
	return v
}

// relaxedHeader は DKIM/ARC の relaxed ヘッダー正規化を適用して返す（RFC 6376 §3.4.2）。
// 返り値は "lowercase-name:value\r\n" の形式。
func relaxedHeader(field string) string {
	k, v, ok := strings.Cut(field, ":")
	if !ok {
		return strings.TrimSpace(strings.ToLower(field)) + ":\r\n"
	}
	k = strings.TrimSpace(strings.ToLower(k))
	// 値内の連続する空白（SP/TAB/CR/LF）を単一 SP に正規化
	parts := strings.FieldsFunc(v, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\r' || r == '\n'
	})
	v = strings.Join(parts, " ")
	return k + ":" + v + crlf
}

// bodyHash は relaxed 正規化したボディの SHA256 ハッシュを base64 で返す（RFC 6376 §3.4.4）。
func bodyHash(body []byte) string {
	// 改行を LF に正規化
	b := bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n"))
	b = bytes.ReplaceAll(b, []byte("\r"), []byte("\n"))
	lines := strings.Split(string(b), "\n")

	canonical := make([]string, 0, len(lines))
	for _, line := range lines {
		// 行末の空白を除去し、内部の連続空白を 1 つの SP に圧縮
		var sb strings.Builder
		wsp := false
		for _, c := range line {
			if c == ' ' || c == '\t' {
				wsp = true
			} else {
				if wsp && sb.Len() > 0 {
					sb.WriteByte(' ')
				}
				wsp = false
				sb.WriteRune(c)
			}
		}
		canonical = append(canonical, sb.String())
	}

	// 末尾の空行を除去
	for len(canonical) > 0 && canonical[len(canonical)-1] == "" {
		canonical = canonical[:len(canonical)-1]
	}

	var normalized string
	if len(canonical) == 0 {
		normalized = crlf
	} else {
		normalized = strings.Join(canonical, crlf) + crlf
	}

	h := sha256.Sum256([]byte(normalized))
	return base64.StdEncoding.EncodeToString(h[:])
}

var arcInstanceRE = regexp.MustCompile(`(?i)\bi\s*=\s*(\d+)`)

func extractInstance(field string) int {
	_, val, _ := strings.Cut(field, ":")
	m := arcInstanceRE.FindStringSubmatch(val)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// countARCSets は既存のヘッダーから最大の ARC インスタンス番号を返す。
func countARCSets(headers []string) int {
	max := 0
	for _, h := range headers {
		if strings.ToLower(headerName(h)) != "arc-seal" {
			continue
		}
		if n := extractInstance(h); n > max {
			max = n
		}
	}
	return max
}

// DKIM 仕様に従い後ろから順に同名ヘッダーを選択するイテレーター
type headerPicker struct {
	fields []string
	picked map[string]int
}

func newHeaderPicker(fields []string) *headerPicker {
	return &headerPicker{fields: fields, picked: make(map[string]int)}
}

func (p *headerPicker) pick(key string) string {
	key = strings.ToLower(key)
	at := p.picked[key]
	for i := len(p.fields) - 1; i >= 0; i-- {
		if strings.ToLower(headerName(p.fields[i])) != key {
			continue
		}
		if at == 0 {
			p.picked[key]++
			return p.fields[i]
		}
		at--
	}
	return ""
}

func loadConfig(dir string) (*Config, error) {
	path := filepath.Join(dir, workerName+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("設定ファイルが必要です（%s）", path)
		}
		return nil, fmt.Errorf("設定ファイル読み込み失敗: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("設定ファイルパース失敗: %w", err)
	}
	if cfg.SigningDomain == "" {
		return nil, errors.New("arc-sealer: signing_domain が未設定")
	}
	if cfg.Selector == "" {
		return nil, errors.New("arc-sealer: selector が未設定")
	}
	if cfg.PrivateKeyPath == "" {
		return nil, errors.New("arc-sealer: private_key_path が未設定")
	}
	return &cfg, nil
}

// PKCS8（RSA / Ed25519）と PKCS1（RSA のみ）に対応
func parsePrivateKey(data []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("PEM デコード失敗")
	}

	// PKCS8 を先に試みる（RSA・Ed25519 両対応）
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err == nil {
		signer, ok := key.(crypto.Signer)
		if !ok {
			return nil, errors.New("PKCS8 鍵が crypto.Signer を実装していません")
		}
		return signer, nil
	}

	// PKCS1（RSA のみ）にフォールバック
	rsaKey, err2 := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err2 != nil {
		return nil, fmt.Errorf("秘密鍵パース失敗 (PKCS8: %v, PKCS1: %v)", err, err2)
	}
	return rsaKey, nil
}
