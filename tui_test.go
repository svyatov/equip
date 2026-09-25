package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/svyatov/equip/internal/equip"
	"github.com/svyatov/equip/internal/equiptest"
)

func newModel(t *testing.T, m *equiptest.Machine) *model {
	t.Helper()
	s, err := equip.Open(m.Machine, m.Root)
	if err != nil {
		t.Fatal(err)
	}
	return &model{s: s}
}

// press feeds keys to tm and returns the last command.
func press(tm *model, keys ...tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		_, cmd = tm.Update(k)
	}
	return cmd
}

func key(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

var down = tea.KeyPressMsg{Code: tea.KeyDown}

func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// line returns the first line of the view that contains s.
func line(tm *model, s string) string {
	for l := range strings.Lines(tm.View().Content) {
		if strings.Contains(l, s) {
			return l
		}
	}
	return ""
}

func TestMainScreenShowsProjectPathAndSkills(t *testing.T) {
	m := equiptest.New(t)
	m.Skill(m.ClaudeSkills(), "review")

	top, list, _ := strings.Cut(newModel(t, m).View().Content, "\n")
	if !strings.Contains(top, m.Root) {
		t.Errorf("top line %q does not show the Project path %q", top, m.Root)
	}
	if !strings.Contains(list, "review") {
		t.Errorf("list %q does not show the skill", list)
	}
}

func TestStateKeySetsAnUnsavedOverrideOnTheHighlightedSkill(t *testing.T) {
	m := equiptest.New(t)
	m.Skill(m.ClaudeSkills(), "alpha")
	m.Skill(m.ClaudeSkills(), "beta")
	tm := newModel(t, m)

	press(tm, down, key('3'))

	if r := tm.s.View().Rows[1]; r.State != equip.Off || !r.Override {
		t.Errorf("beta = %+v, want an Override off", r)
	}
	top, _, _ := strings.Cut(tm.View().Content, "\n")
	if !strings.Contains(top, "1 unsaved") {
		t.Errorf("top line %q does not count 1 unsaved", top)
	}
	if strings.Count(tm.View().Content, "*") != 1 || strings.Contains(line(tm, "alpha"), "*") {
		t.Errorf("unsaved marker on the wrong row:\n%s", tm.View().Content)
	}
}

func TestDetailPaneShowsStatesOriginAndFallback(t *testing.T) {
	m := equiptest.New(t)
	m.Skill(m.ClaudeSkills(), "review")
	tm := newModel(t, m)
	if line(tm, "Origin  default") == "" {
		t.Errorf("view does not show the default origin:\n%s", tm.View().Content)
	}

	press(tm, key('3'))

	for _, want := range []string{"( ) 1 on", "( ) 2 manual-only", "(○) 3 off", "set by hand here", "without it: on (default)"} {
		if line(tm, want) == "" {
			t.Errorf("view does not show %q:\n%s", want, tm.View().Content)
		}
	}
}

func TestDetailPaneShowsTheNote(t *testing.T) {
	m := equiptest.New(t)
	m.Skill(m.ClaudeSkills(), "review")
	press(newModel(t, m), key('3'), key('s'))
	settings := filepath.Join(m.Root, ".claude", "settings.local.json")
	if err := os.WriteFile(settings, []byte(`{"skillOverrides": {"review": "on"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if tm := newModel(t, m); line(tm, "changed outside equip in Claude Code") == "" {
		t.Errorf("view does not show the note:\n%s", tm.View().Content)
	}
}

func TestDropKeyDropsTheOverride(t *testing.T) {
	m := equiptest.New(t)
	m.Skill(m.ClaudeSkills(), "review")
	tm := newModel(t, m)

	press(tm, key('3'), key('x'))

	if r := tm.s.View().Rows[0]; r.Override {
		t.Errorf("review = %+v, want no Override", r)
	}
}

func TestSaveKeyWritesTheOverrides(t *testing.T) {
	m := equiptest.New(t)
	m.Skill(m.ClaudeSkills(), "review")
	tm := newModel(t, m)

	press(tm, key('3'), key('s'))

	if n := tm.s.View().Unsaved; n != 0 {
		t.Errorf("Unsaved = %d after s, want 0", n)
	}
}

func TestSaveKeyShowsTheError(t *testing.T) {
	m := equiptest.New(t)
	m.Skill(m.ClaudeSkills(), "review")
	settings := filepath.Join(m.Root, ".claude", "settings.local.json")
	m.Mkdir(filepath.Dir(settings))
	if err := os.WriteFile(settings, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	tm := newModel(t, m)

	press(tm, key('3'), key('s'))

	if line(tm, "settings.local.json") == "" {
		t.Errorf("view does not show the save error:\n%s", tm.View().Content)
	}
	if press(tm, down); line(tm, "settings.local.json") != "" {
		t.Error("the save error stays after the next key")
	}
}

func TestUpKeyMovesBack(t *testing.T) {
	m := equiptest.New(t)
	m.Skill(m.ClaudeSkills(), "alpha")
	m.Skill(m.ClaudeSkills(), "beta")
	tm := newModel(t, m)

	press(tm, down, tea.KeyPressMsg{Code: tea.KeyUp}, key('3'))

	if r := tm.s.View().Rows[0]; !r.Override {
		t.Errorf("alpha = %+v, want an Override", r)
	}
}

func TestKeysWithNoSkillsDoNothing(t *testing.T) {
	tm := newModel(t, equiptest.New(t))

	press(tm, down, key('1'), key('x'))

	if n := tm.s.View().Unsaved; n != 0 {
		t.Errorf("Unsaved = %d, want 0", n)
	}
}

func TestQuitKeysQuit(t *testing.T) {
	m := equiptest.New(t)
	for _, k := range []tea.KeyPressMsg{
		key('q'),
		{Code: 'c', Mod: tea.ModCtrl},
	} {
		if !quits(press(newModel(t, m), k)) {
			t.Errorf("%s did not quit", k)
		}
	}
}

func TestQuitWithUnsavedChangesAsksFirst(t *testing.T) {
	m := equiptest.New(t)
	m.Skill(m.ClaudeSkills(), "review")
	tm := newModel(t, m)

	if quits(press(tm, key('3'), key('q'))) {
		t.Fatal("q quit with unsaved changes")
	}
	if line(tm, "Quit without saving? y/n") == "" {
		t.Errorf("view does not ask:\n%s", tm.View().Content)
	}
	if quits(press(tm, key('n'))) || line(tm, "Quit without saving?") != "" {
		t.Error("n did not cancel the quit")
	}
	if !quits(press(tm, key('q'), key('y'))) {
		t.Error("y did not quit")
	}
}
