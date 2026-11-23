package main

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/tobias-martens/slash-shell/internal/probe"
	"github.com/tobias-martens/slash-shell/internal/takeover"
)

type fakeProbe map[string]string // "source/pkg" → name

func (f fakeProbe) Available(src, pkg string) (string, bool) { n, ok := f[src+"/"+pkg]; return n, ok }
func (f fakeProbe) Installed(src, pkg string) (string, bool) { return "", false }

func (f fakeProbe) Search(src string, words []string, limit int) ([]probe.Match, int, string, error) {
	switch src {
	case "apt":
		ms := []probe.Match{{Name: "gnome-text-editor", Summary: "simple text editor for GNOME", Installed: true},
			{Name: "mu-editor", Summary: "simple editor for beginner Python programmers"}}
		return ms[:min(limit, 2)], 7, "apt-cache search editor", nil
	case "flatpak":
		return nil, 0, "", probe.ErrNoData
	}
	return nil, 0, "", nil
}

func (f fakeProbe) About(src, pkg string) (probe.Info, bool) {
	switch {
	case src == "apt" && pkg == "vlc":
		return probe.Info{Source: "apt", Name: "vlc", Version: "3.0.23-1", Installed: "3.0.23-1",
			Summary: "multimedia player and streamer", Homepage: "https://www.videolan.org/vlc/",
			How: []string{"apt-cache show vlc"}}, true
	case src == "flatpak" && pkg == "vlc":
		return probe.Info{Source: "flatpak", Name: "org.videolan.VLC", Version: "3.0.24",
			Summary: "VLC media player", How: []string{"flatpak's local catalog"}}, true
	}
	return probe.Info{Source: src, How: []string{src + " lookup"}}, false
}

type fakeFiles struct{ home string }

func (f fakeFiles) Find(words []string, limit int) ([]string, int, string) {
	return []string{f.home + "/Documents/editor notes.txt"}, 1, "find " + f.home + " -iname '*editor*'"
}

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
		probe: fakeProbe{"apt/vlc": "vlc", "flatpak/obsidian": "md.obsidian.Obsidian"},
		state: takeover.State{Dir: t.TempDir() + "/slash"},
		home:  t.TempDir(),
		getenv: func(k string) string {
			if k == "SHELL" {
				return "/bin/bash"
			}
			return ""
		},
		confirm: func(string) bool { return true },
		execute: func(c string) int { ran = append(ran, c); return 0 },
	}
	a.files = fakeFiles{a.home}
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

func TestEnableDisable(t *testing.T) {
	a, out, _, _ := newTestApp(t, "remove") // "remove" exists before slash
	rc := a.home + "/.bashrc"
	os.WriteFile(rc, []byte("alias ll='ls -l'\n"), 0o644)
	if code := a.run([]string{"enable"}); code != 0 {
		t.Fatalf("enable: %d", code)
	}
	for _, want := range []string{"enabled for bash", "~/.bashrc", "install  /usr/bin/install keeps working", "remove   /usr/bin/remove, still available as: command remove"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
	// "remove" existed at enable time → taken over.
	if code := a.run([]string{"resolve", "--line", "remove vlc"}); code != exitRun {
		t.Errorf("remove should be taken over, code %d", code)
	}
	if code := a.run([]string{"disable"}); code != 0 {
		t.Fatalf("disable: %d", code)
	}
	if data, _ := os.ReadFile(rc); string(data) != "alias ll='ls -l'\n" {
		t.Errorf("rc not restored: %q", data)
	}
	if _, err := os.Stat(a.state.Dir); !os.IsNotExist(err) {
		t.Error("state dir should be gone")
	}
}

func TestDoctor(t *testing.T) {
	a, out, _, _ := newTestApp(t, "update")
	a.run([]string{"enable", "bash"})
	out.Reset()
	if a.state.Record(nil) != nil { // "update" appeared after slash was enabled
		t.Fatal("record")
	}
	if a.run([]string{"doctor"}) != 0 {
		t.Fatal("doctor failed")
	}
	for _, want := range []string{
		"Sources:   apt (system), flatpak",
		"Installs:  from apt first",
		"bash  enabled in ~/.bashrc",
		"zsh   not enabled (turn on with: slash enable zsh)",
		"install   slash's; /usr/bin/install still handles files",
		"update    /usr/bin/update was installed after slash and wins; slash's: slash update …",
		"about, describe, remove, search, uninstall, upgrade: slash's, no other program uses them",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out.String())
		}
	}
}

func TestSearch(t *testing.T) {
	a, out, _, ran := newTestApp(t)
	a.getenv = func(string) string { return "" }
	if code := a.run([]string{"search", "editor"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	want := `Files  (find ~ -iname '*editor*')
  ~/Documents/editor notes.txt

apt  (apt-cache search editor)
  gnome-text-editor  simple text editor for GNOME  [installed]
  mu-editor          simple editor for beginner Python programmers
  … 5 more: search editor in apt

No package lists downloaded yet for flatpak; run: update
`
	if out.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", out.String(), want)
	}
	if len(*ran) != 0 {
		t.Errorf("search must not run commands through the shell: %v", *ran)
	}
	out.Reset()
	a.run([]string{"search", "editor", "in", "files"})
	if strings.Contains(out.String(), "apt") {
		t.Errorf("in files must only search files:\n%s", out.String())
	}
}

func TestAbout(t *testing.T) {
	a, out, _, _ := newTestApp(t)
	if code := a.run([]string{"about", "vlc"}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	want := `vlc: multimedia player and streamer
Website: https://www.videolan.org/vlc/

  apt      installed  3.0.23-1
  flatpak  available  3.0.24    as org.videolan.VLC

Read from: apt-cache show vlc; flatpak's local catalog
`
	if out.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", out.String(), want)
	}
	out.Reset()
	if code := a.run([]string{"describe", "nothing"}); code != 1 || !strings.Contains(out.String(), `No package called "nothing" here (looked in apt and flatpak). Similar names: search nothing`) {
		t.Errorf("exit %d: %s", code, out.String())
	}
}

func TestResolveOwnVerbs(t *testing.T) {
	a, out, _, _ := newTestApp(t)
	if code := a.run([]string{"resolve", "--line", "search holiday photos"}); code != exitRun || out.String() != "slash search holiday photos\n" {
		t.Errorf("exit %d: %q", code, out.String())
	}
}
