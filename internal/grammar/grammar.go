// Package grammar parses slash phrases like "install firefox, vlc and gimp as
// flatpak" into a Phrase. The grammar is small and fixed on purpose: anything
// that doesn't fit is rejected, never guessed.
package grammar

import (
	"errors"
	"fmt"
	"strings"
)

// Canonical verbs.
const (
	Update  = "update"
	Install = "install"
	Remove  = "remove"
	Search  = "search"
	About   = "about"
)

// Verbs maps every accepted verb word (including synonyms) to its canonical verb.
var Verbs = map[string]string{
	"update": Update, "upgrade": Update,
	"install": Install,
	"remove":  Remove, "uninstall": Remove,
	"search": Search,
	"about":  About, "describe": About,
}

// Source words. "system" means whatever package manager the system uses.
var sourceWords = map[string]string{
	"system": "system", "native": "system", "deb": "system", "rpm": "system",
	"apt": "apt", "dnf": "dnf", "yum": "dnf", "pacman": "pacman", "zypper": "zypper", "apk": "apk",
	"flatpak": "flatpak", "flatpaks": "flatpak", "flatpack": "flatpak", "flatpacks": "flatpak", "flathub": "flatpak",
	"snap": "snap", "snaps": "snap", "snapcraft": "snap",
}

// Extra source words that only make sense for search.
var searchSourceWords = map[string]string{
	"file": "files", "files": "files",
	"package": "packages", "packages": "packages",
	"program": "packages", "programs": "packages", "app": "packages", "apps": "packages",
}

// Connector words that introduce a source at the end of a phrase.
var sourceIntro = map[string]bool{"as": true, "from": true, "via": true, "in": true}

// Phrase is a parsed slash phrase.
type Phrase struct {
	Verb   string   // canonical verb
	Word   string   // verb as typed (e.g. "uninstall")
	Things []string // package names etc.; for search: the words to look for
	Source string   // canonical source, "" if none given
	Text   string   // the phrase, normalised to single spaces
	Words  []string // the words as given (for handing the phrase on to `slash`)
}

// ErrNotAPhrase means the input isn't something slash handles at all; the
// caller should leave it alone.
var ErrNotAPhrase = errors.New("not a slash phrase")

// SourceOf returns the canonical source for a word, or "".
func SourceOf(word string) string { return sourceWords[strings.ToLower(word)] }

// ParseLine parses a full command line as typed. Lines with shell syntax
// (pipes, quotes, variables, redirections, …) are never slash phrases.
func ParseLine(line string) (Phrase, error) {
	if strings.ContainsAny(line, "|&;<>()$`\"'\\*?[]{}!#=\n") {
		return Phrase{}, ErrNotAPhrase
	}
	return Parse(strings.Fields(line))
}

// Parse parses already-split words (e.g. a shell function's arguments).
func Parse(words []string) (Phrase, error) {
	given := words
	words = splitCommas(words)
	if len(words) == 0 {
		return Phrase{}, ErrNotAPhrase
	}
	verb, ok := Verbs[words[0]]
	if !ok {
		return Phrase{}, ErrNotAPhrase
	}
	p := Phrase{Verb: verb, Word: words[0], Text: joinPhrase(words), Words: given}
	rest := words[1:]

	if verb == Update {
		return parseUpdate(p, rest)
	}

	sourceOf := SourceOf
	if verb == Search {
		sourceOf = func(w string) string {
			if s := searchSourceWords[strings.ToLower(w)]; s != "" {
				return s
			}
			return SourceOf(w)
		}
	}
	// Optional trailing "as|from|via|in <source>". Only counts in that exact
	// position, so "install as" still installs a package named "as".
	if n := len(rest); n >= 3 && sourceIntro[strings.ToLower(rest[n-2])] && sourceOf(rest[n-1]) != "" {
		p.Source = sourceOf(rest[n-1])
		rest = rest[:n-2]
	}
	if verb == Search {
		// All words are looked for together; "and" and commas are just words.
		for _, w := range rest {
			if w != "," {
				p.Things = append(p.Things, w)
			}
		}
		if len(p.Things) == 0 {
			return Phrase{}, fmt.Errorf("search for what? For example: search holiday")
		}
		return p, nil
	}
	things, err := parseList(rest)
	if err != nil {
		return Phrase{}, err
	}
	if len(things) == 0 {
		return Phrase{}, fmt.Errorf("%s what? For example: %s firefox", p.Word, p.Word)
	}
	p.Things = things
	return p, nil
}

func parseUpdate(p Phrase, rest []string) (Phrase, error) {
	if len(rest) > 0 && strings.EqualFold(rest[0], "only") {
		rest = rest[1:]
		if len(rest) == 0 {
			return Phrase{}, fmt.Errorf("update only what? For example: update only flatpak")
		}
	}
	switch {
	case len(rest) == 0:
		return p, nil
	case len(rest) == 1 && (strings.EqualFold(rest[0], "everything") || strings.EqualFold(rest[0], "all")):
		return p, nil
	case len(rest) == 1 && SourceOf(rest[0]) != "":
		p.Source = SourceOf(rest[0])
		return p, nil
	}
	return Phrase{}, fmt.Errorf("I can update everything (update) or one source (update flatpak), not %q", strings.Join(rest, " "))
}

// parseList turns "a , b and c" into [a b c]. "and" is only a connector
// after a thing; a lone "and" (as in "install and") is a thing itself.
func parseList(words []string) ([]string, error) {
	var out []string
	expectThing := true
	for i, w := range words {
		isAnd := strings.EqualFold(w, "and") && i > 0 && words[i-1] != ","
		switch {
		case w == ",":
			if expectThing {
				return nil, fmt.Errorf("unexpected comma")
			}
			expectThing = true
		case isAnd && !expectThing:
			expectThing = true
		default:
			out = append(out, w)
			expectThing = false
		}
	}
	if expectThing && len(out) > 0 {
		return nil, fmt.Errorf("the list ends with a comma or \"and\"")
	}
	return out, nil
}

// splitCommas turns ["firefox,", "vlc"] into ["firefox", ",", "vlc"].
func splitCommas(words []string) []string {
	var out []string
	for _, w := range words {
		for {
			i := strings.IndexByte(w, ',')
			if i < 0 {
				break
			}
			if i > 0 {
				out = append(out, w[:i])
			}
			out = append(out, ",")
			w = w[i+1:]
		}
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

func joinPhrase(words []string) string {
	var b strings.Builder
	for i, w := range words {
		if i > 0 && w != "," {
			b.WriteByte(' ')
		}
		b.WriteString(w)
	}
	return b.String()
}
