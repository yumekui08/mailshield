package filesep_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jhillyerd/enmime"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/domain"
	"github.com/koizumib/mailshield/services/smtp-gateway/internal/worker/builtin/filesep"
)

// ─── スタブ AttachmentStorage ─────────────────────────────────

type stubStorage struct {
	saved map[string][]byte
}

func newStubStorage() *stubStorage {
	return &stubStorage{saved: make(map[string][]byte)}
}

func (s *stubStorage) SaveAttachment(_ context.Context, messageID, filename string, data []byte) (string, error) {
	key := messageID + "/" + filename
	s.saved[key] = data
	return key, nil
}

func (s *stubStorage) GetPresignedURL(_ context.Context, path string, _ int) (string, error) {
	return "https://minio.example.com/" + path + "?signed=true", nil
}

// ─── スタブ MailRepository ────────────────────────────────────

type stubRepository struct {
	savedAttachments []*domain.MailAttachment
}

func (r *stubRepository) SaveMessage(_ context.Context, _ *domain.Mail) error { return nil }
func (r *stubRepository) UpdateMessageStatus(_ context.Context, _ string, _ domain.MessageStatus) error {
	return nil
}
func (r *stubRepository) SaveInspectResult(_ context.Context, _ *domain.InspectResult, _ string) error {
	return nil
}
func (r *stubRepository) SaveAttachment(_ context.Context, att *domain.MailAttachment) error {
	r.savedAttachments = append(r.savedAttachments, att)
	return nil
}
func (r *stubRepository) UpdateProcessedEMLPath(_ context.Context, _, _ string) error {
	return nil
}

// ─── ヘルパー ─────────────────────────────────────────────────

func buildTestEML(boundary, body, filename string) []byte {
	var buf bytes.Buffer
	buf.WriteString("From: sender@example.com\r\n")
	buf.WriteString("To: recipient@example.com\r\n")
	buf.WriteString("Subject: Test Mail\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n")
	buf.WriteString("\r\n")
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	buf.WriteString("\r\n")
	buf.WriteString(body + "\r\n")
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Type: application/octet-stream\r\n")
	buf.WriteString("Content-Disposition: attachment; filename=\"" + filename + "\"\r\n")
	buf.WriteString("Content-Transfer-Encoding: base64\r\n")
	buf.WriteString("\r\n")
	buf.WriteString("dGVzdGNvbnRlbnQ=\r\n") // "testcontent"
	buf.WriteString("--" + boundary + "--\r\n")
	return buf.Bytes()
}

func makeMail(eml []byte) *domain.Mail {
	return &domain.Mail{
		MessageID:   "msg-001",
		FromAddress: "sender@example.com",
		ToAddresses: []string{"recipient@example.com"},
		Subject:     "Test Mail",
		RawEML:      eml,
		ReceivedAt:  time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
	}
}

