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

// descriptionWidth is the width the detail pane wraps a description at.
const descriptionWidth = 60

// shortDescriptionWidth is the width the detail pane cuts the description of
// a plugin's skill at.
const shortDescriptionWidth = 40

// glyph is the list mark of st.
func glyph(st equip.State) string {
	return [...]string{equip.On: "●", equip.ManualOnly: "◐", equip.Off: "○"}[st]
}

// cost shows an estimate of tokens.
func cost(tokens int) string { return fmt.Sprintf("~%d", tokens) }

// costOf shows the cost of an extension of kind. An MCP server's is unknown
// until it is measured.
func costOf(kind equip.Kind, tokens int) string {
	if kind == equip.MCPServer {
		return "unknown"
	}

	return cost(tokens)
}

// model is the Bubble Tea root model over a Session.
type model struct {
	style    styles
	s        *equip.Session
	flash    string
	cur      int  // the highlighted row
	quitting bool // asking to quit with unsaved changes
}

// newTUI is the model of a fresh TUI over s.
func newTUI(s *equip.Session) *model {
	return &model{s: s, style: newStyles(), cur: 0, quitting: false, flash: ""}
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

	top := []string{m.style.top.Render("equip  " + session.Project.Path)}
	for _, agent := range equip.Agents() {
		top = append(top, agent.String()+" "+cost(session.Totals[agent]))
	}

	if session.Unsaved > 0 {
		top = append(top, m.style.warn.Render(fmt.Sprintf("%d unsaved", session.Unsaved)))
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

		list = append(list, mark+glyph(row.State)+" "+name+" "+m.style.dim.Render(costOf(row.Kind, row.Cost)))
	}

	panes := []string{m.style.pane.Render(strings.Join(list, "\n"))}
	if m.cur < len(session.Rows) {
		row := session.Rows[m.cur]
		panes = append(panes, m.style.pane.Render(m.detail(row, m.s.Detail(row.Key))))
	}

	footer := m.style.dim.Render("↑↓ move  1-3 set state  x drop override  s save  q quit")

	switch {
	case m.quitting:
		footer = m.style.warn.Render(fmt.Sprintf("%d unsaved changes. Quit without saving? y/n", session.Unsaved))
	case m.flash != "":
		footer = m.flash
	}

	view := tea.NewView(strings.Join(top, "  ") + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, panes...) + "\n" + footer)
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
			m.setState(rows[m.cur].Key, int(key[0]-'1'))
		}
	case "x":
		if m.cur < len(rows) {
			m.s.DropOverride(rows[m.cur].Key)
		}
	case "s":
		err := m.s.Save()
		if err != nil {
			m.flash = m.style.warn.Render("save failed: " + err.Error())
		}
	}

	return nil
}

// setState sets the extension with key to the state the key numbered i picks
// among the states it offers.
func (m *model) setState(key string, i int) {
	if states := m.s.Detail(key).States; i < len(states) {
		m.s.SetState(key, states[i])
	}
}

// quit quits, or first asks to confirm with unsaved changes.
func (m *model) quit() tea.Cmd {
	if m.s.View().Unsaved > 0 {
		m.quitting = true

		return nil
	}

	return tea.Quit
}

// detail is the detail pane of row: what it is, which agents have it, its
// origin, the states to pick from and where it comes from.
func (m *model) detail(row equip.Row, ext equip.Detail) string {
	agents := make([]string, 0, len(ext.Agents))
	for _, agent := range ext.Agents {
		agents = append(agents, agent.String())
	}

	lines := []string{
		m.style.cur.Render(row.Name) + m.style.dim.Render("  "+row.Kind.String()),
		m.style.dim.Width(descriptionWidth).Render(ext.Description),
		"",
	}

	if ext.Marketplace != "" {
		lines = append(lines, "Marketplace  "+ext.Marketplace)
	}

	lines = append(lines, "Agents  "+strings.Join(agents, ", "))

	for _, agent := range ext.Agents {
		if reason, ok := ext.NotApplied[agent]; ok {
			lines = append(lines, "        "+m.style.dim.Render("not applied in "+agent.String()+": "+reason))
		}
	}

	lines = append(lines, costLine(row.Kind, ext))

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

	for index, state := range ext.States {
		radio := " "
		if state == row.State {
			radio = glyph(state)
		}

		lines = append(lines, fmt.Sprintf("  (%s) %d %s", radio, index+1, state))
	}

	lines = append(lines, m.contents(ext.Contents)...)
	lines = append(lines, m.locations(ext)...)

	return strings.Join(lines, "\n")
}

// locations are the detail pane lines of where ext comes from.
func (m *model) locations(ext equip.Detail) []string {
	lines := []string{"", "Locations"}
	if ext.BuiltIn {
		lines = append(lines, "  "+m.style.dim.Render("built into Claude Code"))
	}

	for _, loc := range ext.Locations {
		lines = append(lines, "  "+loc.Path+"  "+m.style.dim.Render(loc.Agent.String()))
	}

	return lines
}

// costLine is the detail pane line of the cost in each agent of ext, of kind.
func costLine(kind equip.Kind, ext equip.Detail) string {
	costs := make([]string, 0, len(ext.Agents))
	for _, agent := range ext.Agents {
		costs = append(costs, agent.String()+" "+costOf(kind, ext.Costs[agent]))
	}

	line := "Cost    " + strings.Join(costs, ", ")
	if ext.Hooks {
		line += " + hook output, unknown"
	}

	return line
}

// contents are the detail pane lines of a plugin's contents, read-only.
func (m *model) contents(contents []equip.Content) []string {
	if len(contents) == 0 {
		return nil
	}

	lines := []string{"", "Contents  " + m.style.dim.Render("follow the plugin")}

	for _, content := range contents {
		lines = append(lines, fmt.Sprintf("  %s %s %s %s %s", glyph(content.State), content.Kind, content.Name,
			costOf(content.Kind, content.Cost),
			m.style.dim.MaxWidth(shortDescriptionWidth).MaxHeight(1).Render(content.Description)))
	}

	return lines
}
