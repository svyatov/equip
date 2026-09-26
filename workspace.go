package main

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/svyatov/equip/internal/equip"
)

// openWorkspace opens the presets workspace, which compares what the keys
// change there with the view at opening.
func (m *model) openWorkspace() {
	m.workspace, m.preset, m.before = true, 0, m.s.View()
}

// inWorkspace acts on key in the presets workspace: the arrows move among
// the presets, space makes the highlighted one active here or not, and esc
// goes back to the main screen.
func (m *model) inWorkspace(key string) {
	presets := m.s.Presets()

	if delta, ok := step(key); ok {
		m.preset = max(min(m.preset+delta, len(presets)-1), 0)
	}

	switch key {
	case "space":
		if m.preset >= len(presets) {
			return
		}

		var active []string

		for _, preset := range presets {
			if preset.Active != (preset.ID == presets[m.preset].ID) {
				active = append(active, preset.ID)
			}
		}

		m.s.SetPresets(active)
	case escKey:
		m.workspace = false
	}
}

// workspaceView is the presets workspace: the library and the highlighted
// preset's members.
func (m *model) workspaceView() string {
	presets := m.s.Presets()
	panes := []string{m.style.pane.Render(m.library(presets))}

	if m.preset < len(presets) {
		panes = append(panes, m.style.pane.Render(m.members(presets[m.preset])))
	}

	footer := m.style.dim.Render("↑↓ preset  space active here  esc back")
	if m.quitting {
		footer = m.footer(m.s.View().Unsaved)
	}

	return m.workspaceTop() + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, panes...) + "\n" + footer
}

// workspaceTop is the workspace's top line: the active presets, and each
// agent's session total with how many extensions turn on and off since the
// workspace opened.
func (m *model) workspaceTop() string {
	now := m.s.View()

	active := m.style.dim.Render("none (agent defaults)")
	if len(now.Presets) > 0 {
		active = strings.Join(now.Presets, " + ")
	}

	turned := turned(m.before, now)
	changed := turned[equip.On]+turned[equip.Off] > 0
	top := []string{m.style.top.Render("equip presets"), "active here: " + active}

	for _, agent := range equip.Agents() {
		total := costOf(now.Unknown[agent], now.Totals[agent])
		if changed {
			total = costOf(m.before.Unknown[agent], m.before.Totals[agent]) + " → " + total
		}

		top = append(top, agent.String()+" "+total)
	}

	if changed {
		top = append(top, m.style.warn.Render(
			fmt.Sprintf("%d on, %d off, unsaved, s on main", turned[equip.On], turned[equip.Off])))
	}

	return strings.Join(top, "  ")
}

// turned counts the rows that turned into each state between the views
// before and now.
func turned(before, now equip.View) map[equip.State]int {
	counts := map[equip.State]int{}

	for _, row := range now.Rows {
		if i := index(before.Rows, row.Key); i >= 0 && before.Rows[i].State != row.State {
			counts[row.State]++
		}
	}

	return counts
}

// library is the library pane: each preset with whether it is active here,
// its member count and its project count.
func (m *model) library(presets []equip.Preset) string {
	lines := []string{"Library", m.style.dim.Render("  here name            ext proj")}
	if len(presets) == 0 {
		lines = append(lines, m.style.dim.Render("  no presets in ~/.config/equip/presets"))
	}

	for i, preset := range presets {
		mark, check := "  ", "[ ]"
		if i == m.preset {
			mark = m.style.cur.Render("▸ ")
		}

		if preset.Active {
			check = "[x]"
		}

		lines = append(lines, fmt.Sprintf("%s%s %-15s %3d %4d", mark, check, preset.Name, len(preset.Members),
			preset.Projects))
	}

	return strings.Join(lines, "\n")
}

// members is the members pane of preset, grouped by kind. A member not
// installed here shows greyed.
func (m *model) members(preset equip.Preset) string {
	lines := []string{"Members of " + preset.Name}

	for i, member := range preset.Members {
		if i == 0 || member.Kind != preset.Members[i-1].Kind {
			lines = append(lines, "", m.style.dim.Render(member.Kind.String()))
		}

		if member.Installed {
			lines = append(lines, "  "+member.Name)
		} else {
			lines = append(lines, m.style.dim.Render("  "+member.Name+"  not installed"))
		}
	}

	return strings.Join(lines, "\n")
}
