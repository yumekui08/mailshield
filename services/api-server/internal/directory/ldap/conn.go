package ldap

import (
	"crypto/tls"
	"fmt"
	"time"

	goldap "github.com/go-ldap/ldap/v3"
)

// TLSMode は LDAP 接続の暗号化方式。
type TLSMode string

const (
	TLSNone     TLSMode = "none"
	TLSStartTLS TLSMode = "starttls"
	TLSLDAPS    TLSMode = "ldaps"
)

// ConnConfig は LDAP 接続に必要な設定。
// config パッケージ（viper/mapstructure）から独立させ、本パッケージが上位の
// 設定ローダーに依存しないようにする。
type ConnConfig struct {
	Host          string
	Port          int
	TLS           TLSMode
	TLSSkipVerify bool
	BindDN        string
	BindPassword  string
	SearchTimeout time.Duration
	PageSize      uint32
}

type conn struct {
	c        *goldap.Conn
	timeout  time.Duration
	pageSize uint32
}

// Dial は LDAP サーバーへ接続し、サービスアカウントで bind した Searcher を返す。
// 呼び出し後は Close() で接続を解放すること。
func Dial(cfg ConnConfig) (Searcher, error) {
	scheme := "ldap"
	if cfg.TLS == TLSLDAPS {
		scheme = "ldaps"
	}
	addr := fmt.Sprintf("%s://%s:%d", scheme, cfg.Host, cfg.Port)

	tlsConfig := &tls.Config{
		ServerName:         cfg.Host,
		InsecureSkipVerify: cfg.TLSSkipVerify, //nolint:gosec // 開発・テスト用の明示的なオプトイン
	}

	c, err := goldap.DialURL(addr, goldap.DialWithTLSConfig(tlsConfig))
	if err != nil {
		return nil, fmt.Errorf("LDAP 接続失敗 (addr=%s): %w", addr, err)
	}

	if cfg.TLS == TLSStartTLS {
		if err := c.StartTLS(tlsConfig); err != nil {
			c.Close()
			return nil, fmt.Errorf("STARTTLS 失敗 (addr=%s): %w", addr, err)
		}
	}

	if err := c.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
		c.Close()
		return nil, fmt.Errorf("LDAP bind 失敗 (bind_dn=%s): %w", cfg.BindDN, err)
	}

	pageSize := cfg.PageSize
	if pageSize == 0 {
		pageSize = 500
	}
	timeout := cfg.SearchTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	return &conn{c: c, timeout: timeout, pageSize: pageSize}, nil
}

// SearchUsers は baseDN 配下で filter にマッチするエントリをページング検索で取得する。
// SearchWithPaging は AD 等のサーバー側件数上限（既定 1000 件）を超えるディレクトリでも
// 内部で複数ページを辿って全件取得するため、大規模ディレクトリでの取りこぼしを防ぐ。
func (w *conn) SearchUsers(baseDN, filter string, attrs []string) ([]Entry, error) {
	req := goldap.NewSearchRequest(
		baseDN,
		goldap.ScopeWholeSubtree,
		goldap.NeverDerefAliases,
		0, int(w.timeout.Seconds()), false,
		filter,
		attrs,
		nil,
	)

	res, err := w.c.SearchWithPaging(req, w.pageSize)
	if err != nil {
		return nil, fmt.Errorf("LDAP 検索失敗 (base_dn=%s, filter=%s): %w", baseDN, filter, err)
	}

	entries := make([]Entry, 0, len(res.Entries))
	for _, e := range res.Entries {
		attrMap := make(map[string][]string, len(e.Attributes))
		for _, a := range e.Attributes {
			attrMap[a.Name] = a.Values
		}
		entries = append(entries, Entry{DN: e.DN, Attributes: attrMap})
	}
	return entries, nil
}

func (w *conn) Close() error {
	return w.c.Close()
}
