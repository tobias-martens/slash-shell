package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

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

type fakeSvc struct{}

func (fakeSvc) Unit(n string) (string, bool) {
	switch n {
	case "ssh", "cups", "docker":
		return "system", true
	case "pipewire":
		return "user", true
	}
	return "", false
}
func (fakeSvc) Active(_, n string) bool { return n != "cups" }
func (fakeSvc) Program(n string) bool   { _, ok := probe.FindProgram(fakeSvc{}.Programs(), n); return ok }
func (fakeSvc) Programs() []probe.Program {
	return []probe.Program{{Name: "firefox", PIDs: []int{20, 22}}, {Name: "stubborn", PIDs: []int{30}},
		{Name: "docker", PIDs: []int{40}}}
}
func (fakeSvc) Running(scope string) []string {
	if scope == "user" {
		return []string{"pipewire"}
	}
	return []string{"docker", "ssh"}
}
func (fakeSvc) All(scope string) []string { return append(fakeSvc{}.Running(scope), "cups") }
func (fakeSvc) Scopes() []string          { return []string{"system", "user"} }

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
	a.services = fakeSvc{}
	a.ask = func(string) string { return "" }
	a.env.Exists = func(p string) bool { return p == "/run/systemd/system" }
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

// The install script puts slash in ~/.local/bin, which may not be on PATH
// yet: enable adds it in slash's block, disable takes it away again.
func TestEnableAddsPath(t *testing.T) {
	a, out, _, _ := newTestApp(t)
	a.self = func() (string, error) { return a.home + "/.local/bin/slash", nil }
	a.getenv = func(k string) string {
		return map[string]string{"SHELL": "/bin/zsh", "PATH": "/usr/bin:/bin"}[k]
	}
	if code := a.run([]string{"enable"}); code != 0 {
		t.Fatalf("enable: %d", code)
	}
	data, _ := os.ReadFile(a.home + "/.zshrc")
	if !strings.Contains(string(data), `PATH="$PATH:""$HOME/.local/bin"`) || !strings.Contains(out.String(), "adds ~/.local/bin to PATH") {
		t.Errorf("no PATH line:\n%s\n%s", data, out.String())
	}
	a.run([]string{"disable"})
	if _, err := os.Stat(a.home + "/.zshrc"); !os.IsNotExist(err) {
		t.Error(".zshrc should be gone again")
	}

	// Already on PATH: no PATH line.
	a.getenv = func(k string) string {
		return map[string]string{"SHELL": "/bin/zsh", "PATH": "/usr/bin:" + a.home + "/.local/bin"}[k]
	}
	a.run([]string{"enable"})
	if data, _ := os.ReadFile(a.home + "/.zshrc"); strings.Contains(string(data), "PATH") {
		t.Errorf("PATH line not needed:\n%s", data)
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
		"about, clean, delete, describe, pack, remove, restart, search, show, start, stop, uninstall, unpack, upgrade: slash's, no other program uses them",
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

// fakeProcs: every process ends on TERM except 30 ("stubborn").
type fakeProcs struct {
	signals []string
	gone    map[int]bool
}

func (f *fakeProcs) install(a *app) {
	f.gone = map[int]bool{}
	a.procs = processes{
		kill: func(pid int, sig syscall.Signal) error {
			f.signals = append(f.signals, fmt.Sprintf("%d:%d", pid, sig))
			if pid != 30 || sig == syscall.SIGKILL {
				f.gone[pid] = true
			}
			return nil
		},
		alive: func(pid int) bool { return !f.gone[pid] },
		sleep: func(time.Duration) {},
	}
}

func TestStop(t *testing.T) {
	a, out, errOut, ran := newTestApp(t)
	var f fakeProcs
	f.install(a)

	if code := a.run([]string{"stop", "firefox"}); code != 0 || !strings.Contains(errOut.String(), "→ kill 20 22") {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	if strings.Join(f.signals, " ") != "20:15 22:15" {
		t.Errorf("signals %v", f.signals)
	}

	// Still running after TERM: asks; "no" leaves it running.
	f.signals = nil
	asked := ""
	a.confirm = func(q string) bool { asked = q; return false }
	if code := a.run([]string{"stop", "stubborn"}); code != 1 || asked != "stubborn is still running. Force it to stop? [y/N] " {
		t.Errorf("exit %d, asked %q", code, asked)
	}
	a.confirm = func(string) bool { return true }
	errOut.Reset()
	a.run([]string{"stop", "stubborn"})
	if !strings.Contains(errOut.String(), "→ kill -KILL 30") || f.signals[len(f.signals)-1] != "30:9" {
		t.Errorf("force: %s %v", errOut.String(), f.signals)
	}

	// A service and a program with the same name: asks which.
	*ran = nil
	a.ask = func(string) string { return "s" }
	a.run([]string{"stop", "docker"})
	if len(*ran) != 1 || (*ran)[0] != "sudo systemctl stop docker" {
		t.Errorf("ran %v", *ran)
	}

	// Services only: a plain systemctl command, through layer 1.
	out.Reset()
	if code := a.run([]string{"resolve", "--line", "stop ssh and pipewire"}); code != exitRun ||
		out.String() != "sudo systemctl stop ssh; systemctl --user stop pipewire  #slash: stop ssh and pipewire\n" {
		t.Errorf("exit %d: %q", code, out.String())
	}
	out.Reset()
	a.run([]string{"resolve", "--line", "stop firefox"})
	if out.String() != "slash stop firefox\n" {
		t.Errorf("got %q", out.String())
	}
}

func TestListAndComplete(t *testing.T) {
	a, out, _, _ := newTestApp(t)
	a.run([]string{"stop"})
	want := "Programs:\n  firefox, stubborn, docker\nServices:\n  docker, ssh\nYour services:\n  pipewire\n\nStop one with: stop <name>\n"
	if out.String() != want {
		t.Errorf("got:\n%s", out.String())
	}
	out.Reset()
	a.run([]string{"restart"})
	if strings.Contains(out.String(), "firefox") || !strings.Contains(out.String(), "Restart one with: restart <name>") {
		t.Errorf("restart lists services only:\n%s", out.String())
	}
	out.Reset()
	a.run([]string{"complete", "stop"})
	if out.String() != "docker\nfirefox\npipewire\nssh\nstubborn\n" {
		t.Errorf("complete stop: %q", out.String())
	}
	out.Reset()
	a.run([]string{"complete", "start"})
	if !strings.Contains(out.String(), "cups\n") {
		t.Errorf("complete start: %q", out.String())
	}
}

func TestShow(t *testing.T) {
	a, out, errOut, _ := newTestApp(t, "df", "curl")
	if code := a.run([]string{"resolve", "--line", "show disk space"}); code != exitRun || out.String() != "df -h -x tmpfs -x devtmpfs  #slash: show disk space\n" {
		t.Errorf("exit %d: %q", code, out.String())
	}
	out.Reset()
	if code := a.run([]string{"resolve", "--line", "show public ip"}); code != exitConfirm ||
		!strings.Contains(out.String(), "(asks ifconfig.me over the internet)") {
		t.Errorf("public ip must ask first: exit %d %q", code, out.String())
	}
	if code := a.run([]string{"resolve", "--words", "show", "cats"}); code != exitUserError || !strings.Contains(errOut.String(), "I can show:") {
		t.Errorf("exit %d: %s", code, errOut.String())
	}
}
