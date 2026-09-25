package main

import (
	"strings"
	"testing"
)

func TestFlags(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ arg, want string }{
		{"--help", "Usage: equip [--help] [--version]\n"},
		{"--version", "equip "},
	} {
		var out strings.Builder
		if err := run([]string{tc.arg}, &out); err != nil {
			t.Errorf("%s: %v", tc.arg, err)
		}
		if !strings.HasPrefix(out.String(), tc.want) {
			t.Errorf("%s printed %q, want it to start with %q", tc.arg, out.String(), tc.want)
		}
	}
}

func TestArgumentsAreRejected(t *testing.T) {
	t.Parallel()
	if err := run([]string{"--version", "extra"}, &strings.Builder{}); err == nil {
		t.Error("run accepted a positional argument")
	}
}
