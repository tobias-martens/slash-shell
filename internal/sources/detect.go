// Package sources detects which package sources exist on this system:
// one system package manager plus flatpak and snap.
package sources

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"strings"
)

// Managers lists the supported system package managers, in the fallback order
// used when /etc/os-release gives no hint.
var Managers = []string{"apt", "dnf", "pacman", "zypper", "apk"}

// osHints maps os-release IDs (ID and ID_LIKE) to their package manager.
var osHints = map[string]string{
	"debian": "apt", "ubuntu": "apt",
	"fedora": "dnf", "rhel": "dnf", "centos": "dnf",
	"arch": "pacman", "steamos": "pacman",
	"opensuse": "zypper", "suse": "zypper",
	"alpine": "apk",
}

// System describes what was found.
type System struct {
	Manager   string // system package manager, "" if none
	Immutable bool   // read-only base system (SteamOS, Fedora Atomic, …)
	Flatpak   bool
	Snap      bool
	OSID      string
	OSLike    []string
}

// InstallDefault is the source `install` tries first.
func (s System) InstallDefault() string {
	if s.Immutable && s.Flatpak {
		return "flatpak"
	}
	if s.Manager != "" {
		return s.Manager
	}
	if s.Flatpak {
		return "flatpak"
	}
	if s.Snap {
		return "snap"
	}
	return ""
}

// Env abstracts the outside world so detection can be tested.
type Env struct {
	LookPath func(string) (string, error)
	ReadFile func(string) ([]byte, error)
	Exists   func(string) bool
}

// RealEnv is the Env of the running system.
func RealEnv() Env {
	return Env{
		LookPath: exec.LookPath,
		ReadFile: os.ReadFile,
		Exists: func(p string) bool {
			_, err := os.Stat(p)
			return err == nil
		},
	}
}

// Detect inspects the system through env.
func Detect(env Env) System {
	var s System
	s.OSID, s.OSLike = readOSRelease(env)

	has := func(name string) bool {
		_, err := env.LookPath(name)
		return err == nil
	}

	var present []string
	for _, m := range Managers {
		if has(m) {
			present = append(present, m)
		}
	}
	// Prefer the manager the OS itself names; otherwise the first one found.
	for _, id := range append([]string{s.OSID}, s.OSLike...) {
		if m, ok := osHints[id]; ok && contains(present, m) {
			s.Manager = m
			break
		}
	}
	if s.Manager == "" && len(present) > 0 {
		s.Manager = present[0]
	}

	s.Flatpak = has("flatpak")
	s.Snap = has("snap")
	s.Immutable = s.OSID == "steamos" || env.Exists("/run/ostree-booted")
	return s
}

func readOSRelease(env Env) (id string, like []string) {
	data, err := env.ReadFile("/etc/os-release")
	if err != nil {
		data, err = env.ReadFile("/usr/lib/os-release")
		if err != nil {
			return "", nil
		}
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		key, val, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok {
			continue
		}
		val = strings.Trim(val, `"'`)
		switch key {
		case "ID":
			id = strings.ToLower(val)
		case "ID_LIKE":
			like = strings.Fields(strings.ToLower(val))
		}
	}
	return id, like
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
