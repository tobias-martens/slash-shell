package resolve

import (
	"errors"
	"strings"
	"testing"

	"github.com/tobias-martens/slash-shell/data"
	"github.com/tobias-martens/slash-shell/internal/grammar"
	"github.com/tobias-martens/slash-shell/internal/sources"
)

// fakeProbe: avail/inst map "source/pkg" → name there.
type fakeProbe struct{ avail, inst map[string]string }

func (f fakeProbe) Available(src, pkg string) (string, bool) {
	n, ok := f.avail[src+"/"+pkg]
	return n, ok
}
func (f fakeProbe) Installed(src, pkg string) (string, bool) {
	n, ok := f.inst[src+"/"+pkg]
	return n, ok
}

var (
	ubuntu  = sources.System{Manager: "apt", Flatpak: true, Snap: true, OSID: "ubuntu"}
	fedora  = sources.System{Manager: "dnf", Flatpak: true, OSID: "fedora"}
	arch    = sources.System{Manager: "pacman", OSID: "arch"}
	tumble  = sources.System{Manager: "zypper", OSID: "opensuse-tumbleweed"}
	steamos = sources.System{Manager: "pacman", Immutable: true, Flatpak: true, OSID: "steamos"}
)

func TestResolve(t *testing.T) {
	tmpl, err := data.Load()
	if err != nil {
		t.Fatal(err)
	}
	probe := fakeProbe{
		avail: map[string]string{
			"apt/vlc": "vlc", "apt/gimp": "gimp", "apt/firefox": "firefox",
			"flatpak/firefox": "org.mozilla.firefox", "flatpak/obsidian": "md.obsidian.Obsidian",
			"snap/obsidian": "obsidian", "snap/spotify": "spotify",
			"dnf/vlc": "vlc", "flatpak/vlc": "org.videolan.VLC",
		},
		inst: map[string]string{
			"snap/spotify": "spotify", "apt/gimp": "gimp",
			"apt/vlc": "vlc", "flatpak/vlc": "org.videolan.VLC",
		},
	}
	tests := []struct {
		name    string
		sys     sources.System
		root    bool
		line    string
		want    string
		confirm bool
		errText string
	}{
		{name: "ubuntu update everything", sys: ubuntu, line: "update",
			want: "sudo apt update && sudo apt upgrade; flatpak update; sudo snap refresh"},
		{name: "as root no sudo", sys: ubuntu, root: true, line: "update",
			want: "apt update && apt upgrade; flatpak update; snap refresh"},
		{name: "update one source", sys: ubuntu, line: "update only flatpak", want: "flatpak update"},
		{name: "update system word", sys: fedora, line: "update system", want: "sudo dnf upgrade"},
		{name: "arch full upgrade", sys: arch, line: "upgrade", want: "sudo pacman -Syu"},
		{name: "tumbleweed dup", sys: tumble, line: "update", want: "sudo zypper dist-upgrade"},
		{name: "steamos skips base", sys: steamos, line: "update", want: "flatpak update"},
		{name: "steamos base update refused", sys: steamos, line: "update pacman", errText: "own updater"},
		{name: "install from system", sys: ubuntu, line: "install vlc and gimp", want: "sudo apt install vlc gimp"},
		{name: "install explicit flatpak uses app id", sys: ubuntu, line: "install firefox as flatpak",
			want: "flatpak install org.mozilla.firefox"},
		{name: "install falls back to flatpak, confirm", sys: ubuntu, line: "install obsidian",
			want: "flatpak install md.obsidian.Obsidian", confirm: true},
		{name: "install mixed sources", sys: ubuntu, line: "install vlc, obsidian",
			want: "sudo apt install vlc; flatpak install md.obsidian.Obsidian", confirm: true},
		{name: "install unknown lets apt report it", sys: ubuntu, line: "install nosuchthing",
			want: "sudo apt install nosuchthing"},
		{name: "steamos installs flatpak by default", sys: steamos, line: "install firefox",
			want: "flatpak install org.mozilla.firefox"},
		{name: "source not on system", sys: fedora, line: "install foo as snap", errText: "snap isn't installed"},
		{name: "wrong package manager", sys: fedora, line: "install foo from apt", errText: "this system uses dnf"},
		{name: "remove finds snap", sys: ubuntu, line: "remove spotify", want: "sudo snap remove spotify"},
		{name: "remove from system", sys: ubuntu, line: "uninstall gimp", want: "sudo apt remove gimp"},
		{name: "remove installed twice asks", sys: ubuntu, line: "remove vlc",
			want: "sudo apt remove vlc", confirm: true},
		{name: "remove explicit flatpak", sys: ubuntu, line: "remove vlc from flatpak",
			want: "flatpak uninstall org.videolan.VLC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := grammar.ParseLine(tt.line)
			if err != nil {
				t.Fatal(err)
			}
			r := &Resolver{Sys: tt.sys, Root: tt.root, Probe: probe, Templates: tmpl}
			res, err := r.Resolve(p)
			if tt.errText != "" {
				var ue *UserError
				if !errors.As(err, &ue) || !contains(ue.Msg, tt.errText) {
					t.Fatalf("want user error containing %q, got %v", tt.errText, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.Command != tt.want || res.Confirm != tt.confirm {
				t.Errorf("got %q confirm=%v (reason %q)", res.Command, res.Confirm, res.Reason)
			}
		})
	}
}

func TestLine(t *testing.T) {
	p, _ := grammar.ParseLine("install obsidian")
	r := Result{Command: "flatpak install md.obsidian.Obsidian", Reason: "obsidian: not in apt, found in flatpak", Phrase: p}
	want := "flatpak install md.obsidian.Obsidian  #slash: install obsidian (obsidian: not in apt, found in flatpak)"
	if r.Line() != want {
		t.Errorf("got %q", r.Line())
	}
}

func TestQuote(t *testing.T) {
	for in, want := range map[string]string{
		"vlc": "vlc", "g++": "g++", "python3.12": "python3.12",
		"my app": "'my app'", "it's": `'it'\''s'`, "$(rm)": "'$(rm)'",
	} {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %q, want %q", in, got, want)
		}
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
