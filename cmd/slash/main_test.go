package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/tobias-martens/slash-shell/internal/takeover"
)

type fakeProbe map[string]string // "source/pkg" → name

func (f fakeProbe) Available(src, pkg string) (string, bool) { n, ok := f[src+"/"+pkg]; return n, ok }
func (f fakeProbe) Installed(src, pkg string) (string, bool) { return "", false }

// newTestApp simulates an Ubuntu system with apt, flatpak and coreutils.
func newTestApp(t *testing.T, bins ...string) (*app, *bytes.Buffer, *bytes.Buffer, *[]string) {
	t.Helper()
	var out, errOut bytes.Buffer
	var ran []string
	present := map[string]bool{"apt": true, "flatpak": true, "install": true}
	for _, b := range bins {
		present[b] = true
	}
	a := &app{
		stdout: &out, stderr: &errOut,
		probe:   fakeProbe{"apt/vlc": "vlc", "flatpak/obsidian": "md.obsidian.Obsidian"},
		state:   takeover.State{Dir: t.TempDir() + "/slash"},
		confirm: func(string) bool { return true },
		execute: func(c string) int { ran = append(ran, c); return 0 },
	}
	a.env.LookPath = func(n string) (string, error) {
		if present[n] {
			return "/usr/bin/" + n, nil
		}
		return "", errors.New("not found")
	}
	a.env.ReadFile = func(p string) ([]byte, error) {
		if p == "/etc/os-release" {
			return []byte("ID=ubuntu\n"), nil
		}
		return nil, errors.New("no")
	}
	a.env.Exists = func(string) bool { return false }
	return a, &out, &errOut, &ran
}

func TestBasics(t *testing.T) {
	a, out, errOut, _ := newTestApp(t)
	if a.run(nil) != 0 || !strings.Contains(out.String(), "simple and natural") {
		t.Errorf("intro: %q", out.String())
	}
	if a.run([]string{"frobnicate"}) != exitUserError || !strings.Contains(errOut.String(), `don't know "frobnicate"`) {
		t.Errorf("unknown: %q", errOut.String())
	}
}

func TestResolveLine(t *testing.T) {
	tests := []struct {
		line string
		code int
		want string
	}{
		{"update", exitRun, "sudo apt update && sudo apt upgrade; flatpak update  #slash: update\n"},
		{"install vlc", exitRun, "sudo apt install vlc  #slash: install vlc\n"},
		{"install obsidian", exitConfirm, "flatpak install md.obsidian.Obsidian  #slash: install obsidian (obsidian: not in apt, found in flatpak)\n"},
		{"install -m 755 a b", exitNotHandled, ""},
		{"update firefox", exitNotHandled, ""}, // grammar error: silent here
		{"install foo as snap", exitNotHandled, ""},
		{"ls -la", exitNotHandled, ""},
		{"update && reboot", exitNotHandled, ""},
	}
	for _, tt := range tests {
		a, out, errOut, _ := newTestApp(t)
		if code := a.run([]string{"resolve", "--line", tt.line}); code != tt.code || out.String() != tt.want {
			t.Errorf("%q: code %d, out %q", tt.line, code, out.String())
		}
		if errOut.Len() != 0 {
			t.Errorf("%q: --line must be silent, got %q", tt.line, errOut.String())
		}
	}
}

func TestResolveWords(t *testing.T) {
	a, out, errOut, _ := newTestApp(t)
	if code := a.run([]string{"resolve", "--words", "update", "firefox"}); code != exitUserError || !strings.Contains(errOut.String(), "I can update everything") {
		t.Errorf("code %d, stderr %q", code, errOut.String())
	}
	if code := a.run([]string{"resolve", "--words", "install", "vlc"}); code != exitRun || !strings.HasPrefix(out.String(), "sudo apt install vlc") {
		t.Errorf("code %d, out %q", code, out.String())
	}
}

func TestNewerProgramWins(t *testing.T) {
	a, out, errOut, _ := newTestApp(t, "remove") // a program called "remove", installed after slash
	for i := 0; i < 2; i++ {
		if code := a.run([]string{"resolve", "--words", "remove", "x"}); code != exitNotHandled || out.Len() != 0 {
			t.Errorf("code %d, out %q", code, out.String())
		}
	}
	if n := strings.Count(errOut.String(), "installed after slash"); n != 1 {
		t.Errorf("notice shown %d times: %q", n, errOut.String())
	}
	// Recorded at enable time → slash keeps it.
	a.state.Record([]string{"remove"})
	if code := a.run([]string{"resolve", "--line", "remove vlc"}); code != exitRun {
		t.Errorf("recorded name should be taken over, code %d", code)
	}
}

func TestDirect(t *testing.T) {
	a, _, errOut, ran := newTestApp(t)
	if code := a.run([]string{"install", "obsidian"}); code != 0 {
		t.Fatalf("code %d", code)
	}
	if len(*ran) != 1 || (*ran)[0] != "flatpak install md.obsidian.Obsidian" {
		t.Errorf("ran %q", *ran)
	}
	if !strings.Contains(errOut.String(), "→ flatpak install md.obsidian.Obsidian") {
		t.Errorf("stderr %q", errOut.String())
	}
	a.confirm = func(string) bool { return false }
	*ran = nil
	a.run([]string{"install", "obsidian"})
	if len(*ran) != 0 {
		t.Errorf("declined but ran %q", *ran)
	}
}

func TestResolveWordsHistory(t *testing.T) {
	a, out, _, _ := newTestApp(t)
	code := a.run([]string{"resolve", "--words", "--history", "update && reboot", "update"})
	want := "sudo apt update && sudo apt upgrade; flatpak update  #slash: update\n" +
		"{ sudo apt update && sudo apt upgrade; flatpak update; } && reboot  #slash: update\n"
	if code != exitRun || out.String() != want {
		t.Errorf("code %d, out %q", code, out.String())
	}
}
