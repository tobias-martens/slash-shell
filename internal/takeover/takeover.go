// Package takeover decides whether slash may handle a verb on this system,
// following the "newest install wins" rule, and keeps the small state that
// rule needs.
package takeover

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// State lives in ~/.local/state/slash (or $XDG_STATE_HOME/slash).
type State struct{ Dir string }

// DefaultState returns the state location for the current user.
func DefaultState() State {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "state")
	}
	return State{Dir: filepath.Join(base, "slash")}
}

func (s State) read(name string) []string {
	data, err := os.ReadFile(filepath.Join(s.Dir, name))
	if err != nil {
		return nil
	}
	return strings.Fields(string(data))
}

func (s State) write(name string, words []string) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.Dir, name), []byte(strings.Join(words, "\n")+"\n"), 0o644)
}

// Recorded returns the names that already existed when slash was enabled.
func (s State) Recorded() []string { return s.read("taken-over") }

// Record stores the names slash took over at enable time and resets notices.
func (s State) Record(names []string) error {
	os.Remove(filepath.Join(s.Dir, "notified"))
	return s.write("taken-over", names)
}

// Enabled reports whether `slash enable` has run.
func (s State) Enabled() bool {
	_, err := os.Stat(filepath.Join(s.Dir, "taken-over"))
	return err == nil
}

// NotifyOnce reports whether the notice for name should be shown, and
// remembers that it was.
func (s State) NotifyOnce(name string) bool {
	done := s.read("notified")
	if slices.Contains(done, name) {
		return false
	}
	s.write("notified", append(done, name))
	return true
}

// Decider applies the rules.
type Decider struct {
	LookPath func(string) (string, error)
	Exists   func(string) bool
	Recorded []string
}

// Decision is the outcome for one line.
type Decision struct {
	Handle bool
	// Newer is set when a program installed after slash owns the verb's name.
	Newer string
}

// Decide says whether slash handles `word args…`.
func (d Decider) Decide(word string, args []string) Decision {
	if word == "install" {
		return Decision{Handle: !coreutilsCould(args, d.Exists)}
	}
	path, err := d.LookPath(word)
	if err != nil || slices.Contains(d.Recorded, word) {
		return Decision{Handle: true}
	}
	return Decision{Newer: path}
}

// coreutilsCould reports whether coreutils `install` might run args
// successfully. Such lines are always left to it.
//
// install [OPTION]... SOURCE DEST | SOURCE... DIRECTORY | -t DIR SOURCE... | -d DIR...
//
// Without options it needs at least two operands, and its sources must exist.
func coreutilsCould(args []string, exists func(string) bool) bool {
	if len(args) == 0 {
		return true // prints its usage error, as before
	}
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return true
		}
	}
	if len(args) == 1 {
		return false // always "missing destination file operand"
	}
	for _, a := range args {
		if strings.Contains(a, "/") || exists(a) {
			return true
		}
	}
	return false
}

// Candidates are the verb names checked for collisions at enable time.
func Candidates(verbs map[string]string) []string {
	var names []string
	for w := range verbs {
		names = append(names, w)
	}
	slices.Sort(names)
	return names
}
