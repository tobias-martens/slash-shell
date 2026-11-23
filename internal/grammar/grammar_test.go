package grammar

import (
	"errors"
	"reflect"
	"testing"
)

func TestParseLine(t *testing.T) {
	tests := []struct {
		line   string
		verb   string
		things []string
		source string
		text   string
	}{
		{"update", Update, nil, "", "update"},
		{"  upgrade  ", Update, nil, "", "upgrade"},
		{"update everything", Update, nil, "", "update everything"},
		{"update flatpak", Update, nil, "flatpak", "update flatpak"},
		{"update only snap", Update, nil, "snap", "update only snap"},
		{"install firefox", Install, []string{"firefox"}, "", "install firefox"},
		{"install firefox as flatpak", Install, []string{"firefox"}, "flatpak", "install firefox as flatpak"},
		{"install firefox as flatpack", Install, []string{"firefox"}, "flatpak", "install firefox as flatpack"},
		{"install firefox, vlc and gimp", Install, []string{"firefox", "vlc", "gimp"}, "", "install firefox, vlc and gimp"},
		{"install firefox,vlc", Install, []string{"firefox", "vlc"}, "", "install firefox, vlc"},
		{"install vlc and gimp from snap", Install, []string{"vlc", "gimp"}, "snap", "install vlc and gimp from snap"},
		{"install as", Install, []string{"as"}, "", "install as"},
		{"install and", Install, []string{"and"}, "", "install and"},
		{"install foo as", Install, []string{"foo", "as"}, "", "install foo as"},
		{"install as flatpak", Install, []string{"as", "flatpak"}, "", "install as flatpak"},
		{"remove spotify from snap", Remove, []string{"spotify"}, "snap", "remove spotify from snap"},
		{"uninstall gimp", Remove, []string{"gimp"}, "", "uninstall gimp"},
		{"install python3-pip libfoo++ g++", Install, []string{"python3-pip", "libfoo++", "g++"}, "", "install python3-pip libfoo++ g++"},
	}
	for _, tt := range tests {
		p, err := ParseLine(tt.line)
		if err != nil {
			t.Errorf("%q: unexpected error %v", tt.line, err)
			continue
		}
		if p.Verb != tt.verb || !reflect.DeepEqual(p.Things, tt.things) || p.Source != tt.source || p.Text != tt.text {
			t.Errorf("%q: got %+v", tt.line, p)
		}
	}
}

func TestNotAPhrase(t *testing.T) {
	for _, line := range []string{
		"", "ls -la", "sudo update", "update && reboot", "install foo | tee x",
		"install 'my app'", "install $PKG", "update # comment", "FOO=1 update",
	} {
		if _, err := ParseLine(line); !errors.Is(err, ErrNotAPhrase) {
			t.Errorf("%q: want ErrNotAPhrase, got %v", line, err)
		}
	}
}

func TestErrors(t *testing.T) {
	for _, line := range []string{
		"install", "remove", "update firefox", "update only", "install a,,b", "install a and",
		"install , a",
	} {
		_, err := ParseLine(line)
		if err == nil || errors.Is(err, ErrNotAPhrase) {
			t.Errorf("%q: want a helpful error, got %v", line, err)
		}
	}
}
