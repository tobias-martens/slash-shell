// Package setup implements `slash enable` and `slash disable`: adding and
// removing slash's one block in the user's shell start-up file, so that
// disabling restores every file exactly as it was.
package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Target is a shell start-up file slash can hook into.
type Target struct {
	Shell string
	Path  string
	Block string // exact text slash adds
	Own   bool   // the whole file belongs to slash (fish conf.d)
}

const (
	begin = "# >>> slash >>> (added by `slash enable`, removed by `slash disable`)\n"
	end   = "# <<< slash <<<\n"
)

// TargetFor returns where slash hooks into shell for the user at home.
func TargetFor(shell, home string, getenv func(string) string) (Target, error) {
	switch shell {
	case "bash":
		return Target{Shell: shell, Path: filepath.Join(home, ".bashrc"),
			Block: begin + "command -v slash >/dev/null && eval \"$(slash init bash)\"\n" + end}, nil
	case "zsh":
		dir := getenv("ZDOTDIR")
		if dir == "" {
			dir = home
		}
		return Target{Shell: shell, Path: filepath.Join(dir, ".zshrc"),
			Block: begin + "command -v slash >/dev/null && eval \"$(slash init zsh)\"\n" + end}, nil
	case "fish":
		dir := getenv("XDG_CONFIG_HOME")
		if dir == "" {
			dir = filepath.Join(home, ".config")
		}
		return Target{Shell: shell, Path: filepath.Join(dir, "fish", "conf.d", "slash.fish"), Own: true,
			Block: begin + "command -q slash; and slash init fish | source\n" + end}, nil
	}
	return Target{}, fmt.Errorf("I can't set up %q; supported: bash, zsh, fish", shell)
}

// WithPath returns t with a line in its block that adds dir to PATH (at
// the end, so it never shadows other commands), for a slash binary in a
// folder that isn't on PATH yet. The line checks first, so dir is never
// added twice; disabling slash removes it with the rest of the block.
func (t Target) WithPath(dir, home string) (Target, error) {
	if strings.ContainsAny(dir, "'\"$\\`\n") {
		return t, fmt.Errorf("can't add %s to PATH: unusual characters in its name", dir)
	}
	q := "'" + dir + "'"
	if rel, ok := strings.CutPrefix(dir, home+"/"); ok && home != "" {
		q = `"$HOME/` + rel + `"`
	}
	var line string
	if t.Shell == "fish" {
		line = "contains -- " + q + " $PATH; or set -gx PATH $PATH " + q + "\n"
	} else {
		line = `case ":$PATH:" in *:` + q + `:*) ;; *) PATH="$PATH:"` + q + " ;; esac\n"
	}
	t.Block = begin + line + strings.TrimPrefix(t.Block, begin)
	return t, nil
}

// record is what slash remembers about one change, to undo it exactly.
type record struct {
	Path     string   `json:"path"`
	Inserted string   `json:"inserted"`       // exact text added (may start with "\n")
	Created  bool     `json:"created"`        // the file didn't exist before
	Dirs     []string `json:"dirs,omitempty"` // directories slash created, innermost first
}

// Store keeps the records in slash's state dir.
type Store struct{ Dir string }

func (s Store) path() string { return filepath.Join(s.Dir, "changes.json") }

func (s Store) load() ([]record, error) {
	data, err := os.ReadFile(s.path())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var recs []record
	return recs, json.Unmarshal(data, &recs)
}

func (s Store) save(recs []record) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(recs, "", "  ")
	return os.WriteFile(s.path(), data, 0o644)
}

// Enable adds t's block unless it is already there. It reports whether the
// file changed.
func (s Store) Enable(t Target) (bool, error) {
	old, err := os.ReadFile(t.Path)
	existed := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if strings.Contains(string(old), strings.TrimSuffix(begin, "\n")) {
		return false, nil // already enabled
	}
	rec := record{Path: t.Path, Created: !existed}
	if !existed {
		rec.Dirs = missingDirs(filepath.Dir(t.Path))
		if err := os.MkdirAll(filepath.Dir(t.Path), 0o755); err != nil {
			return false, err
		}
	}
	rec.Inserted = t.Block
	if len(old) > 0 {
		// Ends the last line if needed, or leaves a blank line before the block.
		rec.Inserted = "\n" + t.Block
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(t.Path); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.WriteFile(t.Path, append(old, rec.Inserted...), mode); err != nil {
		return false, err
	}
	recs, err := s.load()
	if err != nil {
		return true, err
	}
	return true, s.save(append(recs, rec))
}

// Disable undoes every recorded change. Files are restored byte for byte if
// the user didn't touch slash's block; otherwise the block's lines are
// removed and a note is returned.
func (s Store) Disable() (notes []string, err error) {
	recs, err := s.load()
	if err != nil {
		return nil, err
	}
	for _, r := range recs {
		data, err := os.ReadFile(r.Path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return notes, err
		}
		content := string(data)
		var restored string
		switch {
		case strings.Count(content, r.Inserted) == 1:
			restored = strings.Replace(content, r.Inserted, "", 1)
		default:
			restored = removeMarked(content)
			notes = append(notes, fmt.Sprintf("%s was edited since slash was enabled; removed slash's lines only", r.Path))
		}
		if r.Created && restored == "" {
			os.Remove(r.Path)
			for _, d := range r.Dirs {
				os.Remove(d) // only succeeds if empty
			}
			continue
		}
		st, _ := os.Stat(r.Path)
		if err := os.WriteFile(r.Path, []byte(restored), st.Mode().Perm()); err != nil {
			return notes, err
		}
	}
	return notes, os.Remove(s.path())
}

// removeMarked removes everything from slash's begin marker to its end marker.
func removeMarked(content string) string {
	b := strings.Index(content, strings.TrimSuffix(begin, "\n"))
	if b < 0 {
		return content
	}
	e := strings.Index(content[b:], end)
	if e < 0 {
		return content
	}
	return content[:b] + content[b+e+len(end):]
}

// missingDirs lists dir and its parents that don't exist yet, innermost first.
func missingDirs(dir string) []string {
	var out []string
	for {
		if _, err := os.Stat(dir); err == nil {
			return out
		}
		out = append(out, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			return out
		}
		dir = parent
	}
}

// Paths lists the files slash's block was added to.
func (s Store) Paths() []string {
	recs, _ := s.load()
	var out []string
	for _, r := range recs {
		out = append(out, r.Path)
	}
	return out
}
