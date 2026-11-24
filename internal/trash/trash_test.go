package trash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPut(t *testing.T) {
	home := t.TempDir()
	dir := Dir(home, "")
	now := time.Date(2026, 11, 24, 18, 5, 0, 0, time.Local)
	for i := range 3 {
		p := filepath.Join(home, "my notes.txt")
		os.WriteFile(p, []byte{byte('a' + i)}, 0o644)
		got, err := Put(dir, p, now)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"my notes.txt", "my notes.2.txt", "my notes.3.txt"}[i]
		if filepath.Base(got) != want {
			t.Errorf("trashed as %s, want %s", filepath.Base(got), want)
		}
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Error("file still there")
		}
		info, _ := os.ReadFile(filepath.Join(dir, "info", want+".trashinfo"))
		wantInfo := "[Trash Info]\nPath=" + strings.ReplaceAll(home, " ", "%20") + "/my%20notes.txt\nDeletionDate=2026-11-24T18:05:00\n"
		if string(info) != wantInfo {
			t.Errorf("info file:\n%s\nwant:\n%s", info, wantInfo)
		}
	}

	// Folders too.
	os.MkdirAll(filepath.Join(home, "Old", "x"), 0o755)
	if _, err := Put(dir, filepath.Join(home, "Old"), now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "files", "Old", "x")); err != nil {
		t.Error("folder not in the trash")
	}

	// Never the trash itself, or a folder holding it.
	for _, p := range []string{dir, home, filepath.Join(home, ".local")} {
		os.MkdirAll(p, 0o755)
		if _, err := Put(dir, p, now); err == nil {
			t.Errorf("%s: moved the trash into itself", p)
		}
	}
	if _, err := Put(dir, filepath.Join(home, "nothing"), now); err == nil {
		t.Error("missing file: want an error")
	}
}
