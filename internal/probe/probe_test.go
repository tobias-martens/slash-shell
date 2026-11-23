package probe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const appstream = `<?xml version="1.0" encoding="UTF-8"?>
<components version="0.8" origin="flatpak">
  <component type="desktop-application">
    <id>org.mozilla.firefox.desktop</id>
    <name>Firefox</name>
    <name xml:lang="de">Feuerfuchs</name>
  </component>
  <component type="runtime">
    <id>org.gnome.Platform</id>
    <name>GNOME Platform</name>
  </component>
  <component type="desktop-application">
    <id>md.obsidian.Obsidian</id>
    <name>Obsidian &amp; Co</name>
  </component>
  <custom>x</custom></component><component type="desktop">
    <id>org.videolan.VLC</id>
    <name>VLC</name>
  </component>
  <component type="console-application">
    <id>io.github.cli.Tool</id>
    <name>Tool</name>
  </component>
</components>
`

func writeCatalog(t *testing.T) (glob string) {
	dir := filepath.Join(t.TempDir(), "flathub", "x86_64", "active")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "appstream.xml"), []byte(appstream), 0o644); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(dir))), "*", "*", "active", "appstream.xml")
}

func TestFlatpakFind(t *testing.T) {
	cacheDir := t.TempDir()
	c := &FlatpakCatalog{Globs: []string{writeCatalog(t)}, CacheDir: cacheDir}
	for _, round := range []string{"fresh", "cached"} {
		for pkg, want := range map[string]string{
			"firefox": "org.mozilla.firefox", "FIREFOX": "org.mozilla.firefox",
			"org.mozilla.firefox": "org.mozilla.firefox", "obsidian": "md.obsidian.Obsidian",
			"Obsidian & Co": "md.obsidian.Obsidian", "vlc": "org.videolan.VLC",
			"tool":     "io.github.cli.Tool",
			"platform": "", "feuerfuchs": "", "nothing": "",
		} {
			got, ok := c.Find(pkg)
			if got != want || ok != (want != "") {
				t.Errorf("%s: Find(%q) = %q, %v; want %q", round, pkg, got, ok, want)
			}
		}
		if idx, _ := filepath.Glob(filepath.Join(cacheDir, "flatpak-*.tsv")); len(idx) != 1 {
			t.Errorf("%s: want exactly one cached index, got %v", round, idx)
		}
	}
}

func TestProbeCommands(t *testing.T) {
	var calls []string
	run := func(name string, args ...string) (string, bool) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		switch name + " " + strings.Join(args, " ") {
		case "apt-cache --no-generate show -- vlc", "rpm -q -- vlc", "snap list -- spotify":
			return "", true
		case "dpkg-query -W -f=${db:Status-Abbrev} -- gimp":
			return "ii ", true
		case "dpkg-query -W -f=${db:Status-Abbrev} -- removed":
			return "rc ", true
		case "flatpak list --app --columns=application,name":
			return "org.kde.kdenlive\tKdenlive\n", true
		case "apk search --exact vlc":
			return "vlc-3.0.21-r0\n", true
		case "apk search --exact nothing":
			return "", true
		}
		return "", false
	}
	names := filepath.Join(t.TempDir(), "names")
	os.WriteFile(names, []byte("0ad\nspotify\nzoom-client\n"), 0o644)
	p := &Probe{Run: run, ReadFile: os.ReadFile, Flatpak: &FlatpakCatalog{}, SnapNames: names}

	check := func(what string, got bool, want bool) {
		t.Helper()
		if got != want {
			t.Errorf("%s = %v, want %v", what, got, want)
		}
	}
	_, ok := p.Available("apt", "vlc")
	check("apt has vlc", ok, true)
	_, ok = p.Available("apt", "nothing")
	check("apt has nothing", ok, false)
	_, ok = p.Available("apk", "vlc")
	check("apk has vlc", ok, true)
	_, ok = p.Available("apk", "nothing")
	check("apk has nothing", ok, false)
	_, ok = p.Available("snap", "spotify")
	check("snap has spotify", ok, true)
	_, ok = p.Available("snap", "spot")
	check("snap has spot (no substring match)", ok, false)
	_, ok = p.Installed("apt", "gimp")
	check("gimp installed", ok, true)
	_, ok = p.Installed("apt", "removed")
	check("removed-but-configured counts as not installed", ok, false)
	_, ok = p.Installed("dnf", "vlc")
	check("rpm vlc", ok, true)
	id, ok := p.Installed("flatpak", "kdenlive")
	check("flatpak kdenlive", ok && id == "org.kde.kdenlive", true)
	_, ok = p.Installed("snap", "spotify")
	check("snap spotify", ok, true)

	for _, c := range calls {
		for _, online := range []string{"flatpak search", "snap find", "snap info", "apt update", "dnf makecache"} {
			if strings.HasPrefix(c, online) {
				t.Errorf("probe ran a network command: %s", c)
			}
		}
		if strings.HasPrefix(c, "dnf ") && !strings.Contains(c, "--cacheonly") {
			t.Errorf("dnf without --cacheonly may go online: %s", c)
		}
		if strings.HasPrefix(c, "zypper ") && !strings.Contains(c, "--no-refresh") {
			t.Errorf("zypper without --no-refresh may go online: %s", c)
		}
	}
}

// TestRealFlatpakCatalog runs against this machine's catalog if there is one.
func TestRealFlatpakCatalog(t *testing.T) {
	c := New().Flatpak
	c.CacheDir = t.TempDir()
	start := time.Now()
	id, ok := c.Find("firefox")
	cold := time.Since(start)
	if !ok {
		t.Skip("no local flatpak catalog with firefox")
	}
	start = time.Now()
	c.Find("firefox")
	warm := time.Since(start)
	t.Logf("firefox → %s (cold %v, cached %v)", id, cold, warm)
	if id != "org.mozilla.firefox" {
		t.Errorf("got %q", id)
	}
	if warm > 150*time.Millisecond {
		t.Errorf("cached lookup too slow for an Enter hook: %v", warm)
	}
}
