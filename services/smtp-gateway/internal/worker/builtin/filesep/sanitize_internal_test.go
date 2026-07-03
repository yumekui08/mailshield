package filesep

import "testing"

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"通常のファイル名はそのまま", "report.pdf", "report.pdf"},
		{"日本語ファイル名はそのまま", "見積書.xlsx", "見積書.xlsx"},
		{"パス区切りを含む名前は置換される", "dir/evil.txt", "dir_evil.txt"},
		{"親ディレクトリ参照は無効化される", "../../../etc/passwd", ".._.._.._etc_passwd"},
		{"バックスラッシュも置換される", `..\..\evil.exe`, ".._.._evil.exe"},
		{"制御文字は除去される", "a\x00b\x1fc.txt", "abc.txt"},
		{"ドットのみは空になる", "..", ""},
		{"空文字列は空のまま", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeFilename(tt.in); got != tt.want {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
