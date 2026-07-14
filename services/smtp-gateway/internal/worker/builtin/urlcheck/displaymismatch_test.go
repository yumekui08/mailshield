package urlcheck

import (
	"context"
	"strings"
	"testing"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
)

func scoresWithMismatch() ScoresConfig {
	return ScoresConfig{DenyListMatch: 100, ReputationAPIHit: 90, DisplayMismatch: 70}
}

func TestDisplayMismatch_Detected(t *testing.T) {
	w := newWorker(nil, nil, scoresWithMismatch())
	// 表示は正規サイト、リンク先は攻撃者サイト
	html := `<html><body>
	    <a href="https://phishing.evil.test/login">https://www.bank.example/login</a>
	</body></html>`
	res, err := w.Inspect(context.Background(), &domain.Mail{RawEML: buildHTMLEML(html)})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Detected {
		t.Errorf("表示テキスト不一致を検知すべき (score=%d)", res.Score)
	}
	mismatches, _ := res.Details["display_mismatches"].([]string)
	if len(mismatches) == 0 {
		t.Fatal("display_mismatches が空")
	}
	if !strings.Contains(mismatches[0], "bank.example") || !strings.Contains(mismatches[0], "evil.test") {
		t.Errorf("不一致内容が期待と異なる: %v", mismatches)
	}
}

func TestDisplayMismatch_SameDomainNotFlagged(t *testing.T) {
	w := newWorker(nil, nil, scoresWithMismatch())
	// 表示・リンク先とも同一登録可能ドメイン（サブドメイン差は許容）
	html := `<a href="https://www.bank.example/login">bank.example</a>`
	res, err := w.Inspect(context.Background(), &domain.Mail{RawEML: buildHTMLEML(html)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Detected {
		t.Errorf("同一ドメインを誤検知 (mismatches=%v)", res.Details["display_mismatches"])
	}
}

func TestDisplayMismatch_NonDomainTextIgnored(t *testing.T) {
	w := newWorker(nil, nil, scoresWithMismatch())
	// 表示テキストがドメインを含まない（「こちらをクリック」）→ 対象外
	html := `<a href="https://legit.example/x">こちらをクリック</a>`
	res, err := w.Inspect(context.Background(), &domain.Mail{RawEML: buildHTMLEML(html)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Detected {
		t.Errorf("ドメインなし表示テキストを誤検知 (mismatches=%v)", res.Details["display_mismatches"])
	}
}

func TestRegistrable(t *testing.T) {
	cases := map[string]string{
		"www.example.com":  "example.com",
		"example.com":      "example.com",
		"a.b.c.example.co": "example.co",
		"localhost":        "localhost",
	}
	for host, want := range cases {
		if got := registrable(host); got != want {
			t.Errorf("registrable(%q) = %q, want %q", host, got, want)
		}
	}
}
