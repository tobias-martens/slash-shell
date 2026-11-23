package sources

import (
	"errors"
	"testing"
)

func fakeEnv(osRelease string, bins []string, files []string) Env {
	return Env{
		LookPath: func(name string) (string, error) {
			for _, b := range bins {
				if b == name {
					return "/usr/bin/" + name, nil
				}
			}
			return "", errors.New("not found")
		},
		ReadFile: func(p string) ([]byte, error) {
			if p == "/etc/os-release" && osRelease != "" {
				return []byte(osRelease), nil
			}
			return nil, errors.New("not found")
		},
		Exists: func(p string) bool {
			for _, f := range files {
				if f == p {
					return true
				}
			}
			return false
		},
	}
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name          string
		osRelease     string
		bins, files   []string
		wantManager   string
		wantImmutable bool
		wantFlatpak   bool
		wantSnap      bool
		wantInstall   string
	}{
		{
			name:        "ubuntu with flatpak and snap",
			osRelease:   "ID=ubuntu\nID_LIKE=debian\n",
			bins:        []string{"apt", "flatpak", "snap"},
			wantManager: "apt", wantFlatpak: true, wantSnap: true, wantInstall: "apt",
		},
		{
			name:        "mint via ID_LIKE with quotes",
			osRelease:   "ID=linuxmint\nID_LIKE=\"ubuntu debian\"\n",
			bins:        []string{"apt"},
			wantManager: "apt", wantInstall: "apt",
		},
		{
			name:        "fedora prefers dnf even if apt is also installed",
			osRelease:   "ID=fedora\n",
			bins:        []string{"apt", "dnf", "flatpak"},
			wantManager: "dnf", wantFlatpak: true, wantInstall: "dnf",
		},
		{
			name:        "arch",
			osRelease:   "ID=arch\n",
			bins:        []string{"pacman"},
			wantManager: "pacman", wantInstall: "pacman",
		},
		{
			name:        "steamos is immutable, installs via flatpak",
			osRelease:   "ID=steamos\nID_LIKE=arch\n",
			bins:        []string{"pacman", "flatpak"},
			wantManager: "pacman", wantImmutable: true, wantFlatpak: true, wantInstall: "flatpak",
		},
		{
			name:        "fedora atomic (ostree) is immutable",
			osRelease:   "ID=fedora\nVARIANT_ID=silverblue\n",
			bins:        []string{"dnf", "flatpak", "rpm-ostree"},
			files:       []string{"/run/ostree-booted"},
			wantManager: "dnf", wantImmutable: true, wantFlatpak: true, wantInstall: "flatpak",
		},
		{
			name:        "opensuse via ID_LIKE",
			osRelease:   "ID=opensuse-tumbleweed\nID_LIKE=\"opensuse suse\"\n",
			bins:        []string{"zypper"},
			wantManager: "zypper", wantInstall: "zypper",
		},
		{
			name:        "unknown os falls back to first manager found",
			osRelease:   "ID=somethingelse\n",
			bins:        []string{"apk"},
			wantManager: "apk", wantInstall: "apk",
		},
		{
			name:        "no os-release, only flatpak",
			bins:        []string{"flatpak"},
			wantFlatpak: true, wantInstall: "flatpak",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Detect(fakeEnv(tt.osRelease, tt.bins, tt.files))
			if s.Manager != tt.wantManager || s.Immutable != tt.wantImmutable ||
				s.Flatpak != tt.wantFlatpak || s.Snap != tt.wantSnap {
				t.Errorf("got %+v", s)
			}
			if got := s.InstallDefault(); got != tt.wantInstall {
				t.Errorf("InstallDefault() = %q, want %q", got, tt.wantInstall)
			}
		})
	}
}
