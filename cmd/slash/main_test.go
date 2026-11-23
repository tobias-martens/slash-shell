package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tobias-martens/slash-shell/internal/sources"
)

func TestRun(t *testing.T) {
	env := sources.RealEnv()
	tests := []struct {
		args     []string
		code     int
		inStdout string
		inStderr string
	}{
		{nil, 0, "simple and natural", ""},
		{[]string{"--help"}, 0, "simple and natural", ""},
		{[]string{"version"}, 0, "slash " + version, ""},
		{[]string{"frobnicate"}, 2, "", `don't know "frobnicate"`},
	}
	for _, tt := range tests {
		var out, errOut bytes.Buffer
		code := run(tt.args, &out, &errOut, env)
		if code != tt.code {
			t.Errorf("run(%q) = %d, want %d", tt.args, code, tt.code)
		}
		if !strings.Contains(out.String(), tt.inStdout) || !strings.Contains(errOut.String(), tt.inStderr) {
			t.Errorf("run(%q): stdout=%q stderr=%q", tt.args, out.String(), errOut.String())
		}
	}
}
