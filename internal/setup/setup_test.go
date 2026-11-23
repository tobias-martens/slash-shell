package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(map[string]string) func(string) string {
	return func(string) string { return "" }
}

func roundTrip(t *testing.T, shell string, before *string) {
	t.Helper()
	home := t.TempDir()
	tg, err := TargetFor(shell, home, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if before != nil {
		os.MkdirAll(filepath.Dir(tg.Path), 0o755)
		if err := os.WriteFile(tg.Path, []byte(*before), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s := Store{Dir: filepath.Join(home, ".local/state/slash")}
	changed, err := s.Enable(tg)
	if err != nil || !changed {
		t.Fatalf("enable: changed=%v err=%v", changed, err)
	}
	data, _ := os.ReadFile(tg.Path)
	if !strings.Contains(string(data), "slash init "+shell) {
		t.Fatalf("block missing:\n%s", data)
	}
	if changed, _ := s.Enable(tg); changed {
		t.Error("second enable must not add a second block")
	}
	if notes, err := s.Disable(); err != nil || len(notes) != 0 {
		t.Fatalf("disable: notes=%v err=%v", notes, err)
	}
	after, err := os.ReadFile(tg.Path)
	if before == nil {
		if !os.IsNotExist(err) {
			t.Errorf("file slash created should be gone, got %q", after)
		}
		if shell == "fish" {
			if _, err := os.Stat(filepath.Join(home, ".config")); !os.IsNotExist(err) {
				t.Error("directories slash created should be gone")
			}
		}
		return
	}
	if string(after) != *before {
		t.Errorf("not byte-identical:\nbefore %q\nafter  %q", *before, after)
	}
	if st, _ := os.Stat(tg.Path); st.Mode().Perm() != 0o600 {
		t.Errorf("permissions changed to %v", st.Mode().Perm())
	}
}

func TestRoundTripRestoresExactly(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		for name, content := range map[string]*string{
			"missing":          nil,
			"empty":            ptr(""),
			"normal":           ptr("alias ll='ls -l'\nexport EDITOR=vim\n"),
			"no final newline": ptr("export PATH=$HOME/bin:$PATH"),
			"trailing blanks":  ptr("x=1\n\n\n"),
		} {
			t.Run(shell+"/"+name, func(t *testing.T) { roundTrip(t, shell, content) })
		}
	}
}

func TestDisableAfterUserEdits(t *testing.T) {
	home := t.TempDir()
	tg, _ := TargetFor("bash", home, env(nil))
	os.WriteFile(tg.Path, []byte("a=1\n"), 0o644)
	s := Store{Dir: filepath.Join(home, "state")}
	s.Enable(tg)
	// The user adds a line inside slash's block.
	data, _ := os.ReadFile(tg.Path)
	edited := strings.Replace(string(data), "# <<< slash <<<", "echo mine\n# <<< slash <<<", 1)
	os.WriteFile(tg.Path, []byte(edited+"b=2\n"), 0o644)
	notes, err := s.Disable()
	if err != nil || len(notes) != 1 {
		t.Fatalf("notes=%v err=%v", notes, err)
	}
	after, _ := os.ReadFile(tg.Path)
	if strings.Contains(string(after), "slash") || !strings.Contains(string(after), "a=1") || !strings.Contains(string(after), "b=2") {
		t.Errorf("got %q", after)
	}
}

func TestZdotdir(t *testing.T) {
	tg, _ := TargetFor("zsh", "/home/u", func(k string) string {
		if k == "ZDOTDIR" {
			return "/home/u/.config/zsh"
		}
		return ""
	})
	if tg.Path != "/home/u/.config/zsh/.zshrc" {
		t.Errorf("got %s", tg.Path)
	}
}

func ptr(s string) *string { return &s }
