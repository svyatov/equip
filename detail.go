package main

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/svyatov/equip/internal/equip"
)

// The sizes of the detail pane's lines, in cells.
const (
	noteIndent = 8 // a note's under a field, past the field's label
	// A location's line's indent, and the gap between its agent and path.
	locationMarks = 4
	// A content's line's mark, glyph, the spaces between its columns and the
	// two before its description.
	contentMarks = 8
	nameShare    = 3 // a content's name takes at most 1 of this many parts of the width
)

// glyph is the list mark of st. All three are in the common coding fonts,
// such as JetBrains Mono, Menlo and Fira Code, so none falls back to a wider
// glyph of another font.
func glyph(st equip.State) string {
	return [...]string{equip.On: "●", equip.ManualOnly: "◉", equip.Off: "○"}[st]
}

// costOf shows a cost of tokens, or that it is unmeasured in whole or in
// part, as an MCP server's is until it is measured.
func costOf(unmeasured bool, tokens int) string {
	text := fmt.Sprintf("~%d", tokens)

	switch {
	case unmeasured && tokens == 0:
		return "unmeasured"
	case unmeasured:
		return text + " + unmeasured"
	}

	return text
}

// totalOf shows the total of a session in an agent in style, with the count
// of MCP servers it leaves out until they are measured, and marked when its
// skill listing passes the agent's listing budget.
func totalOf(total equip.Total, style styles) string {
	text := fmt.Sprintf("~%d", total.Tokens)
	if total.Unmeasured > 0 {
		text += style.unmeasured.Render(fmt.Sprintf(" + %d MCP unmeasured", total.Unmeasured))
	}

	if total.OverBudget {
		text += style.bad.Render(", skills over budget")
	}

	return text
}

// agentTotal is agent's name and its total, in style.
func agentTotal(agent equip.Agent, total equip.Total, style styles) string {
	return style.agent.Render(agent.String()) + " " + totalOf(total, style)
}

// field is a detail pane line of a label and its value.
func (m *model) field(label, value string) string {
	return m.style.dim.Render(fmt.Sprintf("%-7s", label)) + " " + value
}

// note is text in style under a detail pane field, wrapped at width cells
// past its indent.
func (m *model) note(text string, style lipgloss.Style, width int) []string {
	lines := strings.Split(style.Width(max(width-noteIndent, 1)).Render(text), "\n")
	for i := range lines {
		lines[i] = strings.Repeat(" ", noteIndent) + lines[i]
	}

	return lines
}

// origin is where the state of row comes from in view, with why it changed
// outside equip, its lines wrapped at width.
func (m *model) origin(view equip.View, row equip.Row, ext equip.Detail, width int) []string {
	if row.Plugin != "" {
		return []string{m.field("Origin", "follows "+row.Plugin)}
	}

	var lines []string

	origin := "default"
	if len(view.Presets) > 0 {
		origin = "presets"
	}

	if row.Override {
		lines = append(lines, m.field("Origin", m.style.warn.Render("override")+", set by hand here"))
		lines = append(lines, m.note("without it: "+row.Fallback.String()+" ("+origin+")", m.style.dim, width)...)
	} else {
		lines = append(lines, m.field("Origin", origin))
	}

	if row.ChangedOutside {
		lines = append(lines, m.note("changed outside equip in "+ext.ChangedIn.String(), m.style.warn, width)...)
	}

	if ext.Note != "" {
		lines = append(lines, m.note(ext.Note, m.style.warn, width)...)
	}

	return lines
}

// detailTitle is the title of the detail pane of row: its name and kind.
func (m *model) detailTitle(row equip.Row) string {
	return m.style.cur.Render(row.Name) + m.style.dim.Render(" · ") + m.style.kinds[row.Kind].Render(row.Kind.String())
}

// detail is the detail pane of row in view, width by height cells: what it
// is, which agents have it, its origin, the states to pick from, its contents
// with the MCP server with key server highlighted, and where it comes from.
// It scrolls to keep the highlighted MCP server in. The presets workspace
// leaves out the states, which its keys do not set.
func (m *model) detail(view equip.View, row equip.Row, ext equip.Detail, server string, width, height int) string {
	agents := make([]string, 0, len(ext.Agents))
	for _, agent := range ext.Agents {
		agents = append(agents, m.style.agent.Render(agent.String()))
	}

	var lines []string
	if ext.Description != "" {
		lines = append(strings.Split(m.style.dim.Width(width).Render(ext.Description), "\n"), "")
	}

	if ext.Marketplace != "" {
		lines = append(lines, m.field("From", ext.Marketplace+m.style.dim.Render(" marketplace")))
	}

	lines = append(lines, m.field("Agents", strings.Join(agents, ", ")))

	for _, agent := range ext.Agents {
		if reason, ok := ext.NotApplied[agent]; ok {
			lines = append(lines, m.note("not applied in "+agent.String()+": "+reason, m.style.dim, width)...)
		}
	}

	lines = append(lines, m.costLine(row, ext))
	if ext.Unmeasurable != "" {
		lines = append(lines, m.note("not measured: "+ext.Unmeasurable, m.style.dim, width)...)
	}

	lines = append(lines, m.origin(view, row, ext, width)...)
	lines = append(lines, m.states(row, ext)...)

	contents, highlighted := m.contents(ext.Contents, server, width)
	if highlighted >= 0 {
		highlighted += len(lines)
	}

	lines = append(lines, contents...)
	lines = append(lines, m.locations(ext, width)...)

	return strings.Join(m.scrollDetail(lines, highlighted, height), "\n")
}

