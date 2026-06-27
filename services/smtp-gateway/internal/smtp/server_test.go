package smtp

import (
	"net"
	"testing"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

func TestExtractSubject(t *testing.T) {
	tests := []struct {
		name string
		eml  string
		want string
	}{
		{
			name: "Subjectヘッダーを取り出す",
			eml:  "From: sender@example.com\nSubject: Hello World\nTo: user@example.com\n\nBody",
			want: "Hello World",
		},
		{
			name: "subject: と小文字でも取り出せる",
			eml:  "From: sender@example.com\nsubject: hello world\n\nBody",
			want: "hello world",
		},
		{
			name: "Subjectがない場合は空文字列",
			eml:  "From: sender@example.com\nTo: user@example.com\n\nBody",
			want: "",
		},
		{
			name: "空行より後のSubjectは取得しない",
			eml:  "From: sender@example.com\n\nSubject: In Body",
			want: "",
		},
		{
			name: "空のEMLは空文字列",
			eml:  "",
			want: "",
		},
		{
			name: "前後のスペースをトリムする",
			eml:  "Subject:   spaces around   \n\nBody",
			want: "spaces around",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSubject([]byte(tt.eml))
			if got != tt.want {
				t.Errorf("extractSubject() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractAuthResults(t *testing.T) {
	tests := []struct {
		name     string
		eml      string
		wantSPF  domain.AuthResult
		wantDKIM domain.AuthResult
		wantDMARC domain.AuthResult
	}{
		{
			name: "Authentication-Resultsヘッダーがない場合はすべてnone",
			eml:  "From: sender@example.com\nSubject: test\n\nBody",
			wantSPF: domain.AuthNone, wantDKIM: domain.AuthNone, wantDMARC: domain.AuthNone,
		},
		{
			name: "SPF/DKIM/DMARCがすべてpass",
			eml: "From: sender@example.com\nAuthentication-Results: postfix;" +
				" spf=pass smtp.mailfrom=sender@example.com;" +
				" dkim=pass header.d=example.com;" +
				" dmarc=pass (p=QUARANTINE) header.from=example.com\n\nBody",
			wantSPF: domain.AuthPass, wantDKIM: domain.AuthPass, wantDMARC: domain.AuthPass,
		},
		{
			name: "SPF/DKIM/DMARCがすべてfail",
			eml: "From: sender@example.com\nAuthentication-Results: postfix;" +
				" spf=fail smtp.mailfrom=sender@example.com;" +
				" dkim=fail header.d=example.com;" +
				" dmarc=fail\n\nBody",
			wantSPF: domain.AuthFail, wantDKIM: domain.AuthFail, wantDMARC: domain.AuthFail,
		},
		{
			name: "softfailはfailとして扱う",
			eml: "From: sender@example.com\nAuthentication-Results: postfix;" +
				" spf=softfail smtp.mailfrom=sender@example.com;" +
				" dkim=none; dmarc=none\n\nBody",
			wantSPF: domain.AuthFail, wantDKIM: domain.AuthNone, wantDMARC: domain.AuthNone,
		},
		{
			name: "SPFのみpassで残りはnone",
			eml: "From: sender@example.com\nAuthentication-Results: postfix;" +
				" spf=pass smtp.mailfrom=sender@example.com\n\nBody",
			wantSPF: domain.AuthPass, wantDKIM: domain.AuthNone, wantDMARC: domain.AuthNone,
		},
		{
			name: "折り畳みヘッダー（継続行）を正しくパースする",
			eml: "From: sender@example.com\nAuthentication-Results: postfix;\r\n" +
				"\tspf=pass smtp.mailfrom=sender@example.com;\r\n" +
				"\tdkim=pass header.d=example.com;\r\n" +
				"\tdmarc=pass\n\nBody",
			wantSPF: domain.AuthPass, wantDKIM: domain.AuthPass, wantDMARC: domain.AuthPass,
		},
		{
			name: "neutralはnoneとして扱う",
			eml: "From: sender@example.com\nAuthentication-Results: postfix;" +
				" spf=neutral smtp.mailfrom=sender@example.com;" +
				" dkim=none; dmarc=none\n\nBody",
			wantSPF: domain.AuthNone, wantDKIM: domain.AuthNone, wantDMARC: domain.AuthNone,
		},
		{
			name: "結果の後に理由文が続いてもパースできる",
			eml: "From: sender@example.com\nAuthentication-Results: postfix;" +
				" dkim=fail (bad signature) header.d=example.com;" +
				" spf=pass (sender is authorized);" +
				" dmarc=fail (p=REJECT)\n\nBody",
			wantSPF: domain.AuthPass, wantDKIM: domain.AuthFail, wantDMARC: domain.AuthFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAuthResults([]byte(tt.eml))
			if got.SPF != tt.wantSPF {
				t.Errorf("SPF = %q, want %q", got.SPF, tt.wantSPF)
			}
			if got.DKIM != tt.wantDKIM {
				t.Errorf("DKIM = %q, want %q", got.DKIM, tt.wantDKIM)
			}
			if got.DMARC != tt.wantDMARC {
				t.Errorf("DMARC = %q, want %q", got.DMARC, tt.wantDMARC)
			}
		})
	}
}

func TestIsTrusted(t *testing.T) {
	_, net172, _ := net.ParseCIDR("172.17.0.0/16")
	_, net10, _ := net.ParseCIDR("10.0.0.0/8")

	b := &smtpBackend{
		trustedIPs:  map[string]bool{"127.0.0.1": true, "192.168.1.100": true},
		trustedNets: []*net.IPNet{net172, net10},
	}

	tests := []struct {
		name     string
		remoteIP string
		want     bool
	}{
		{
			name:     "単体IPはIPマップで信頼する",
			remoteIP: "127.0.0.1",
			want:     true,
		},
		{
			name:     "2番目の単体IPも信頼する",
			remoteIP: "192.168.1.100",
			want:     true,
		},
		{
			name:     "CIDRサブネット内のIPは信頼する",
			remoteIP: "172.17.0.1",
			want:     true,
		},
		{
			name:     "CIDRサブネット内の別IPも信頼する",
			remoteIP: "172.17.42.10",
			want:     true,
		},
		{
			name:     "2番目のCIDRサブネット内のIPも信頼する",
			remoteIP: "10.100.200.1",
			want:     true,
		},
		{
			name:     "サブネット外のIPは拒否する",
			remoteIP: "172.18.0.1",
			want:     false,
		},
		{
			name:     "ホワイトリストにないIPは拒否する",
			remoteIP: "192.168.1.200",
			want:     false,
		},
		{
			name:     "空文字列は拒否する",
			remoteIP: "",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := b.isTrusted(tt.remoteIP)
			if got != tt.want {
				t.Errorf("isTrusted(%q) = %v, want %v", tt.remoteIP, got, tt.want)
			}
		})
	}
}

func TestResolveTrustedSources(t *testing.T) {
	sources := []string{
		"127.0.0.1",
		"172.17.0.0/16",
		"10.0.0.0/8",
		"256.256.256.256/99", // 不正なCIDR
	}
	ips, nets := resolveTrustedSources(sources)

	if !ips["127.0.0.1"] {
		t.Error("単体IPが登録されていない")
	}
	if len(nets) != 2 {
		t.Errorf("CIDRネットワーク数 = %d, want 2", len(nets))
	}
}
