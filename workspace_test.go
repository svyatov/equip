package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/svyatov/equip/internal/equiptest"
)

// presetModel is a TUI over the skills docs, lint and review, with the
// presets Ruby (lint and the skill rspec, not installed) and Writing (docs).
func presetModel(t *testing.T) *model {
	t.Helper()

	machine := equiptest.New(t)
	for _, name := range []string{"docs", "lint", "review"} {
		machine.Skill(machine.ClaudeSkills(), name)
	}

	machine.Preset("Ruby", `id = "r1"
skills = ["lint", "rspec"]`)
	machine.Preset("Writing", `id = "w1"
skills = ["docs"]`)

	return newModel(t, machine)
}

// topLine is the first line of the view.
func topLine(tui *model) string {
	top, _, _ := strings.Cut(tui.View().Content, "\n")

	return top
}

func TestPresetsKeyOpensTheLibrary(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'))

	for _, want := range []string{"[ ] Ruby", "[ ] Writing", "rspec", "not installed"} {
		if line(tui, want) == "" {
			t.Errorf("workspace does not show %q:\n%s", want, tui.View().Content)
		}
	}

	if top := topLine(tui); !strings.Contains(top, "none (agent defaults)") {
		t.Errorf("top line %q does not say no preset is active", top)
	}
}

func TestSpaceActivatesThePresetAndTheTopLineShowsTheEffect(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'), key(' '))

	if line(tui, "[x] Ruby") == "" {
		t.Errorf("workspace does not check Ruby:\n%s", tui.View().Content)
	}

	top := topLine(tui)
	for _, want := range []string{"active here: Ruby", "Claude Code ~22 → ~7", "Codex ~0 → ~0", "0 on, 2 off"} {
		if !strings.Contains(top, want) {
			t.Errorf("top line %q does not show %q", top, want)
		}
	}

	press(tui, tea.KeyPressMsg{Code: tea.KeyEscape})

	if top := topLine(tui); !strings.Contains(top, "presets Ruby") {
		t.Errorf("main top line %q does not show the active preset", top)
	}
}

func TestDetailPaneShowsThePresetsAsTheOrigin(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'), key(' '), tea.KeyPressMsg{Code: tea.KeyEscape})

	if line(tui, "Origin  presets") == "" {
		t.Errorf("view does not show the presets origin:\n%s", tui.View().Content)
	}

	press(tui, key('1'))

	if line(tui, "without it: off (presets)") == "" {
		t.Errorf("view does not show the presets' state as the fallback:\n%s", tui.View().Content)
	}
}

func TestCtrlCInTheWorkspaceAsksBeforeDroppingUnsavedChanges(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	cmd := press(tui, key('p'), key(' '), tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

	if quits(cmd) || line(tui, "Quit without saving?") == "" {
		t.Errorf("ctrl+c did not ask first:\n%s", tui.View().Content)
	}
}

func TestWorkspaceComparesWithTheViewAtItsOpening(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'), key(' '), tea.KeyPressMsg{Code: tea.KeyEscape}, key('p'))

	if top := topLine(tui); strings.Contains(top, "→") {
		t.Errorf("top line %q shows a change since opening", top)
	}
}

func TestSpaceOnAnActivePresetRemovesIt(t *testing.T) {
	t.Parallel()
	tui := presetModel(t)

	press(tui, key('p'), down(), key(' '), key(' '))

	if line(tui, "[ ] Writing") == "" || strings.Contains(topLine(tui), "→") {
		t.Errorf("Writing is still active:\n%s", tui.View().Content)
	}
}
