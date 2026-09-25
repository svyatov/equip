package main

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/svyatov/equip/internal/equip"
)

// styles are the lipgloss styles of the TUI.
type styles struct {
	top, dim, warn, cur, pane lipgloss.Style
}

func newStyles() styles {
	accent := lipgloss.Color("#af87ff")

	return styles{
		top:  lipgloss.NewStyle().Bold(true).Foreground(accent),
		dim:  lipgloss.NewStyle().Foreground(lipgloss.Color("#8a8a8a")),
		warn: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#d7af5f")),
		cur:  lipgloss.NewStyle().Bold(true).Foreground(accent),
		pane: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1),
	}
}

// glyph is the list mark of st.
func glyph(st equip.State) string {
	return [...]string{equip.On: "●", equip.ManualOnly: "◐", equip.Off: "○"}[st]
}

// model is the Bubble Tea root model over a Session.
type model struct {
	s        *equip.Session
	style    styles
	cur      int  // the highlighted row
	quitting bool // asking to quit with unsaved changes
	flash    string
}

// newTUI is the model of a fresh TUI over s.
func newTUI(s *equip.Session) *model {
	return &model{s: s, style: newStyles()}
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}

	key := keyMsg.String()

	m.flash = ""
	if m.quitting {
		m.quitting = false
		if key == "y" {
			return m, tea.Quit
		}

		return m, nil
	}

	cmd := m.press(key)

	return m, cmd
}

func (m *model) View() tea.View {
	session := m.s.View()

	top := m.style.top.Render("equip  " + session.Project.Path)
	if session.Unsaved > 0 {
		top += "  " + m.style.warn.Render(fmt.Sprintf("%d unsaved", session.Unsaved))
	}

	list := make([]string, 0, len(session.Rows))
	for i, row := range session.Rows {
		mark, name := "  ", row.Name
		if i == m.cur {
			mark, name = m.style.cur.Render("▸ "), m.style.cur.Render(name)
		}

		if row.Unsaved {
			name += m.style.warn.Render("*")
		}

		list = append(list, mark+glyph(row.State)+" "+name)
	}

	panes := []string{m.style.pane.Render(strings.Join(list, "\n"))}
	if m.cur < len(session.Rows) {
		panes = append(panes, m.style.pane.Render(m.detail(session.Rows[m.cur])))
	}

	footer := m.style.dim.Render("↑↓ move  1-3 set state  x drop override  s save  q quit")

	switch {
	case m.quitting:
		footer = m.style.warn.Render(fmt.Sprintf("%d unsaved changes. Quit without saving? y/n", session.Unsaved))
	case m.flash != "":
		footer = m.flash
	}

	view := tea.NewView(top + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, panes...) + "\n" + footer)
	view.AltScreen = true

	return view
}

// press acts on key on the main screen.
func (m *model) press(key string) tea.Cmd {
	rows := m.s.View().Rows

	switch key {
	case "q", "ctrl+c":
		return m.quit()
	case "up", "k":
		m.cur = max(m.cur-1, 0)
	case "down", "j":
		m.cur = max(min(m.cur+1, len(rows)-1), 0)
	case "1", "2", "3":
		if m.cur < len(rows) {
			m.s.SetState(rows[m.cur].Name, equip.States()[key[0]-'1'])
		}
	case "x":
		if m.cur < len(rows) {
			m.s.DropOverride(rows[m.cur].Name)
		}
	case "s":
		err := m.s.Save()
		if err != nil {
			m.flash = m.style.warn.Render("save failed: " + err.Error())
		}
	}

	return nil
}

// quit quits, or first asks to confirm with unsaved changes.
func (m *model) quit() tea.Cmd {
	if m.s.View().Unsaved > 0 {
		m.quitting = true

		return nil
	}

	return tea.Quit
}

// detail is the detail pane of row: its origin and the states to pick from.
func (m *model) detail(row equip.Row) string {
	lines := []string{m.style.cur.Render(row.Name), ""}
	if row.Override {
		lines = append(lines,
			"Origin  "+m.style.warn.Render("override")+", set by hand here",
			m.style.dim.Render("        without it: "+row.Fallback.String()+" (default)"))
	} else {
		lines = append(lines, "Origin  default")
	}

	if row.ChangedOutside {
		lines = append(lines, "        "+m.style.warn.Render("changed outside equip in Claude Code"))
	}

	lines = append(lines, "", "State")

	for index, state := range equip.States() {
		radio := " "
		if state == row.State {
			radio = glyph(state)
		}

		lines = append(lines, fmt.Sprintf("  (%s) %d %s", radio, index+1, state))
	}

	return strings.Join(lines, "\n")
}
