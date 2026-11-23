// Package shell holds the per-shell integration scripts printed by
// `slash init <shell>`.
package shell

import (
	"embed"
	"fmt"
	"slices"
	"strings"
)

//go:embed slash.bash slash.zsh slash.fish
var files embed.FS

// Supported lists the shells `slash init` knows.
var Supported = []string{"bash", "zsh", "fish"}

// Script returns the integration for shell, with the verb words filled in.
func Script(shell string, verbs []string) (string, error) {
	if !slices.Contains(Supported, shell) {
		return "", fmt.Errorf("I can't set up %q yet; supported: %s", shell, strings.Join(Supported, ", "))
	}
	raw, err := files.ReadFile("slash." + shell)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(raw), "@VERBS@", strings.Join(verbs, " ")), nil
}
