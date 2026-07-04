package deliver

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

// ─── テスト用ミニマル SMTP サーバー ────────────────────────────
//
// STARTTLS・AUTH PLAIN / LOGIN を検証するための最小実装。
// net/smtp クライアントが必要とするコマンドのみ対応する。

type testSMTPServer struct {
	listener net.Listener
	tlsCert  *tls.Certificate // nil なら STARTTLS を広告しない
	authMech string           // "" なら AUTH を広告しない（"PLAIN" / "LOGIN"）
	wantUser string
	wantPass string

	mu       sync.Mutex
	from     string
	rcpts    []string
	data     string
	authOK   bool
	usedTLS  bool
	authLine string // 受信した AUTH コマンド（メカニズム検証用）
}

func newTestSMTPServer(t *testing.T) *testSMTPServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &testSMTPServer{listener: ln}
	t.Cleanup(func() { ln.Close() })
	return s
}

func (s *testSMTPServer) port() int {
	return s.listener.Addr().(*net.TCPAddr).Port
}

// serveOne は 1 接続だけ処理する。テストごとに goroutine で起動する。
func (s *testSMTPServer) serveOne(t *testing.T) {
	t.Helper()
	go func() {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		s.handle(conn)
	}()
}

func (s *testSMTPServer) handle(conn net.Conn) {
	conn.SetDeadline(time.Now().Add(10 * time.Second)) //nolint:errcheck
	w := bufio.NewWriter(conn)
	r := bufio.NewReader(conn)
	writeLine := func(line string) {
		w.WriteString(line + "\r\n") //nolint:errcheck
		w.Flush()                    //nolint:errcheck
	}

	writeLine("220 test-smtp ready")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd := strings.ToUpper(line)

		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			exts := []string{"250-test-smtp"}
			if s.tlsCert != nil {
				s.mu.Lock()
				usedTLS := s.usedTLS
				s.mu.Unlock()
				if !usedTLS {
					exts = append(exts, "250-STARTTLS")
				}
			}
			if s.authMech != "" {
				exts = append(exts, "250-AUTH "+s.authMech)
			}
			exts = append(exts, "250 8BITMIME")
			for _, e := range exts {
				writeLine(e)
			}
		case cmd == "STARTTLS":
			writeLine("220 Ready to start TLS")
			tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{*s.tlsCert}})
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			s.mu.Lock()
			s.usedTLS = true
			s.mu.Unlock()
			conn = tlsConn
			w = bufio.NewWriter(conn)
			r = bufio.NewReader(conn)
		case strings.HasPrefix(cmd, "AUTH PLAIN"):
			s.mu.Lock()
			s.authLine = line
			s.mu.Unlock()
			// AUTH PLAIN <base64("\0user\0pass")>
			parts := strings.SplitN(line, " ", 3)
			if len(parts) != 3 {
				writeLine("501 malformed")
				continue
			}
			decoded, err := base64.StdEncoding.DecodeString(parts[2])
			if err != nil {
				writeLine("501 bad base64")
				continue
			}
			fields := strings.Split(string(decoded), "\x00")
			if len(fields) == 3 && fields[1] == s.wantUser && fields[2] == s.wantPass {
				s.mu.Lock()
				s.authOK = true
				s.mu.Unlock()
				writeLine("235 ok")
			} else {
				writeLine("535 auth failed")
			}
		case strings.HasPrefix(cmd, "AUTH LOGIN"):
			s.mu.Lock()
			s.authLine = line
			s.mu.Unlock()
			writeLine("334 " + base64.StdEncoding.EncodeToString([]byte("Username:")))
			userLine, _ := r.ReadString('\n')
			writeLine("334 " + base64.StdEncoding.EncodeToString([]byte("Password:")))
			passLine, _ := r.ReadString('\n')
			user, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(userLine))
			pass, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(passLine))
			if string(user) == s.wantUser && string(pass) == s.wantPass {
				s.mu.Lock()
				s.authOK = true
				s.mu.Unlock()
				writeLine("235 ok")
			} else {
				writeLine("535 auth failed")
			}
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			s.mu.Lock()
			s.from = line
			s.mu.Unlock()
			writeLine("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			s.mu.Lock()
			s.rcpts = append(s.rcpts, line)
			s.mu.Unlock()
			writeLine("250 ok")
		case cmd == "DATA":
			writeLine("354 go ahead")
			var b strings.Builder
			for {
				dl, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(dl, "\r\n") == "." {
					break
				}
				b.WriteString(dl)
			}
			s.mu.Lock()
			s.data = b.String()
			s.mu.Unlock()
			writeLine("250 accepted")
		case cmd == "QUIT":
			writeLine("221 bye")
			return
		default:
			writeLine("500 unknown")
		}
	}
}

// selfSignedCert は 127.0.0.1 用の自己署名証明書を生成する。
func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func testMail() *domain.Mail {
	return &domain.Mail{
		MessageID:   "test-message-id",
		FromAddress: "sender@external.test",
		ToAddresses: []string{"rcpt@internal.test"},
		RawEML:      []byte("Subject: hello\r\n\r\nbody\r\n"),
	}
}

func deliverCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// ─── SMTPDeliverer テスト ────────────────────────────────────

