// Package trash moves files to the user's trash the way file managers do
// (the freedesktop.org trash specification), for systems without gio or
// trash-put. Files can be restored from any file manager's trash.
//
// Only the home trash is used: a file on another drive can't be moved
// there without copying, so slash refuses instead of deleting anything.
package trash

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Dir is the home trash: $XDG_DATA_HOME/Trash, or ~/.local/share/Trash.
func Dir(home, dataHome string) string {
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "Trash")
}

// Put moves path into the trash at dir and returns where it went.
func Put(dir, path string, now time.Time) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(abs); err != nil {
		return "", fmt.Errorf("I found no file or folder called %q", path)
	}
	if abs == "/" || strings.HasPrefix(dir+"/", abs+"/") {
		return "", fmt.Errorf("%s holds the trash itself; I can't move it there", path)
	}
	if !sameDrive(filepath.Dir(abs), dir) {
		return "", fmt.Errorf("%s is on another drive than your home folder; I can only move files from your home drive to the trash", path)
	}
	files, info := filepath.Join(dir, "files"), filepath.Join(dir, "info")
	for _, d := range []string{files, info} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return "", err
		}
	}
	text := "[Trash Info]\nPath=" + (&url.URL{Path: abs}).EscapedPath() +
		"\nDeletionDate=" + now.Format("2006-01-02T15:04:05") + "\n"
	base := filepath.Base(abs)
	for n := 1; n < 10000; n++ {
		name := base
		if n > 1 {
			ext := filepath.Ext(base)
			name = strings.TrimSuffix(base, ext) + "." + strconv.Itoa(n) + ext
		}
		// The info file is created first and exclusively: it reserves the name.
		f, err := os.OpenFile(filepath.Join(info, name+".trashinfo"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.WriteString(text)
		cerr := f.Close()
		target := filepath.Join(files, name)
		if _, err := os.Lstat(target); err == nil {
			os.Remove(f.Name()) // a leftover without info: keep it, take the next name
			continue
		}
		if err := errors.Join(werr, cerr); err != nil {
			os.Remove(f.Name())
			return "", err
		}
		if err := os.Rename(abs, target); err != nil {
			os.Remove(f.Name())
			return "", err
		}
		return target, nil
	}
	return "", fmt.Errorf("too many files called %s in the trash", base)
}

// sameDrive reports whether a and b (or b's nearest existing parent) are
// on the same file system, so a rename moves without copying.
func sameDrive(a, b string) bool {
	var sa, sb syscall.Stat_t
	if syscall.Stat(a, &sa) != nil {
		return false
	}
	for syscall.Stat(b, &sb) != nil {
		parent := filepath.Dir(b)
		if parent == b {
			return false
		}
		b = parent
	}
	return sa.Dev == sb.Dev
}
