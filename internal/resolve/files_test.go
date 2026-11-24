package resolve

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/tobias-martens/slash-shell/data"
	"github.com/tobias-martens/slash-shell/internal/grammar"
	"github.com/tobias-martens/slash-shell/internal/sources"
)

func TestFilesAndClean(t *testing.T) {
	tmpl, _ := data.Load()
	archives, err := data.LoadArchives()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]bool{"photos.zip": true, "src.tar.gz": true, "notes.txt": true, "Photos": true,
		"Videos": true, "Photos.tar.gz": false, "big.7z": true, "-odd.zip": true, "a b.txt": true,
		"data.gz": true, "song.mp3": true, "vlc": true, "x.rar": true}
	installed := map[string]bool{"tar": true, "gzip": true, "unzip": true, "zip": true, "gio": true}
	probe := fakeProbe{inst: map[string]string{"apt/firefox": "firefox"}}
	ubuntu := sources.System{Manager: "apt", Flatpak: true, Snap: true}
	tests := []struct {
		sys     sources.System
		line    []string
		want    string
		confirm bool
		errText string
	}{
		{ubuntu, []string{"clean", "up"}, "sudo apt autoremove && sudo apt clean; flatpak uninstall --unused", false, ""},
		{sources.System{Manager: "dnf"}, []string{"clean"}, "sudo dnf autoremove && sudo dnf clean packages", false, ""},
		{ubuntu, []string{"clean", "up", "flatpak"}, "flatpak uninstall --unused", false, ""},
		{ubuntu, []string{"clean", "up", "snap"}, "", false, "don't know how to clean up snap"},
		{sources.System{Manager: "pacman", Immutable: true, Flatpak: true}, []string{"clean", "up"}, "flatpak uninstall --unused", false, ""},

		{ubuntu, []string{"unpack", "photos.zip"}, "unzip photos.zip", false, ""},
		{ubuntu, []string{"unpack", "src.tar.gz", "into", "Old"}, "mkdir -p Old && tar -xkf src.tar.gz -C Old", false, ""},
		{ubuntu, []string{"unpack", "photos.zip", "and", "src.tar.gz"}, "unzip photos.zip && tar -xkf src.tar.gz", false, ""},
		{ubuntu, []string{"unpack", "-odd.zip"}, "unzip ./-odd.zip", false, ""},
		{ubuntu, []string{"unpack", "data.gz", "into", "x"}, "", false, "unpack next to themselves"},
		{ubuntu, []string{"unpack", "big.7z"}, "", false, "needs one of: 7z, bsdtar; none is installed"},
		{ubuntu, []string{"unpack", "song.mp3"}, "", false, "don't know how to unpack"},
		{ubuntu, []string{"unpack", "nope.zip"}, "", false, "no file called"},

		{ubuntu, []string{"pack", "Videos"}, "zip -r Videos.zip Videos", false, ""},
		{ubuntu, []string{"pack", "Videos", "as", "tar.gz"}, "tar -czf Videos.tar.gz Videos", false, ""},
		{ubuntu, []string{"pack", "Photos", "as", "zip"}, "zip -r Photos.zip Photos", false, ""},
		{ubuntu, []string{"pack", "Photos", "and", "Videos", "as", "media.zip"}, "zip -r media.zip Photos Videos", false, ""},
		{ubuntu, []string{"pack", "Photos", "and", "Videos"}, "", false, "what should the archive be called"},
		{ubuntu, []string{"pack", "Photos", "as", "rar"}, "", false, "can't create .rar"},
		{ubuntu, []string{"pack", "Photos", "as", "photos.zip"}, "", false, "photos.zip already exists"},
		{ubuntu, []string{"pack", "Photos", "as", "docx"}, "", false, "I can pack as .tar.gz"},

		{ubuntu, []string{"delete", "notes.txt", "a b.txt"}, "gio trash notes.txt 'a b.txt'", false, ""},
		{ubuntu, []string{"delete", "firefox"}, "", false, "To uninstall the program: remove firefox"},
		{ubuntu, []string{"delete", "nothing"}, "", false, "no file or folder"},
		{ubuntu, []string{"remove", "notes.txt"}, "gio trash notes.txt", true, ""},
		{ubuntu, []string{"remove", "firefox"}, "sudo apt remove firefox", false, ""},
		{ubuntu, []string{"remove", "notes.txt", "firefox"}, "", false, "notes.txt is a file, not a program"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.line, " "), func(t *testing.T) {
			p, err := grammar.Parse(tt.line)
			if err != nil {
				t.Fatal(err)
			}
			r := &Resolver{Sys: tt.sys, Templates: tmpl, Archives: archives, Probe: probe,
				Exists: func(f string) bool { return files[f] },
				LookPath: func(n string) (string, error) {
					if installed[n] {
						return "/usr/bin/" + n, nil
					}
					return "", errors.New("no")
				}}
			res, err := r.Resolve(p)
			if tt.errText != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errText) {
					t.Fatalf("want error with %q, got %v (%q)", tt.errText, err, res.Command)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.Command != tt.want || res.Confirm != tt.confirm {
				t.Errorf("got %q confirm=%v", res.Command, res.Confirm)
			}
		})
	}
}

