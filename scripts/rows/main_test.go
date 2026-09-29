package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/svyatov/equip/internal/equiptest"
)

func TestRunPrintsAPluginsSkillsAfterItsLine(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	machine.Skill(filepath.Join(dir, "skills"), "review")

	var stdout, stderr bytes.Buffer

	err := run(machine.Machine, []string{machine.Root}, &stdout, &stderr)
	if err != nil {
		t.Fatal(err)
	}

	where := "Claude Code: " + dir

	want := "plugin\tgithub@official\t\ton\tClaude Code\t" + where + "\n" +
		"skill\treview\tgithub@official\ton\tClaude Code\t" + where + "\n"
	if got := stdout.String(); got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}

	if !strings.Contains(stderr.String(), "Skills\t0\n") {
		t.Errorf("stderr = %q, want Skills at 0", stderr.String())
	}
}
