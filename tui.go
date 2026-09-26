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

// costOf shows a cost of tokens, or that it is unknown in whole or in part,
// as an MCP server's is until it is measured.
func costOf(unknown bool, tokens int) string {
	switch {
	case unknown && tokens == 0:
		return "unknown"
	case unknown:
		return cost(tokens) + " + unknown"
	}

	return cost(tokens)
}

// probedMsg reports that a probe of an MCP server ended, with its error.
type probedMsg struct{ err error }

// model is the Bubble Tea root model over a Session.
type model struct {
	style      styles
	s          *equip.Session
	flash      string
	cur        int  // the highlighted row
	server     int  // the highlighted MCP server among the highlighted plugin's contents
	inContents bool // the keys act on the highlighted MCP server, not the row
	quitting   bool // asking to quit with unsaved changes
}

// newTUI is the model of a fresh TUI over s.
func newTUI(s *equip.Session) *model {
	return &model{s: s, style: newStyles(), cur: 0, server: 0, inContents: false, quitting: false, flash: ""}
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if probed, ok := msg.(probedMsg); ok {
		m.flash = ""
		if probed.err != nil {
			m.flash = m.style.warn.Render("measure failed: " + probed.err.Error())
		}

		return m, nil
	}

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
		top = append(top, agent.String()+" "+costOf(session.Unknown[agent], session.Totals[agent]))
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

		list = append(list, mark+glyph(row.State)+" "+name+" "+m.style.dim.Render(costOf(row.CostUnknown, row.Cost)))
	}

	panes := []string{m.style.pane.Render(strings.Join(list, "\n"))}
	if m.cur < len(session.Rows) {
		row, server := session.Rows[m.cur], ""
		if m.inContents {
			server, _ = m.target(session.Rows, m.servers(session.Rows))
		}

		panes = append(panes, m.style.pane.Render(m.detail(row, m.s.Detail(row.Key), server)))
	}

	view := tea.NewView(strings.Join(top, "  ") + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, panes...) + "\n" +
		m.footer(session.Unsaved))
	view.AltScreen = true

	return view
}

// footer is the bottom line, with unsaved changes pending: the quit guard,
// the flash, or the keys.
func (m *model) footer(unsaved int) string {
	switch {
	case m.quitting:
		return m.style.warn.Render(fmt.Sprintf("%d unsaved changes. Quit without saving? y/n", unsaved))
	case m.flash != "":
		return m.flash
	case m.inContents:
		return m.style.dim.Render("↑↓ MCP server  1-2 set state  x drop override  m measure  tab list  s save  q quit")
	}

	return m.style.dim.Render("↑↓ move  1-3 set state  x drop override  m measure  tab MCP servers  s save  q quit")
}

// press acts on key on the main screen.
func (m *model) press(key string) tea.Cmd {
	rows := m.s.View().Rows
	servers := m.servers(rows)

	switch key {
	case "q", "ctrl+c":
		return m.quit()
	case "tab":
		m.inContents = !m.inContents && len(servers) > 0
		m.server = 0
	case "up", "k":
		m.move(-1, len(rows), len(servers))
	case "down", "j":
		m.move(1, len(rows), len(servers))
	case "1", "2", "3", "x", "m":
		if target, ok := m.target(rows, servers); ok {
			return m.act(key, target)
		}
	case "s":
		err := m.s.Save()
		if err != nil {
			m.flash = m.style.warn.Render("save failed: " + err.Error())
		}
	}

	return nil
}

// move moves the highlight by delta among the MCP servers in the contents,
// else among the rows; servers and rows count them.
func (m *model) move(delta, rows, servers int) {
	if m.inContents {
		m.server = max(min(m.server+delta, servers-1), 0)
	} else {
		m.cur = max(min(m.cur+delta, rows-1), 0)
	}
}

// servers are the keys of the MCP servers among the contents of the
// highlighted row of rows.
func (m *model) servers(rows []equip.Row) []string {
	if m.cur >= len(rows) {
		return nil
	}

	var keys []string

	for _, content := range m.s.Detail(rows[m.cur].Key).Contents {
		if content.Key != "" {
			keys = append(keys, content.Key)
		}
	}

	return keys
}

// target is the key the state keys act on: the highlighted MCP server in the
// contents, else the highlighted row of rows. It reports whether there is one.
func (m *model) target(rows []equip.Row, servers []string) (string, bool) {
	if m.inContents {
		return servers[m.server], true
	}

	if m.cur < len(rows) {
		return rows[m.cur].Key, true
	}

	return "", false
}

// act acts on the extension with target: x drops its Override, m measures
// its cost, and a number key sets the state it picks.
func (m *model) act(key, target string) tea.Cmd {
	switch key {
	case "x":
		m.s.DropOverride(target)
	case "m":
		// The probe runs as a command, so the TUI stays live meanwhile.
		probe := m.s.ProbeCost(target)
		m.flash = m.style.dim.Render("measuring " + target + "…")

		return func() tea.Msg { return probedMsg{err: probe()} }
	default:
		m.setState(target, int(key[0]-'1'))
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
// origin, the states to pick from, its contents with the MCP server with key
// server highlighted, and where it comes from.
func (m *model) detail(row equip.Row, ext equip.Detail, server string) string {
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

	lines = append(lines, costLine(row.CostUnknown, ext))

	if row.Override {
		lines = append(lines,
			"Origin  "+m.style.warn.Render("override")+", set by hand here",
			m.style.dim.Render("        without it: "+row.Fallback.String()+" (default)"))
	} else {
		lines = append(lines, "Origin  default")
	}

	if row.ChangedOutside {
		lines = append(lines, "        "+m.style.warn.Render("changed outside equip in "+ext.ChangedIn.String()))
	}

	lines = append(lines, "", "State")

	for index, state := range ext.States {
		radio := " "
		if state == row.State {
			radio = glyph(state)
		}

		lines = append(lines, fmt.Sprintf("  (%s) %d %s", radio, index+1, state))
	}

	lines = append(lines, m.contents(ext.Contents, server)...)
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

// costLine is the detail pane line of the cost in each agent of ext, which
// may be unknown.
func costLine(unknown bool, ext equip.Detail) string {
	costs := make([]string, 0, len(ext.Agents))
	for _, agent := range ext.Agents {
		costs = append(costs, agent.String()+" "+costOf(unknown, ext.Costs[agent]))
	}

	line := "Cost    " + strings.Join(costs, ", ")
	if ext.Hooks {
		line += " + hook output, unknown"
	}

	return line
}

// contents are the detail pane lines of a plugin's contents, with the MCP
// server with key highlighted.
func (m *model) contents(contents []equip.Content, key string) []string {
	if len(contents) == 0 {
		return nil
	}

	lines := []string{"", "Contents  " + m.style.dim.Render("follow the plugin unless overridden")}

	for _, content := range contents {
		mark, name, ovr := "  ", content.Name, ""
		if content.Key != "" && content.Key == key {
			mark = m.style.cur.Render("▸ ")
		}

		if content.Unsaved {
			name += m.style.warn.Render("*")
		}

		if content.Override {
			ovr = m.style.warn.Render(" ovr")
		}

		lines = append(lines, fmt.Sprintf("%s%s %s %s %s%s %s", mark, glyph(content.State), content.Kind, name,
			costOf(content.CostUnknown, content.Cost), ovr,
			m.style.dim.MaxWidth(shortDescriptionWidth).MaxHeight(1).Render(content.Description)))

		if content.ChangedOutside {
			lines = append(lines, "    "+m.style.warn.Render("changed outside equip in "+content.ChangedIn.String()))
		}
	}

	return lines
}
