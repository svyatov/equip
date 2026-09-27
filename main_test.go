package main

import (
	"strings"
	"testing"

	"github.com/svyatov/equip/internal/equiptest"
)

// A TestMain that returns exits with m.Run's code.
func TestMain(m *testing.M) {
	defer equiptest.RemoveTemplate()

	m.Run()
}

func TestFlags(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct{ arg, want string }{
		{"--help", "Usage: equip [--help] [--version]\n"},
		{"--version", "equip "},
	} {
		var out strings.Builder

		err := run([]string{testCase.arg}, &out)
		if err != nil {
			t.Errorf("%s: %v", testCase.arg, err)
		}

		if !strings.HasPrefix(out.String(), testCase.want) {
			t.Errorf("%s printed %q, want it to start with %q", testCase.arg, out.String(), testCase.want)
		}
	}
}

func TestArgumentsAreRejected(t *testing.T) {
	t.Parallel()

	err := run([]string{"--version", "extra"}, &strings.Builder{})
	if err == nil {
		t.Error("run accepted a positional argument")
	}
}
