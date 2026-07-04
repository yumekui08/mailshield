package deliver

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"strconv"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/config"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

// DefaultName は destination 未指定のルールに使われる予約 deliverer 名。
const DefaultName = "default"

// nameRe は deliverer 名として許可する文字。":" を含む名前を禁止することで
// host:port 形式の destination と曖昧にならないようにする。
var nameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Registry は名前付き deliverer の集合を管理し、policy の destination を解決して配送する。
//
// destination の解決順序:
//  1. 空文字列       → deliverers.default → （未定義なら）reinject.host:port（平文 SMTP）
//  2. deliverer 名   → 該当する名前付き deliverer
//  3. host[:port]    → その宛先への平文 SMTP（後方互換。port 省略時は defaultPort を補完）
type Registry struct {
	deliverers map[string]*SMTPDeliverer
	// legacyDefault は reinject.host:port から構築した後方互換のデフォルト配送先。
	// deliverers.default が定義されていない場合のフォールバック。nil の場合あり。
	legacyDefault *SMTPDeliverer
	// defaultPort は host:port 形式の destination で port が省略された場合の補完値。
	defaultPort int
}

// NewRegistry は deliverers 設定と reinject 設定（後方互換）から Registry を構築する。
// 設定不正（未知の type / tls、host 未設定、不正な名前）は起動時エラーとして返す。
func NewRegistry(cfgs map[string]config.DelivererConfig, reinjectHost string, reinjectPort int) (*Registry, error) {
	r := &Registry{
		deliverers:  make(map[string]*SMTPDeliverer, len(cfgs)),
		defaultPort: reinjectPort,
	}
	if r.defaultPort == 0 {
		r.defaultPort = 25
	}

	for name, dc := range cfgs {
		if !nameRe.MatchString(name) {
			return nil, fmt.Errorf("deliverer 名が不正です (%q): 英数字・ハイフン・アンダースコアのみ使用できます", name)
		}
		d, err := newSMTPDeliverer(name, dc)
		if err != nil {
			return nil, err
		}
		r.deliverers[name] = d
		slog.Info("deliverer 登録",
			"name", name,
			"addr", d.Addr(),
			"tls", string(d.tls),
			"auth", d.username != "")
	}

	if _, hasDefault := r.deliverers[DefaultName]; !hasDefault && reinjectHost != "" {
		r.legacyDefault = &SMTPDeliverer{
			name: "reinject",
			host: reinjectHost,
			port: r.defaultPort,
			tls:  TLSNone,
		}
		slog.Info("デフォルト配送先: reinject 設定を使用（deliverers.default 未定義）",
			"addr", r.legacyDefault.Addr())
	}

	return r, nil
}

// newSMTPDeliverer は 1 件の deliverer 設定を検証して SMTPDeliverer を構築する。
func newSMTPDeliverer(name string, dc config.DelivererConfig) (*SMTPDeliverer, error) {
	if dc.Type != "" && dc.Type != "smtp" {
		return nil, fmt.Errorf("deliverer %s: 未対応の type %q（現在は smtp のみ対応）", name, dc.Type)
	}
	if dc.Host == "" {
		return nil, fmt.Errorf("deliverer %s: host が未設定です", name)
	}

	tlsMode := TLSMode(dc.TLS)
	switch tlsMode {
	case "":
		tlsMode = TLSNone
	case TLSNone, TLSStartTLS, TLSImplicit:
	default:
		return nil, fmt.Errorf("deliverer %s: 未対応の tls %q（none | starttls | tls）", name, dc.TLS)
	}

	port := dc.Port
	if port == 0 {
		port = 25
	}

	if dc.Auth.Username != "" && tlsMode == TLSNone && !isLocalhost(dc.Host) {
		slog.Warn("deliverer: 平文接続での SMTP AUTH は送信時に拒否されます（tls: starttls か tls を設定してください）",
			"name", name, "host", dc.Host)
	}

	return &SMTPDeliverer{
		name:               name,
		host:               dc.Host,
		port:               port,
		tls:                tlsMode,
		username:           dc.Auth.Username,
		password:           dc.Auth.Password,
		insecureSkipVerify: dc.TLSSkipVerify,
	}, nil
}

// Deliver は destination を解決してメールを配送する。
// policy.Deliverer インターフェースを実装する。
func (r *Registry) Deliver(ctx context.Context, mail *domain.Mail, destination string) error {
	d, err := r.Resolve(destination)
	if err != nil {
		return err
	}
	return d.Deliver(ctx, mail)
}

// Resolve は destination 文字列を SMTPDeliverer に解決する。
func (r *Registry) Resolve(destination string) (*SMTPDeliverer, error) {
	if destination == "" {
		if d, ok := r.deliverers[DefaultName]; ok {
			return d, nil
		}
		if r.legacyDefault != nil {
			return r.legacyDefault, nil
		}
		return nil, fmt.Errorf("deliver アクションの宛先が未設定です（policy の destination、deliverers.default、または reinject.host を設定してください）")
	}

	if d, ok := r.deliverers[destination]; ok {
		return d, nil
	}

	// host[:port] 形式（後方互換）: 平文 SMTP で送信する
	host, portStr, err := net.SplitHostPort(destination)
	if err != nil {
		// port 省略とみなして defaultPort を補完する。
		// net.JoinHostPort を使うことでベア IPv6 アドレス（例: ::1）も正しく扱える。
		joined := net.JoinHostPort(destination, strconv.Itoa(r.defaultPort))
		host, portStr, err = net.SplitHostPort(joined)
		if err != nil {
			return nil, fmt.Errorf("宛先の解決失敗 (%s): deliverer 名にも host:port にも解釈できません: %w", destination, err)
		}
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("宛先ポートのパース失敗 (%s): %w", destination, err)
	}

	return &SMTPDeliverer{
		name: destination,
		host: host,
		port: port,
		tls:  TLSNone,
	}, nil
}
