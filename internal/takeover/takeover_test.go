package takeover

import (
	"errors"
	"strings"
	"testing"
)

func TestDecide(t *testing.T) {
	bins := map[string]string{"install": "/usr/bin/install", "about": "/usr/bin/about", "search": "/usr/bin/search"}
	files := map[string]bool{"script.sh": true}
	d := Decider{
		LookPath: func(n string) (string, error) {
			if p, ok := bins[n]; ok {
				return p, nil
			}
			return "", errors.New("not found")
		},
		Exists:   func(p string) bool { return files[p] },
		Recorded: []string{"search"},
	}
	tests := []struct {
		line   string
		handle bool
		newer  string
	}{
		// coreutils install keeps every call it could run
		{"install", false, ""},
		{"install -m 755 script.sh ~/bin/", false, ""},
		{"install -d /opt/foo", false, ""},
		{"install script.sh bin", false, ""},
		{"install a b/", false, ""},
		{"install firefox", true, ""},
		{"install script.sh", true, ""}, // one operand always fails for coreutils
		{"install firefox vlc", true, ""},
		{"install firefox as flatpak", true, ""},
		// no other command with that name
		{"update", true, ""},
		// existed at enable time → taken over
		{"search foo", true, ""},
		// installed after slash → it wins
		{"about firefox", false, "/usr/bin/about"},
	}
	for _, tt := range tests {
		f := strings.Fields(tt.line)
		got := d.Decide(f[0], f[1:])
		if got.Handle != tt.handle || got.Newer != tt.newer {
			t.Errorf("%q: got %+v", tt.line, got)
		}
	}
}

func TestState(t *testing.T) {
	s := State{Dir: t.TempDir() + "/slash"}
	if s.Enabled() || len(s.Recorded()) != 0 {
		t.Fatal("fresh state should be empty")
	}
	if err := s.Record([]string{"install", "search"}); err != nil {
		t.Fatal(err)
	}
	if !s.Enabled() || strings.Join(s.Recorded(), ",") != "install,search" {
		t.Errorf("recorded = %v", s.Recorded())
	}
	if !s.NotifyOnce("about") || s.NotifyOnce("about") {
		t.Error("notice should be shown exactly once")
	}
	s.Record([]string{"install"})
	if !s.NotifyOnce("about") {
		t.Error("re-enabling should reset notices")
	}
}
