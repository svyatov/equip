package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/svyatov/equip/internal/equip"
)

var topStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#af87ff"))

// model is the Bubble Tea root model over a Session.
type model struct {
	s *equip.Session
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *model) View() tea.View {
	v := m.s.View()
	var b strings.Builder
	b.WriteString(topStyle.Render("equip  "+v.Project.Path) + "\n")
	for _, r := range v.Rows {
		b.WriteString("  " + r.Name + "\n")
	}
	view := tea.NewView(b.String())
	view.AltScreen = true
	return view
}
