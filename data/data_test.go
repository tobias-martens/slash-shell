package data

import (
	"strings"
	"testing"
)

func TestLoadAllSources(t *testing.T) {
	tm, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range []string{"apt", "dnf", "pacman", "zypper", "apk", "flatpak", "snap"} {
		for _, key := range []string{"update", "install", "remove"} {
			if _, ok := tm.Lookup(src, key, ""); !ok {
				t.Errorf("%s has no %q", src, key)
			}
		}
	}
	if got, _ := tm.Lookup("zypper", "update", "opensuse-tumbleweed"); !strings.Contains(got, "dist-upgrade") {
		t.Errorf("tumbleweed update = %q", got)
	}
	if got, _ := tm.Lookup("zypper", "update", "opensuse-leap"); strings.Contains(got, "dist-upgrade") {
		t.Errorf("leap update = %q", got)
	}
}

func TestSafetyRules(t *testing.T) {
	tm, _ := Load()
	// Cleaning must keep package lists, or the offline search loses its data.
	for src, m := range tm {
		if c := m["clean"]; strings.Contains(c, "--all") || strings.Contains(c, "clean all") ||
			strings.Contains(c, "-Scc") || strings.Contains(c, "metadata") {
			t.Errorf("%s clean removes package lists: %q", src, c)
		}
	}
	for key, tmpl := range tm["flatpak"] {
		if strings.Contains(tmpl, "sudo") {
			t.Errorf("flatpak %s uses sudo: %q", key, tmpl)
		}
	}
	for key, tmpl := range tm["pacman"] {
		if strings.Contains(tmpl, "-Sy ") || strings.HasSuffix(tmpl, "-Sy") {
			t.Errorf("pacman %s does a partial upgrade: %q", key, tmpl)
		}
	}
	for src, m := range tm {
		for key, tmpl := range m {
			if strings.Contains(tmpl, " -y") || strings.Contains(tmpl, "--assumeyes") || strings.Contains(tmpl, "--noconfirm") {
				t.Errorf("%s %s skips confirmation: %q", src, key, tmpl)
			}
		}
	}
}

func TestShowTable(t *testing.T) {
	entries, err := LoadShow()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		for _, p := range e.Phrases {
			if seen[p] {
				t.Errorf("phrase %q twice", p)
			}
			seen[p] = true
		}
		// Must run in bash, zsh and fish alike.
		for _, bad := range []string{"$(", "`", "[[", "<(", "${"} {
			if strings.Contains(e.Command, bad) {
				t.Errorf("%q: %q doesn't work in every shell", e.Phrases[0], bad)
			}
		}
		online := strings.Contains(e.Command, "http") || strings.Contains(e.Command, "curl") || strings.Contains(e.Command, "wget")
		if online != (e.Internet != "") {
			t.Errorf("%q: uses the internet without saying so (or says so wrongly)", e.Phrases[0])
		}
		if strings.Contains(e.Command, "sudo") || strings.Contains(e.Command, " -y") {
			t.Errorf("%q: show never needs sudo or -y", e.Phrases[0])
		}
	}
	for _, want := range []string{"disk space", "memory", "logs", "public ip"} {
		if !seen[want] {
			t.Errorf("missing %q", want)
		}
	}
}

func TestArchives(t *testing.T) {
	as, err := LoadArchives()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range as {
		if !strings.Contains(a.Unpack, "{file}") || (a.UnpackInto != "" && !strings.Contains(a.UnpackInto, "{dir}")) {
			t.Errorf("%v: bad unpack template", a.Exts)
		}
		if a.Pack != "" && (!strings.Contains(a.Pack, "{archive}") || !strings.Contains(a.Pack, "{files}")) {
			t.Errorf("%v: bad pack template", a.Exts)
		}
		if strings.HasPrefix(a.Unpack, "tar ") && !strings.Contains(a.Unpack, "k") {
			t.Errorf("%v: tar must keep existing files (-k)", a.Exts)
		}
		for _, bad := range []string{"$(", "`", "rm "} {
			if strings.Contains(a.Unpack+a.UnpackInto+a.Pack, bad) {
				t.Errorf("%v: %q", a.Exts, bad)
			}
		}
	}
}
