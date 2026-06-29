//go:build e2e

// smtp_test.go は実際の SMTP 送信（Postfix port 25 経由）から Mailpit 到達まで
// のエンドツーエンドフローをテストする。
//
// 前提:
//   - `make dev-up` で docker-compose dev プロファイルが起動済み
//   - Postfix が localhost:25 で待ち受け中（relay_domains に internal.test が含まれる）
//   - smtp-gateway が Mailpit に deliver する設定
//
// 実行方法:
//
//	cd tests/e2e && go test -v -tags e2e -run TestSMTP ./...
package e2e_test

import (
	"fmt"
	"testing"
	"time"
)

// TestSMTP_InboundNormal_ArrivesInMailpit は通常の受信メールが
// パイプラインを通過して Mailpit に届くことを確認する。
func TestSMTP_InboundNormal_ArrivesInMailpit(t *testing.T) {
	requireMailpit(t)

	clearMailpit(t)
	subject := fmt.Sprintf("e2e-normal-%s", randomID())

	err := sendSMTP("sender@external.test", "user@internal.test", subject, "通常のテストメールです。")
	if err != nil {
		t.Skipf("SMTP 送信失敗（Postfix が起動していない可能性）: %v", err)
	}

	msg := waitForMailpit(t, subject, 15*time.Second)
	if msg == nil {
		t.Errorf("件名 %q のメールが 15 秒以内に Mailpit に届きませんでした", subject)
	}
}

// TestSMTP_InboundVirusSubject_SubjectPrefixed は件名に "virus" を含む受信メールが
// subject-virus-transformer によって "[迷惑メール注意] " プレフィックス付きで
// Mailpit に届くことを確認する。
func TestSMTP_InboundVirusSubject_SubjectPrefixed(t *testing.T) {
	requireMailpit(t)

	clearMailpit(t)
	id := randomID()
	origSubject := fmt.Sprintf("virus test e2e-%s", id)
	expectedSubject := fmt.Sprintf("[迷惑メール注意] %s", origSubject)

	err := sendSMTP("sender@external.test", "user@internal.test", origSubject, "件名に virus が含まれるテストメールです。")
	if err != nil {
		t.Skipf("SMTP 送信失敗: %v", err)
	}

	msg := waitForMailpit(t, expectedSubject, 15*time.Second)
	if msg == nil {
		t.Errorf("変換後件名 %q のメールが Mailpit に届きませんでした", expectedSubject)
	}
}

// TestSMTP_InboundEICAR_Quarantined は EICAR テスト文字列を含むメールが
// 隔離されて Mailpit に届かないことを確認する。
// ClamAV が起動していない場合は simulate で事前確認してスキップする。
func TestSMTP_InboundEICAR_Quarantined(t *testing.T) {
	requireGateway(t)
	requireMailpit(t)

	// simulate で ClamAV が動いているか確認
	r := simulateFile(t, "testdata/emls/inbound-eicar.eml")
	ir := findWorkerResult(r, "av-worker")
	if ir == nil || !ir.Detected {
		t.Skip("シミュレーターで av-worker が EICAR を検知しませんでした（ClamAV が起動していない可能性）")
	}

	clearMailpit(t)
	subject := fmt.Sprintf("eicar-quarantine-%s", randomID())

	const eicar = "X5O!P%@AP[4\\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*"
	err := sendSMTP("sender@external.test", "user@internal.test", subject, eicar)
	if err != nil {
		t.Skipf("SMTP 送信失敗: %v", err)
	}

	// 10 秒待ってから「届いていない」ことを確認
	time.Sleep(10 * time.Second)
	msg := waitForMailpit(t, subject, 1*time.Second)
	if msg != nil {
		t.Error("EICAR メールは隔離されるべきですが Mailpit に届いています")
	}
}
