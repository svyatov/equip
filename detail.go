package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/svyatov/equip/internal/equip"
)

// shortDescriptionWidth is the width the detail pane cuts the description of
// a plugin's skill at.
const shortDescriptionWidth = 40

// glyph is the list mark of st.
func glyph(st equip.State) string {
	return [...]string{equip.On: "●", equip.ManualOnly: "◐", equip.Off: "○"}[st]
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

// origin is where the state of row comes from in view, with why it changed
// outside equip.
func (m *model) origin(view equip.View, row equip.Row, ext equip.Detail) []string {
	var lines []string

	origin := "default"
	if len(view.Presets) > 0 {
		origin = "presets"
	}

	if row.Override {
		lines = append(lines,
			m.field("Origin", m.style.warn.Render("override")+", set by hand here"),
			m.style.dim.Render("        without it: "+row.Fallback.String()+" ("+origin+")"))
	} else {
		lines = append(lines, m.field("Origin", origin))
	}

	if row.ChangedOutside {
		lines = append(lines, "        "+m.style.warn.Render("changed outside equip in "+ext.ChangedIn.String()))
	}

	if ext.Note != "" {
		lines = append(lines, "        "+m.style.warn.Render(ext.Note))
	}

	return lines
}

// detail is the detail pane of row in view, width by height cells: what it
// is, which agents have it, its origin, the states to pick from, its contents
// with the MCP server with key server highlighted, and where it comes from.
// Under the title, it scrolls to keep the highlighted MCP server in.
func (m *model) detail(view equip.View, row equip.Row, ext equip.Detail, server string, width, height int) string {
	agents := make([]string, 0, len(ext.Agents))
	for _, agent := range ext.Agents {
		agents = append(agents, m.style.agent.Render(agent.String()))
	}

	lines := append(strings.Split(m.style.dim.Width(width).Render(ext.Description), "\n"), "")

	if ext.Marketplace != "" {
		lines = append(lines, m.style.dim.Render("Marketplace")+"  "+ext.Marketplace)
	}

	lines = append(lines, m.field("Agents", strings.Join(agents, ", ")))

	for _, agent := range ext.Agents {
		if reason, ok := ext.NotApplied[agent]; ok {
			lines = append(lines, "        "+m.style.dim.Render("not applied in "+agent.String()+": "+reason))
		}
	}

	lines = append(lines, m.costLine(row.CostUnknown, ext))
	lines = append(lines, m.origin(view, row, ext)...)
	lines = append(lines, "", m.style.head.Render("State"))

	for index, state := range ext.States {
		radio := " "
		if state == row.State {
			radio = m.glyph(state)
		}

		lines = append(lines, fmt.Sprintf("  (%s) %s %s", radio, m.style.key.Render(strconv.Itoa(index+1)),
			m.style.states[state].Render(state.String())))
	}

	contents, highlighted := m.contents(ext.Contents, server)
	if highlighted >= 0 {
		highlighted += len(lines)
	}

	lines = append(lines, contents...)
	lines = append(lines, m.locations(ext)...)

	top := 0
	if highlighted >= 0 {
		top = m.detailTop
	}

	lines, top = m.window(lines, top, max(highlighted, 0), height-1)
	if highlighted >= 0 {
		m.detailTop = top
	}

	return m.style.cur.Render(row.Name) + "  " + m.style.kinds[row.Kind].Render(row.Kind.String()) + "\n" +
		strings.Join(lines, "\n")
}

// locations are the detail pane lines of where ext comes from.
func (m *model) locations(ext equip.Detail) []string {
	lines := []string{"", m.style.head.Render("Locations")}
	if ext.BuiltIn {
		lines = append(lines, "  "+m.style.dim.Render("built into Claude Code"))
	}

	for _, loc := range ext.Locations {
		lines = append(lines, "  "+loc.Path+"  "+m.style.agent.Render(loc.Agent.String()))
	}

	return lines
}

// costLine is the detail pane line of the cost in each agent of ext, which
// may be unmeasured.
func (m *model) costLine(unmeasured bool, ext equip.Detail) string {
	costs := make([]string, 0, len(ext.Agents))
	for _, agent := range ext.Agents {
		costs = append(costs, m.style.agent.Render(agent.String())+" "+costOf(unmeasured, ext.Costs[agent]))
	}

	line := strings.Join(costs, ", ")
	if ext.Hooks {
		line += " + hook output, unknown"
	}

	return m.field("Cost", line)
}

// contents are the detail pane lines of a plugin's contents, with the MCP
// server with key highlighted, and the index of its line; -1 with none.
func (m *model) contents(contents []equip.Content, key string) ([]string, int) {
	if len(contents) == 0 {
		return nil, -1
	}

	lines := []string{
		"", m.style.head.Render("Contents") + "  " +
			m.style.dim.Render("skills follow the plugin, MCP servers too unless overridden"),
	}
	highlighted := -1

	for _, content := range contents {
		isHighlighted := content.Key != "" && content.Key == key
		if isHighlighted {
			highlighted = len(lines)
		}

		name, ovr := content.Name, ""
		if content.Unsaved {
			name += m.style.warn.Render("*")
		}

		if content.Override {
			ovr = m.style.warn.Render(" ovr")
		}

		lines = append(lines, fmt.Sprintf("%s%s %s %s %s%s %s", m.mark(isHighlighted),
			m.glyph(content.State), m.style.kinds[content.Kind].Render(content.Kind.String()), name,
			m.style.dim.Render(costOf(content.CostUnknown, content.Cost)), ovr,
			m.style.dim.MaxWidth(shortDescriptionWidth).MaxHeight(1).Render(content.Description)))

		if content.ChangedOutside {
			lines = append(lines, "    "+m.style.warn.Render("changed outside equip in "+content.ChangedIn.String()))
		}
	}

	return lines, highlighted
}