// TestDeliver_Plain は平文 SMTP（従来の再インジェクト相当）で配送できることを確認する。
func TestDeliver_Plain(t *testing.T) {
	srv := newTestSMTPServer(t)
	srv.serveOne(t)

	d := &SMTPDeliverer{name: "plain", host: "127.0.0.1", port: srv.port(), tls: TLSNone}
	if err := d.Deliver(deliverCtx(t), testMail()); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	if !strings.Contains(srv.from, "sender@external.test") {
		t.Errorf("MAIL FROM が届いていません: %q", srv.from)
	}
	if len(srv.rcpts) != 1 || !strings.Contains(srv.rcpts[0], "rcpt@internal.test") {
		t.Errorf("RCPT TO が期待と異なります: %v", srv.rcpts)
	}
	if !strings.Contains(srv.data, "Subject: hello") {
		t.Errorf("DATA 本文が届いていません: %q", srv.data)
	}
}

// TestDeliver_StartTLSWithAuthPlain は STARTTLS + AUTH PLAIN で配送できることを確認する
// （SendGrid / SES の :587 と同じ接続方式）。
func TestDeliver_StartTLSWithAuthPlain(t *testing.T) {
	cert := selfSignedCert(t)
	srv := newTestSMTPServer(t)
	srv.tlsCert = &cert
	srv.authMech = "PLAIN LOGIN"
	srv.wantUser = "apikey"
	srv.wantPass = "SG.secret"
	srv.serveOne(t)

	d := &SMTPDeliverer{
		name: "sendgrid", host: "127.0.0.1", port: srv.port(),
		tls: TLSStartTLS, username: "apikey", password: "SG.secret",
		insecureSkipVerify: true,
	}
	if err := d.Deliver(deliverCtx(t), testMail()); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	if !srv.usedTLS {
		t.Error("STARTTLS が使われていません")
	}
	if !srv.authOK {
		t.Error("AUTH が成功していません")
	}
	if !strings.HasPrefix(strings.ToUpper(srv.authLine), "AUTH PLAIN") {
		t.Errorf("PLAIN が選択されるべきです: %q", srv.authLine)
	}
	if !strings.Contains(srv.data, "Subject: hello") {
		t.Errorf("DATA 本文が届いていません: %q", srv.data)
	}
}

// TestDeliver_StartTLSWithAuthLogin はサーバーが LOGIN のみ広告する場合に
// LOGIN へフォールバックすることを確認する（Amazon SES 相当）。
func TestDeliver_StartTLSWithAuthLogin(t *testing.T) {
	cert := selfSignedCert(t)
	srv := newTestSMTPServer(t)
	srv.tlsCert = &cert
	srv.authMech = "LOGIN"
	srv.wantUser = "AKIAEXAMPLE"
	srv.wantPass = "ses-smtp-password"
	srv.serveOne(t)

	d := &SMTPDeliverer{
		name: "ses", host: "127.0.0.1", port: srv.port(),
		tls: TLSStartTLS, username: "AKIAEXAMPLE", password: "ses-smtp-password",
		insecureSkipVerify: true,
	}
	if err := d.Deliver(deliverCtx(t), testMail()); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	if !srv.authOK {
		t.Error("AUTH LOGIN が成功していません")
	}
	if !strings.HasPrefix(strings.ToUpper(srv.authLine), "AUTH LOGIN") {
		t.Errorf("LOGIN が選択されるべきです: %q", srv.authLine)
	}
}

// TestDeliver_StartTLSNotSupported は STARTTLS 指定なのにサーバーが
// 非対応の場合にエラーになる（平文で送らない）ことを確認する。
func TestDeliver_StartTLSNotSupported(t *testing.T) {
	srv := newTestSMTPServer(t) // tlsCert なし → STARTTLS を広告しない
	srv.serveOne(t)

	d := &SMTPDeliverer{name: "strict", host: "127.0.0.1", port: srv.port(), tls: TLSStartTLS}
	err := d.Deliver(deliverCtx(t), testMail())
	if err == nil {
		t.Fatal("STARTTLS 非対応サーバーへの配送はエラーになるべきです")
	}
	if !strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("エラーメッセージに STARTTLS が含まれていません: %v", err)
	}
}

// TestDeliver_AuthFailed は認証失敗がエラーとして返ることを確認する。
func TestDeliver_AuthFailed(t *testing.T) {
	cert := selfSignedCert(t)
	srv := newTestSMTPServer(t)
	srv.tlsCert = &cert
	srv.authMech = "PLAIN"
	srv.wantUser = "user"
	srv.wantPass = "correct"
	srv.serveOne(t)

	d := &SMTPDeliverer{
		name: "badauth", host: "127.0.0.1", port: srv.port(),
		tls: TLSStartTLS, username: "user", password: "wrong",
		insecureSkipVerify: true,
	}
	err := d.Deliver(deliverCtx(t), testMail())
	if err == nil || !strings.Contains(err.Error(), "AUTH") {
		t.Fatalf("認証失敗エラーが返るべきです: %v", err)
	}
}

// TestDeliver_ConnectionRefused は接続失敗が deliverer 名付きのエラーになることを確認する。
func TestDeliver_ConnectionRefused(t *testing.T) {
	// 予約だけして閉じたポートに接続する
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	d := &SMTPDeliverer{name: "unreachable", host: "127.0.0.1", port: port, tls: TLSNone}
	err = d.Deliver(deliverCtx(t), testMail())
	if err == nil {
		t.Fatal("接続失敗はエラーになるべきです")
	}
	if !strings.Contains(err.Error(), "unreachable") {
		t.Errorf("エラーに deliverer 名が含まれるべきです: %v", err)
	}
}

// アドレス組み立ての確認（ベア IPv6 も JoinHostPort で扱えること）
func TestSMTPDeliverer_Addr(t *testing.T) {
	d := &SMTPDeliverer{host: "::1", port: 25}
	if got := d.Addr(); got != "[::1]:25" {
		t.Errorf("Addr() = %q, want %q", got, "[::1]:25")
	}
}
