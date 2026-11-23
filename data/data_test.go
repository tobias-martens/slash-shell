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
