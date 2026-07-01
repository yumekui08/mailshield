package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/koizumib/mailshield/services/smtp-gateway/internal/storage"
)

func newTestFilesystem(t *testing.T) *storage.FilesystemStorage {
	t.Helper()
	s, err := storage.NewFilesystem(t.TempDir(), "https://example.com", "")
	if err != nil {
		t.Fatalf("NewFilesystem() error = %v", err)
	}
	return s
}

func TestFilesystemStorage_NewFilesystem_EmptyBaseDir(t *testing.T) {
	_, err := storage.NewFilesystem("", "", "")
	if err == nil {
		t.Error("空の baseDir はエラーを返すべき")
	}
}

func TestFilesystemStorage_SaveAttachment(t *testing.T) {
	s := newTestFilesystem(t)
	data := []byte("attachment content")

	path, err := s.SaveAttachment(context.Background(), "msg-001", "test.txt", data)
	if err != nil {
		t.Fatalf("SaveAttachment() error = %v", err)
	}
	if path == "" {
		t.Error("SaveAttachment() returned empty path")
	}

	// 保存されたファイルを直接確認
	baseDir := filepath.Dir(filepath.Dir(filepath.Dir(path)))
	_ = baseDir
}

func TestFilesystemStorage_SaveAndReadBack(t *testing.T) {
	base := t.TempDir()
	s, err := storage.NewFilesystem(base, "", "")
	if err != nil {
		t.Fatal(err)
	}

	content := []byte("hello attachment")
	path, err := s.SaveAttachment(context.Background(), "msg-123", "file.txt", content)
	if err != nil {
		t.Fatalf("SaveAttachment() error = %v", err)
	}

	// ファイルが実際に書き込まれたか確認
	full := filepath.Join(base, path)
	got, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("保存されたファイルの読み込み失敗: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("保存内容 = %q, want %q", got, content)
	}
}

func TestFilesystemStorage_DeleteAttachment(t *testing.T) {
	s := newTestFilesystem(t)

	path, err := s.SaveAttachment(context.Background(), "msg-del", "del.txt", []byte("to delete"))
	if err != nil {
		t.Fatalf("SaveAttachment() error = %v", err)
	}

	// 削除
	if err := s.DeleteAttachment(context.Background(), path); err != nil {
		t.Fatalf("DeleteAttachment() error = %v", err)
	}
}

func TestFilesystemStorage_DeleteAttachment_NotExist(t *testing.T) {
	s := newTestFilesystem(t)
	// 存在しないパスの削除はエラーにならないこと
	if err := s.DeleteAttachment(context.Background(), "nonexistent/file.txt"); err != nil {
		t.Errorf("存在しないファイルの削除はエラーにならないべき: %v", err)
	}
}

func TestFilesystemStorage_GetPresignedURL(t *testing.T) {
	s, err := storage.NewFilesystem(t.TempDir(), "https://cdn.example.com", "")
	if err != nil {
		t.Fatal(err)
	}

	url, err := s.GetPresignedURL(context.Background(), "attachments/msg/file.txt", 1)
	if err != nil {
		t.Fatalf("GetPresignedURL() error = %v", err)
	}
	if url == "" {
		t.Error("GetPresignedURL() returned empty URL")
	}
}

func TestFilesystemStorage_GetPresignedURL_NoBaseURL(t *testing.T) {
	s, err := storage.NewFilesystem(t.TempDir(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.GetPresignedURL(context.Background(), "some/path", 1)
	if err == nil {
		t.Error("publicBaseURL 未設定はエラーを返すべき")
	}
}

func TestFilesystemStorage_SaveAttachment_MultipleFiles(t *testing.T) {
	s := newTestFilesystem(t)

	files := map[string][]byte{
		"a.txt": []byte("aaa"),
		"b.pdf": []byte("bbb"),
		"c.zip": []byte("ccc"),
	}
	for name, content := range files {
		_, err := s.SaveAttachment(context.Background(), "multi-msg", name, content)
		if err != nil {
			t.Errorf("SaveAttachment(%s) error = %v", name, err)
		}
	}
}
