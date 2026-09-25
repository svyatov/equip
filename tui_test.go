package main

import (
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

func TestQuitKeysQuit(t *testing.T) {
	m := equiptest.New(t)
	for _, key := range []tea.KeyPressMsg{
		{Code: 'q', Text: "q"},
		{Code: 'c', Mod: tea.ModCtrl},
	} {
		_, cmd := newModel(t, m).Update(key)
		if cmd == nil {
			t.Errorf("%s returned no command", key)
			continue
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Errorf("%s did not quit", key)
		}
	}
}