func writeConfigFile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(dir+"/filesep-worker.yaml", []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// ─── テスト ──────────────────────────────────────────────────

func TestFileSepWorker_Name(t *testing.T) {
	w, err := filesep.New(t.TempDir(), newStubStorage(), &stubRepository{}, "localhost", 10026, nil)
	if err != nil {
		t.Fatal(err)
	}
	if w.Name() != "filesep-worker" {
		t.Errorf("Name() = %q", w.Name())
	}
}

func TestFileSepWorker_NoAttachment_ReturnsSameMail(t *testing.T) {
	eml := []byte("From: a@b.com\r\nSubject: test\r\n\r\nBody text")
	mail := makeMail(eml)

	w, err := filesep.New(t.TempDir(), newStubStorage(), &stubRepository{}, "localhost", 10026, nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := w.Transform(context.Background(), mail)
	if err != nil {
		t.Fatal(err)
	}
	if result != mail {
		t.Error("添付なしの場合は元の Mail ポインタをそのまま返すべき")
	}
}

func TestFileSepWorker_InlineMode_SavesAndInsertsURL(t *testing.T) {
	configDir := t.TempDir()
	writeConfigFile(t, configDir, `
mode: inline
link_expiry_hours: 72
frontend_url: https://mail.example.com
`)
	eml := buildTestEML("BOUND001", "Hello world", "document.pdf")
	mail := makeMail(eml)

	stor := newStubStorage()
	repo := &stubRepository{}
	w, err := filesep.New(configDir, stor, repo, "localhost", 10026, nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := w.Transform(context.Background(), mail)
	if err != nil {
		t.Fatal(err)
	}

	// MinIO に保存されていること
	if len(stor.saved) == 0 {
		t.Fatal("添付ファイルが MinIO に保存されていない")
	}

	// mail_attachments に DB 記録されていること
	if len(repo.savedAttachments) == 0 {
		t.Fatal("添付ファイルが DB に記録されていない")
	}
	att := repo.savedAttachments[0]
	if att.DownloadToken == "" {
		t.Error("download_token が空")
	}

	// 変換後 EML をパースしてダウンロードリンクがテキスト本文に含まれること
	resultEnv, err := enmime.ReadEnvelope(bytes.NewReader(result.RawEML))
	if err != nil {
		t.Fatalf("変換後 EML のパース失敗: %v", err)
	}
	expectedURL := "https://mail.example.com/files/" + att.DownloadToken
	if !strings.Contains(resultEnv.Text, expectedURL) {
		t.Errorf("変換後のテキスト本文にダウンロードリンクが含まれていない\nexpected URL: %s\nbody: %s",
			expectedURL, resultEnv.Text)
	}

	// 分離後は HasAttachment が false になること
	if result.HasAttachment {
		t.Error("分離後は HasAttachment = false になるべき")
	}

	// 元の RawEML は変更されていないこと（不変性）
	if bytes.Equal(result.RawEML, mail.RawEML) {
		t.Error("変換後の RawEML が元と同じ（変換されていない）")
	}
}

func TestFileSepWorker_ExtensionFilter_NotMatched(t *testing.T) {
	configDir := t.TempDir()
	writeConfigFile(t, configDir, `
mode: inline
extensions:
  - .txt
link_expiry_hours: 24
`)

	stor := newStubStorage()
	w, err := filesep.New(configDir, stor, &stubRepository{}, "localhost", 10026, nil)
	if err != nil {
		t.Fatal(err)
	}

	// .pdf は extensions=[.txt] に含まれないので分離されない
	eml := buildTestEML("BOUND002", "Hello", "report.pdf")
	mail := makeMail(eml)

	result, err := w.Transform(context.Background(), mail)
	if err != nil {
		t.Fatal(err)
	}
	if len(stor.saved) != 0 {
		t.Error(".pdf は分離対象外なので MinIO に保存されないはず")
	}
	if result != mail {
		t.Error("分離対象外なら元の Mail ポインタを返すべき")
	}
}

func TestFileSepWorker_ExtensionFilter_Matched(t *testing.T) {
	configDir := t.TempDir()
	writeConfigFile(t, configDir, `
mode: inline
extensions:
  - .pdf
link_expiry_hours: 24
`)

	stor := newStubStorage()
	w, err := filesep.New(configDir, stor, &stubRepository{}, "localhost", 10026, nil)
	if err != nil {
		t.Fatal(err)
	}

	eml := buildTestEML("BOUND003", "Hello", "report.pdf")
	mail := makeMail(eml)

	result, err := w.Transform(context.Background(), mail)
	if err != nil {
		t.Fatal(err)
	}
	if len(stor.saved) == 0 {
		t.Error(".pdf は分離対象なので MinIO に保存されるはず")
	}
	if result == mail {
		t.Error("分離対象の場合は新しい Mail を返すべき")
	}
}

func TestFileSepWorker_SizeFilter_BelowMin(t *testing.T) {
	configDir := t.TempDir()
	writeConfigFile(t, configDir, `
mode: inline
min_size_bytes: 999999
link_expiry_hours: 24
`)

	stor := newStubStorage()
	w, err := filesep.New(configDir, stor, &stubRepository{}, "localhost", 10026, nil)
	if err != nil {
		t.Fatal(err)
	}

	// "testcontent" は11バイト → min_size_bytes=999999 未満なので分離されない
	eml := buildTestEML("BOUND004", "Hello", "small.pdf")
	mail := makeMail(eml)

	result, err := w.Transform(context.Background(), mail)
	if err != nil {
		t.Fatal(err)
	}
	if len(stor.saved) != 0 {
		t.Error("min_size_bytes 未満のファイルは保存されないはず")
	}
	if result != mail {
		t.Error("フィルタ対象外なら元の Mail を返すべき")
	}
}