// states are the detail pane lines of the states of ext to pick from, with
// the one of row marked. The presets workspace leaves them out, as its keys
// do not set them, and so does a plugin's skill, which has none.
func (m *model) states(row equip.Row, ext equip.Detail) []string {
	if m.ws.open || len(ext.States) == 0 {
		return nil
	}

	lines := []string{"", m.style.head.Render("State")}

	for index, state := range ext.States {
		radio := " "
		if state == row.State {
			radio = m.glyph(state)
		}

		lines = append(lines, fmt.Sprintf("  (%s) %s %s", radio, m.style.key.Render(strconv.Itoa(index+1)),
			m.style.states[state].Render(state.String())))
	}

	return lines
}

// scrollDetail is the height of the detail pane's lines it shows: kept on
// the line highlighted, or scrolled as the keys move it while they are on
// it, else from the top.
func (m *model) scrollDetail(lines []string, highlighted, height int) []string {
	scrolls := m.focus == onDetail && !m.ws.open
	top, cur := 0, 0

	switch {
	case highlighted >= 0:
		top, cur = m.detailTop, highlighted
	case scrolls:
		// The line under the one that may say how many more are above.
		top, cur = m.detailTop, m.detailTop+1
	}

	lines, top = m.window(lines, top, cur, height)
	if highlighted >= 0 || scrolls {
		m.detailTop = top
	}

	return lines
}

// locations are the detail pane lines of where ext comes from, each path cut
// from its start to fit width.
func (m *model) locations(ext equip.Detail, width int) []string {
	lines := []string{"", m.style.head.Render("Locations")}
	if ext.BuiltIn {
		lines = append(lines, "  "+m.style.dim.Render("built into Claude Code"))
	}

	agentWidth := 0
	for _, loc := range ext.Locations {
		agentWidth = max(agentWidth, len(loc.Agent.String()))
	}

	for _, loc := range ext.Locations {
		lines = append(lines, "  "+m.style.agent.Render(fmt.Sprintf("%-*s", agentWidth, loc.Agent.String()))+"  "+
			cutStart(tilde(loc.Path, m.home), width-agentWidth-locationMarks))
	}

	return lines
}

// costLine is the detail pane line of the cost in each agent of row, which
// may be unmeasured. A By-name skill costs none.
func (m *model) costLine(row equip.Row, ext equip.Detail) string {
	if row.ByName {
		return m.field("Cost", "none, called by name only")
	}

	costs := make([]string, 0, len(ext.Agents))
	for _, agent := range ext.Agents {
		costs = append(costs, m.style.agent.Render(agent.String())+" "+costOf(row.CostUnknown, ext.Costs[agent]))
	}

	line := strings.Join(costs, ", ")
	if ext.Hooks {
		line += " + hook output, unknown"
	}

	return m.field("Cost", line)
}

// contents are the detail pane lines of a plugin's contents, width cells, in
// columns, with the MCP server with key highlighted, and the index of its
// line; -1 with none.
func (m *model) contents(contents []equip.Content, key string, width int) ([]string, int) {
	if len(contents) == 0 {
		return nil, -1
	}

	lines := []string{
		"", m.style.head.Render("Contents") + "  " +
			m.style.dim.Render("skills follow the plugin, MCP servers too unless overridden"),
	}
	highlighted := -1

	names, costs := make([]string, len(contents)), make([]string, len(contents))
	kindWidth, nameWidth, costWidth := 0, 0, 0

	for i, content := range contents {
		names[i] = m.contentName(content)
		costs[i] = m.costCell(content.State, content.ByName, content.CostUnknown, content.Cost)
		kindWidth = max(kindWidth, len(content.Kind.String()))
		nameWidth = max(nameWidth, lipgloss.Width(names[i]))
		costWidth = max(costWidth, lipgloss.Width(costs[i]))
	}

	nameWidth = min(nameWidth, width/nameShare)
	rest := width - contentMarks - kindWidth - nameWidth - costWidth // the description's

	for index, content := range contents {
		isHighlighted := content.Key != "" && content.Key == key
		if isHighlighted {
			highlighted = len(lines)
		}

		cost := costs[index]

		line := m.mark(isHighlighted) + m.glyph(content.State) + " " +
			m.style.kinds[content.Kind].Render(fmt.Sprintf("%-*s", kindWidth, content.Kind.String())) + " " +
			fit(names[index], nameWidth) + " " + strings.Repeat(" ", costWidth-lipgloss.Width(cost)) + cost
		if first, _, _ := strings.Cut(content.Description, "\n"); rest > 0 && first != "" {
			line += "  " + m.style.dim.Render(cut(first, rest))
		}

		lines = append(lines, line)

		if content.ChangedOutside {
			lines = append(lines, m.note("changed outside equip in "+content.ChangedIn.String(), m.style.warn, width)...)
		}

		if content.Unmeasurable != "" {
			lines = append(lines, m.note("not measured: "+content.Unmeasurable, m.style.dim, width)...)
		}
	}

	return lines, highlighted
}

// contentName is the name of a plugin's content, marked when unsaved or an
// Override.
func (m *model) contentName(content equip.Content) string {
	name := content.Name
	if content.Unsaved {
		name += m.style.warn.Render("*")
	}

	if content.Override {
		name += m.style.warn.Render(" ovr")
	}

	return name
}
