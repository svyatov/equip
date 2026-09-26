package main

import (
	"fmt"
	"slices"
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
	query      string // the search: the list keeps the rows whose names contain it
	cur        int    // the highlighted row
	facet      int    // the picked facet
	server     int    // the highlighted MCP server among the highlighted plugin's contents
	inContents bool   // the keys act on the highlighted MCP server, not the row
	quitting   bool   // asking to quit with unsaved changes
	searching  bool   // the keys type into the search
}

// newTUI is the model of a fresh TUI over s.
func newTUI(s *equip.Session) *model {
	return &model{
		s: s, style: newStyles(), cur: 0, facet: 0, server: 0, inContents: false, quitting: false, searching: false,
		flash: "", query: "",
	}
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

	var cmd tea.Cmd

	switch {
	case m.searching:
		m.search(keyMsg)
	case !m.narrow(key):
		cmd = m.press(key)
	}

	m.clamp()

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

	rows := m.rows(session)

	panes := []string{m.style.pane.Render(m.sidebar(session.Facets)), m.style.pane.Render(m.list(rows))}
	if m.cur < len(rows) {
		row, server := rows[m.cur], ""
		if m.inContents {
			server, _ = m.target(rows, m.servers(rows))
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
	case m.searching:
		return m.style.dim.Render("type to search  enter done  esc clear")
	case m.inContents:
		return m.style.dim.Render("↑↓ MCP server  1-2 set state  x drop override  m measure  tab list  s save  q quit")
	}

	return m.style.dim.Render(
		"↑↓ move  [ ] facet  / search  1-3 set state  x drop override  m measure  tab MCP servers  s save  q quit")
}

// sidebar is the facet sidebar, with the picked one of facets highlighted.
func (m *model) sidebar(facets []equip.Facet) string {
	lines := make([]string, 0, len(facets))

	for index, facet := range facets {
		// A blank line before the first facet of the agents, of the states
		// and of the changes.
		if slices.Contains([]int{4, 6, 9}, index) {
			lines = append(lines, "")
		}

		label := fmt.Sprintf("%-16s %3d", facet.Name, facet.Count())
		if index == m.facet {
			lines = append(lines, m.style.cur.Render("▸ "+label))
		} else {
			lines = append(lines, "  "+label)
		}
	}

	return strings.Join(lines, "\n")
}

// list is the list of rows, under the search when there is one.
func (m *model) list(rows []equip.Row) string {
	lines := make([]string, 0, len(rows)+1)
	if m.searching || m.query != "" {
		lines = append(lines, m.style.cur.Render("/"+m.query))
	}

	for i, row := range rows {
		mark, name := "  ", row.Name
		if i == m.cur {
			mark, name = m.style.cur.Render("▸ "), m.style.cur.Render(name)
		}

		if row.Unsaved {
			name += m.style.warn.Render("*")
		}

		lines = append(lines, mark+glyph(row.State)+" "+name+" "+m.style.dim.Render(costOf(row.CostUnknown, row.Cost)))
	}

	return strings.Join(lines, "\n")
}

// search acts on keyMsg while the keys type into the search: enter ends it,
// and esc clears it too.
func (m *model) search(keyMsg tea.KeyPressMsg) {
	switch keyMsg.String() {
	case "enter":
		m.searching = false
	case "esc":
		m.searching, m.query = false, ""
	case "backspace":
		query := []rune(m.query)
		m.query = string(query[:max(len(query)-1, 0)])
	default:
		m.query, m.cur = m.query+keyMsg.Text, 0
	}
}

// clamp keeps the highlight on the rows, and leaves the contents with no MCP
// server left to highlight there, as when a row left the picked facet.
func (m *model) clamp() {
	rows := m.rows(m.s.View())
	m.cur = max(min(m.cur, len(rows)-1), 0)
	servers := m.servers(rows)
	m.server = max(min(m.server, len(servers)-1), 0)
	m.inContents = m.inContents && len(servers) > 0
}

// rows are the rows of session the picked facet and the search keep.
func (m *model) rows(session equip.View) []equip.Row {
	facet, query := session.Facets[m.facet], strings.ToLower(m.query)

	var rows []equip.Row

	for _, row := range session.Rows {
		if facet.Has(row) && strings.Contains(strings.ToLower(row.Name), query) {
			rows = append(rows, row)
		}
	}

	return rows
}

// press acts on key on the main screen.
func (m *model) press(key string) tea.Cmd {
	rows := m.rows(m.s.View())
	servers := m.servers(rows)

	switch key {
	case "q", "ctrl+c":
		return m.quit()
	case "tab":
		m.inContents = !m.inContents && len(servers) > 0
		m.server = 0
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
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

// narrow acts on key if it narrows the list: / starts the search, esc clears
// it, and [ and ] pick the previous and next facet. It reports whether it
// did.
func (m *model) narrow(key string) bool {
	facets := len(m.s.View().Facets)

	switch key {
	case "/":
		m.searching = true
	case "esc":
		m.query = ""
	case "[", "]":
		m.facet = (m.facet + map[string]int{"[": -1, "]": 1}[key] + facets) % facets
		m.cur, m.server, m.inContents = 0, 0, false
	default:
		return false
	}

	return true
}

// move moves the highlight by delta among the MCP servers in the contents,
// else among the rows. clamp keeps it on them.
func (m *model) move(delta int) {
	if m.inContents {
		m.server += delta
	} else {
		m.cur += delta
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
