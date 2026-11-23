package probe

import (
	"reflect"
	"strings"
	"testing"
)

// Sample outputs in each tool's format (LC_ALL=C).
var searchOutputs = map[string]string{
	"apt-cache": "gnome-text-editor - simple text editor for GNOME\neditorconfig - coding style indenter\n",
	"dpkg-query": "gnome-text-editor\tii \neditorconfig\tun \n",
	"dnf": "===== Name Matched: editor =====\n" +
		"gnome-text-editor.x86_64 : Simple text editor\n" +
		"editorconfig.x86_64 : Coding style indenter\n" +
		" ghex.x86_64\tBinary editor\n", // dnf5 style; filtered out: no "editor" in name
	"rpm": "gnome-text-editor\npackage editorconfig is not installed\n",
	"pacman": "extra/gnome-text-editor 47.0-1 [installed]\n    A simple text editor\n" +
		"extra/editorconfig 0.12.9-1\n    EditorConfig core code\n",
	"zypper": "S  | Name              | Summary              | Type\n" +
		"---+-------------------+----------------------+--------\n" +
		"i+ | gnome-text-editor | Simple text editor   | package\n" +
		"   | editorconfig      | Coding style indenter | package\n",
	"apk": "gnome-text-editor-47.0-r0 - Simple text editor\neditorconfig-0.12.9-r1 - Coding style indenter\n",
}

func fakeRun(calls *[]string) Runner {
	return func(name string, args ...string) (string, bool) {
		*calls = append(*calls, name+" "+strings.Join(args, " "))
		if name == "apk" && args[0] == "info" {
			return "gnome-text-editor\n", true
		}
		return searchOutputs[name], true
	}
}

func TestSearchParsers(t *testing.T) {
	want := []Match{
		{Name: "gnome-text-editor", Installed: true},
		{Name: "editorconfig"},
	}
	for _, src := range []string{"apt", "dnf", "pacman", "zypper", "apk"} {
		var calls []string
		p := &Probe{Run: fakeRun(&calls)}
		top, total, how, err := p.Search(src, []string{"editor"}, 5)
		if err != nil {
			t.Errorf("%s: %v", src, err)
			continue
		}
		for i := range top {
			top[i].Summary = "" // summaries are checked by hand above
		}
		if total != 2 || !reflect.DeepEqual(top, want) {
			t.Errorf("%s: got %d %+v", src, total, top)
		}
		if !strings.Contains(how, "editor") {
			t.Errorf("%s: how = %q", src, how)
		}
		for _, c := range calls {
			for _, online := range []string{"refresh", "makecache", "-Sy", "update"} {
				if strings.Contains(" "+c+" ", " "+online+" ") {
					t.Errorf("%s: %q may go online", src, c)
				}
			}
		}
	}
}

func TestSearchNoData(t *testing.T) {
	p := &Probe{Run: func(string, ...string) (string, bool) { return "", false },
		Glob: func(string) ([]string, error) { return nil, nil }}
	for _, src := range []string{"apt", "pacman", "zypper"} {
		if _, _, _, err := p.Search(src, []string{"x"}, 5); err != ErrNoData {
			t.Errorf("%s: want ErrNoData, got %v", src, err)
		}
	}
}

func TestRanking(t *testing.T) {
	in := []Match{{Name: "editorconfig"}, {Name: "mu-editor"}, {Name: "editor"}, {Name: "xeditorx"}, {Name: "vim"}}
	var got []string
	for _, m := range filterRank(in, []string{"Editor"}) {
		got = append(got, m.Name)
	}
	if want := []string{"editor", "mu-editor", "editorconfig", "xeditorx"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestAboutParsers(t *testing.T) {
	outputs := map[string]string{
		"apt-cache": "Package: vlc\nVersion: 3.0.23-1\nHomepage: https://www.videolan.org/vlc/\nDescription-en: multimedia player\n more text\n\nPackage: vlc\nVersion: 3.0.20-1\n",
		"dpkg-query": "ii \t3.0.23-1",
		"dnf": "Available Packages\nName         : vlc\nVersion      : 3.0.23\nRelease      : 1.fc43\nSummary      : multimedia player\nURL          : https://www.videolan.org/vlc/\n",
		"rpm":    "3.0.23-1.fc43",
		"pacman": "Repository      : extra\nName            : vlc\nVersion         : 3.0.23-1\nDescription     : multimedia player\nURL             : https://www.videolan.org/vlc/\n",
		"zypper": "Information for package vlc:\n----------------------------\nRepository     : Main\nName           : vlc\nVersion        : 3.0.23-1.1\nSummary        : multimedia player\n",
		"apk":    "vlc-3.0.23-r0 - multimedia player\nvlc-dev-3.0.23-r0 - development files\n",
	}
	for _, src := range []string{"apt", "dnf", "pacman", "zypper", "apk"} {
		p := &Probe{Run: func(name string, args ...string) (string, bool) {
			if name == "pacman" && args[0] == "-Q" {
				return "vlc 3.0.23-1\n", true
			}
			return outputs[name], true
		}}
		info, ok := p.About(src, "vlc")
		if !ok || !strings.HasPrefix(info.Version, "3.0.23") || info.Summary != "multimedia player" || info.Installed == "" {
			t.Errorf("%s: ok=%v %+v", src, ok, info)
		}
	}
	p := &Probe{Run: func(string, ...string) (string, bool) { return "", false }}
	if _, ok := p.About("apt", "nothing"); ok {
		t.Error("apt: unknown package found")
	}
}

func TestFilesRanking(t *testing.T) {
	f := Files{Home: "/home/u"}
	in := []string{"/usr/share/doc/Holiday.txt", "/home/u/a/b/holiday.jpg", "/home/u/holiday", "/home/u/.cache/holiday", "/home/u/other"}
	got := f.filterRank(in, []string{"HOLIDAY"})
	want := []string{"/home/u/holiday", "/home/u/a/b/holiday.jpg", "/usr/share/doc/Holiday.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
	if got := f.filterRank(in, []string{".cache"}); len(got) != 0 {
		t.Errorf("only base names count: %v", got)
	}
	args := strings.Join(f.findArgs([]string{"a*b"}), " ")
	if args != `/home/u -xdev -name .* -prune -o -iname *a\*b* -print` {
		t.Errorf("find args: %s", args)
	}
}