func TestNeverRm(t *testing.T) {
	r := &Resolver{LookPath: func(string) (string, error) { return "", errors.New("no") }}
	if cmd, own := r.trash([]string{"x"}); !own || cmd != "slash delete x" {
		t.Errorf("without a trash tool slash moves files itself, got %q %v", cmd, own)
	}
}

// Nothing here makes zips: pack makes a tar.gz instead, never asking the
// user to install anything.
func TestPackWithoutZip(t *testing.T) {
	archives, err := data.LoadArchives()
	if err != nil {
		t.Fatal(err)
	}
	r := &Resolver{Archives: archives,
		Exists: func(f string) bool { return f == "Photos" },
		LookPath: func(n string) (string, error) {
			if n == "tar" || n == "gzip" {
				return "/usr/bin/" + n, nil
			}
			return "", errors.New("no")
		}}
	p, _ := grammar.Parse([]string{"pack", "Photos"})
	if res, err := r.Resolve(p); err != nil || res.Command != "tar -czf Photos.tar.gz Photos" {
		t.Errorf("got %q, %v", res.Command, err)
	}
	// Asked for a zip by name: that, or say what would make one.
	p, _ = grammar.Parse([]string{"pack", "Photos", "as", "zip"})
	if _, err := r.Resolve(p); err == nil || !strings.Contains(err.Error(), "needs one of: zip, 7z, bsdtar, python3") {
		t.Errorf("got %v", err)
	}
}

// With only some programs installed, slash uses the alternative that works.
func TestAlternatives(t *testing.T) {
	archives, _ := data.LoadArchives()
	show, _ := data.LoadShow()
	tests := []struct {
		have []string
		line []string
		want string
	}{
		{[]string{"bsdtar"}, []string{"unpack", "photos.zip"}, "bsdtar -xkf photos.zip"},
		{[]string{"7z", "bsdtar"}, []string{"unpack", "photos.zip", "into", "Old"}, "mkdir -p Old && 7z x photos.zip -oOld"},
		{[]string{"bsdtar"}, []string{"pack", "Photos"}, "bsdtar -a -cf Photos.zip Photos"},
		{[]string{"python3", "tar", "gzip"}, []string{"pack", "Photos"}, "python3 -m zipfile -c Photos.zip Photos"},
		{[]string{"tar", "bsdtar"}, []string{"unpack", "src.tar.zst"}, "bsdtar -xkf src.tar.zst"},
		{[]string{"netstat", "grep"}, []string{"show", "what", "uses", "port", "80"}, "netstat -tulpn | grep ':80 '"},
		{[]string{"ss", "netstat"}, []string{"show", "ports"}, "ss -tulpn"},
		{[]string{"wget"}, []string{"show", "public", "ip"}, "wget -qO- https://ifconfig.me; echo"},
	}
	for _, tt := range tests {
		r := &Resolver{Archives: archives, Show: show,
			Exists: func(f string) bool { return f == "Photos" || f == "photos.zip" || f == "src.tar.zst" },
			LookPath: func(n string) (string, error) {
				if slices.Contains(tt.have, n) {
					return "/usr/bin/" + n, nil
				}
				return "", errors.New("no")
			}}
		p, err := grammar.Parse(tt.line)
		if err != nil {
			t.Fatal(err)
		}
		if res, err := r.Resolve(p); err != nil || res.Command != tt.want {
			t.Errorf("%v with %v: got %q, %v; want %q", tt.line, tt.have, res.Command, err, tt.want)
		}
	}
}
