package main

import (
	"fmt"
	"strings"

	"github.com/svyatov/equip/internal/equip"
)

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

// totalOf shows the total of a session in an agent, marked when its skill
// listing passes the agent's listing budget.
func totalOf(total equip.Total) string {
	text := costOf(total.Unknown, total.Tokens)
	if total.OverBudget {
		text += " over budget"
	}

	return text
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
			"Origin  "+m.style.warn.Render("override")+", set by hand here",
			m.style.dim.Render("        without it: "+row.Fallback.String()+" ("+origin+")"))
	} else {
		lines = append(lines, "Origin  "+origin)
	}

	if row.ChangedOutside {
		lines = append(lines, "        "+m.style.warn.Render("changed outside equip in "+ext.ChangedIn.String()))
	}

	if ext.Note != "" {
		lines = append(lines, "        "+m.style.warn.Render(ext.Note))
	}

	return lines
}

// detail is the detail pane of row in view: what it is, which agents have it,
// its origin, the states to pick from, its contents with the MCP server with
// key server highlighted, and where it comes from.
func (m *model) detail(view equip.View, row equip.Row, ext equip.Detail, server string) string {
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
	lines = append(lines, m.origin(view, row, ext)...)
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
		name, ovr := content.Name, ""
		if content.Unsaved {
			name += m.style.warn.Render("*")
		}

		if content.Override {
			ovr = m.style.warn.Render(" ovr")
		}

		lines = append(lines, fmt.Sprintf("%s%s %s %s %s%s %s", m.mark(content.Key != "" && content.Key == key),
			glyph(content.State), content.Kind, name, costOf(content.CostUnknown, content.Cost), ovr,
			m.style.dim.MaxWidth(shortDescriptionWidth).MaxHeight(1).Render(content.Description)))

		if content.ChangedOutside {
			lines = append(lines, "    "+m.style.warn.Render("changed outside equip in "+content.ChangedIn.String()))
		}
	}

	return lines
}
