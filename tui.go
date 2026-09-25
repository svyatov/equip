package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/svyatov/equip/internal/equip"
)

var (
	accent = lipgloss.Color("#af87ff")

	topStyle  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	dimStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#8a8a8a"))
	warnStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#d7af5f"))
	curStyle  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	paneStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)

	states = []equip.State{equip.On, equip.ManualOnly, equip.Off}
	glyphs = map[equip.State]string{equip.On: "●", equip.ManualOnly: "◐", equip.Off: "○"}
)

// model is the Bubble Tea root model over a Session.
type model struct {
	s        *equip.Session
	cur      int  // the highlighted row
	quitting bool // asking to quit with unsaved changes
	flash    string
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	m.flash = ""
	if m.quitting {
		m.quitting = false
		if k.String() == "y" {
			return m, tea.Quit
		}
		return m, nil
	}
	rows := m.s.View().Rows
	switch key := k.String(); key {
	case "q", "ctrl+c":
		if m.s.View().Unsaved > 0 {
			m.quitting = true
			return m, nil
		}
		return m, tea.Quit
	case "up", "k":
		m.cur = max(m.cur-1, 0)
	case "down", "j":
		m.cur = max(min(m.cur+1, len(rows)-1), 0)
	case "1", "2", "3":
		if m.cur < len(rows) {
			m.s.SetState(rows[m.cur].Name, states[key[0]-'1'])
		}
	case "x":
		if m.cur < len(rows) {
			m.s.DropOverride(rows[m.cur].Name)
		}
	case "s":
		if err := m.s.Save(); err != nil {
			m.flash = warnStyle.Render("save failed: " + err.Error())
		}
	}
	return m, nil
}

func (m *model) View() tea.View {
	v := m.s.View()
	top := topStyle.Render("equip  " + v.Project.Path)
	if v.Unsaved > 0 {
		top += "  " + warnStyle.Render(fmt.Sprintf("%d unsaved", v.Unsaved))
	}
	var list []string
	for i, r := range v.Rows {
		mark, name := "  ", r.Name
		if i == m.cur {
			mark, name = curStyle.Render("▸ "), curStyle.Render(name)
		}
		if r.Unsaved {
			name += warnStyle.Render("*")
		}
		list = append(list, mark+glyphs[r.State]+" "+name)
	}
	panes := []string{paneStyle.Render(strings.Join(list, "\n"))}
	if m.cur < len(v.Rows) {
		panes = append(panes, paneStyle.Render(detail(v.Rows[m.cur])))
	}
	footer := dimStyle.Render("↑↓ move  1-3 set state  x drop override  s save  q quit")
	switch {
	case m.quitting:
		footer = warnStyle.Render(fmt.Sprintf("%d unsaved changes. Quit without saving? y/n", v.Unsaved))
	case m.flash != "":
		footer = m.flash
	}
	view := tea.NewView(top + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, panes...) + "\n" + footer)
	view.AltScreen = true
	return view
}

// detail is the detail pane of r: its origin and the states to pick from.
func detail(r equip.Row) string {
	lines := []string{curStyle.Render(r.Name), ""}
	if r.Override {
		lines = append(lines,
			"Origin  "+warnStyle.Render("override")+", set by hand here",
			dimStyle.Render("        without it: "+r.Fallback.String()+" (default)"))
	} else {
		lines = append(lines, "Origin  default")
	}
	if r.ChangedOutside {
		lines = append(lines, "        "+warnStyle.Render("changed outside equip in Claude Code"))
	}
	lines = append(lines, "", "State")
	for i, st := range states {
		radio := " "
		if st == r.State {
			radio = glyphs[st]
		}
		lines = append(lines, fmt.Sprintf("  (%s) %d %s", radio, i+1, st))
	}
	return strings.Join(lines, "\n")
}
