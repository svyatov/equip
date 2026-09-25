// Package a is PROTOTYPE variant A, "Grouped list": one dense, full-width
// scrolling list with SKILLS / PLUGINS / MCP SERVERS sections and plugins that
// fold open inline as a tree. Throwaway.
package a

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/svyatov/equip/prototype/list-screen/data"
)

const window = 200_000 // context window the budget bar is drawn against

var sections = []struct {
	title string
	kind  data.Kind
}{{"SKILLS", data.Skill}, {"PLUGINS", data.Plugin}, {"MCP SERVERS", data.MCP}}

var (
	fg      = func(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }
	dim     = fg("#6c6c6c")
	faint   = fg("#4e4e4e")
	text    = fg("#d0d0d0")
	accent  = fg("#af87ff")
	bold    = text.Bold(true)
	onC     = fg("#5fd787")
	manC    = fg("#d7af5f")
	offC    = fg("#6c6c6c")
	ccC     = fg("#ff875f")
	cxC     = fg("#5fafd7")
	presetC = fg("#87afff")
	overC   = fg("#ffd75f")
	warnC   = fg("#ff5f5f")
)

type row struct {
	e    *data.Ext // nil for a section header
	sec  int
	last bool // last visible child of its plugin
}

type Model struct {
	s       *data.Store
	w, h    int
	sel     *data.Ext
	top     int
	open    map[*data.Ext]bool
	filter  string
	typing  bool
	confirm bool
	flash   string
}

func New(s *data.Store) *Model    { return &Model{s: s, open: map[*data.Ext]bool{}} }
func (m *Model) Name() string     { return "Grouped list" }
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

// rows is the visible list: section headers, top-level rows, open plugin contents.
// With a filter, a plugin whose contents match stays visible with those contents shown.
func (m *Model) rows() []row {
	q := strings.ToLower(m.filter)
	var out []row
	for si, sec := range sections {
		start := len(out)
		out = append(out, row{sec: si})
		for _, e := range m.s.Exts {
			if e.Kind != sec.kind {
				continue
			}
			var kids []*data.Ext
			if e.Match(q) {
				if m.open[e] {
					kids = e.Children
				}
			} else {
				for _, c := range e.Children {
					if c.Match(q) {
						kids = append(kids, c)
					}
				}
				if len(kids) == 0 {
					continue
				}
			}
			out = append(out, row{e: e, sec: si})
			for i, c := range kids {
				out = append(out, row{e: c, sec: si, last: i == len(kids)-1})
			}
		}
		if len(out) == start+1 && q != "" {
			out = out[:start] // hide empty sections while filtering
		}
	}
	return out
}

// cursor returns the selected row's index, reselecting the first extension if
// the selection is gone (filtered out, folded away). -1 when nothing is visible.
func (m *Model) cursor(rs []row) int {
	first := -1
	for i, r := range rs {
		if r.e == nil {
			continue
		}
		if r.e == m.sel {
			return i
		}
		if first < 0 {
			first = i
		}
	}
	m.sel = nil
	if first >= 0 {
		m.sel = rs[first].e
	}
	return first
}

func (m *Model) move(d int) {
	rs := m.rows()
	i := m.cursor(rs)
	if i < 0 {
		return
	}
	step := 1
	if d < 0 {
		step, d = -1, -d
	}
	for n := i + step; d > 0 && n >= 0 && n < len(rs); n += step {
		if rs[n].e != nil {
			m.sel, d = rs[n].e, d-1
		}
	}
}

func (m *Model) bodyH() int { return max(1, m.h-4) }

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	key := k.String()
	m.flash = ""

	if m.confirm {
		m.confirm = false
		switch key {
		case "y":
			return tea.Quit
		case "s":
			m.s.Save()
			return tea.Quit
		}
		m.flash = "quit cancelled"
		return nil
	}

	if m.typing {
		switch key {
		case "esc":
			m.filter, m.typing = "", false
		case "enter":
			m.typing = false
		case "backspace":
			if r := []rune(m.filter); len(r) > 0 {
				m.filter = string(r[:len(r)-1])
			}
		case "up", "down":
			m.move(map[string]int{"up": -1, "down": 1}[key])
		default:
			m.filter += k.Text
		}
		return nil
	}

	switch key {
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "pgup", "ctrl+u":
		m.move(-m.bodyH() / 2)
	case "pgdown", "ctrl+d":
		m.move(m.bodyH() / 2)
	case "home", "g":
		m.move(-1 << 20)
	case "end", "G":
		m.move(1 << 20)
	case "/":
		m.typing = true
	case "esc":
		m.filter = ""
	case "right", "l":
		m.fold(true)
	case "left", "h":
		m.fold(false)
	case "enter":
		if m.sel != nil && m.sel.Parent == nil {
			m.fold(!m.open[m.sel])
		}
	case "space", "1", "2", "3":
		m.setState(key)
	case "s":
		n := m.s.Save()
		m.flash = fmt.Sprintf("saved %d changes (prototype: nothing written)", n)
	case "q":
		if m.s.Unsaved() > 0 {
			m.confirm = true
			return nil
		}
		return tea.Quit
	}
	return nil
}

func (m *Model) fold(open bool) {
	m.cursor(m.rows())
	e := m.sel
	if e == nil {
		return
	}
	if e.Parent != nil {
		if !open {
			m.sel = e.Parent
			m.open[e.Parent] = false
		}
		return
	}
	if len(e.Children) > 0 {
		m.open[e] = open
	}
}

func (m *Model) setState(key string) {
	m.cursor(m.rows())
	e := m.sel
	if e == nil {
		return
	}
	if key == "space" {
		e.Cycle()
		return
	}
	s := map[string]data.State{"1": data.On, "2": data.Manual, "3": data.Off}[key]
	if !slices.Contains(e.States(), s) {
		m.flash = fmt.Sprintf("%s can't be manual-only: only skills have that state", e.Name)
		return
	}
	e.Set(s)
}

// ---- view ----

// fit truncates plain s to n cells with an ellipsis and pads it to n.
func fit(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) > n {
		r = append(r[:n-1], '…')
	}
	return string(r) + strings.Repeat(" ", n-len(r))
}

func rfit(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return strings.Repeat(" ", n-len(s)) + s
}

// line joins a left and right part with padding to exactly w cells.
func line(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return clamp(left, w) // no room for the right part
	}
	return left + strings.Repeat(" ", gap) + right
}

func clamp(s string, w int) string { return lipgloss.NewStyle().MaxWidth(w).Render(s) }

var glyphs = map[data.State]string{data.On: "●", data.Manual: "◐", data.Off: "○"}

func stateStyle(s data.State) lipgloss.Style {
	return map[data.State]lipgloss.Style{data.On: onC, data.Manual: manC, data.Off: offC}[s]
}

func glyph(s data.State) string { return stateStyle(s).Render(glyphs[s]) }

func stateWord(s data.State) string { return stateStyle(s).Render(fit(s.String(), 11)) }

func badges(a data.Agent) string {
	cc, cx := faint.Render("··"), faint.Render("··")
	if a&data.Claude != 0 {
		cc = ccC.Render("CC")
	}
	if a&data.Codex != 0 {
		cx = cxC.Render("CX")
	}
	return cc + " " + cx
}

func originMark(o string) string {
	switch {
	case strings.HasPrefix(o, "preset"):
		return presetC.Render("P")
	case o == "override":
		return overC.Render("✎")
	}
	return " "
}

func cost(n int) string {
	s := rfit(data.Tokens(n), 6)
	if n == 0 {
		return faint.Render(s)
	}
	return text.Render(s)
}

type cols struct{ name, src, desc int }

// Fixed: lead 6, agents 1+5, origin 1+1, state 1+11, cost 1+6.
const fixed = 6 + 6 + 2 + 12 + 7

func (m *Model) cols() cols {
	r := m.w - fixed
	c := cols{name: min(28, max(8, r))}
	r -= c.name
	if r > 8 {
		c.src = min(24, r-1)
		r -= c.src + 1
	}
	if r > 12 {
		c.desc = r - 1
		r = 0
	}
	c.name += max(0, r)
	return c
}

func (m *Model) renderRow(r row, cur bool, c cols) string {
	e := r.e
	ptr := "  "
	if cur {
		ptr = accent.Render("▌ ")
	}
	nameSt := text
	if cur {
		nameSt = bold
	}
	var b strings.Builder
	b.WriteString(ptr)
	if e.Parent != nil {
		tree := "├ "
		if r.last {
			tree = "└ "
		}
		// A plugin that is off loads none of its contents: dim them, keep them settable.
		muted := e.Parent.State != data.On
		g, word := glyph(e.State), stateWord(e.State)
		if muted {
			g, word = faint.Render(glyphs[e.State]), faint.Render(fit(e.State.String(), 11))
		}
		b.WriteString("  " + faint.Render(tree) + g + " ")
		kind := "skill"
		if e.Kind == data.MCP {
			kind = "mcp server"
		}
		switch {
		case muted && !cur:
			nameSt = faint
		case !cur:
			nameSt = dim
		}
		b.WriteString(nameSt.Render(fit(e.Name, c.name-2)))
		if c.src > 0 {
			b.WriteString(" " + faint.Render(fit(kind, c.src)))
		}
		if c.desc > 0 {
			b.WriteString(strings.Repeat(" ", c.desc+1))
		}
		b.WriteString(" " + badges(e.Agents) + " " + originMark(e.Origin) + " " + word + " " + cost(e.Cost()))
		return b.String()
	}
	mark := "  "
	name := e.Name
	if len(e.Children) > 0 {
		mark = dim.Render("▸ ")
		if m.open[e] || m.filter != "" && !e.Match(strings.ToLower(m.filter)) {
			mark = dim.Render("▾ ")
		}
		name = fmt.Sprintf("%s (%d)", e.Name, len(e.Children))
	}
	b.WriteString(glyph(e.State) + " " + mark + nameSt.Render(fit(name, c.name)))
	if c.src > 0 {
		b.WriteString(" " + dim.Render(fit(e.Source, c.src)))
	}
	if c.desc > 0 {
		b.WriteString(" " + faint.Render(fit(e.Desc, c.desc)))
	}
	b.WriteString(" " + badges(e.Agents) + " " + originMark(e.Origin) + " " + stateWord(e.State) + " " + cost(e.Cost()))
	return b.String()
}

func (m *Model) renderSection(si int, c cols) string {
	var n, on, man, sub, shown int
	q := strings.ToLower(m.filter)
	for _, e := range m.s.Exts {
		if e.Kind != sections[si].kind {
			continue
		}
		n++
		sub += e.Cost()
		switch e.State {
		case data.On:
			on++
		case data.Manual:
			man++
		}
		if e.Match(q) || slices.ContainsFunc(e.Children, func(c *data.Ext) bool { return c.Match(q) }) {
			shown++
		}
	}
	count := fmt.Sprint(n)
	if q != "" {
		count = fmt.Sprintf("%d of %d", shown, n)
	}
	stats := fmt.Sprintf(" %s · %d on", count, on)
	if sections[si].kind == data.Skill {
		stats += fmt.Sprintf(" · %d manual-only", man)
	}
	left := accent.Bold(true).Render(sections[si].title) + dim.Render(stats) + " "
	right := " " + rfit(data.Tokens(sub), 6)
	fill := m.w - lipgloss.Width(left) - lipgloss.Width(right)
	if fill < 0 {
		return clamp(left, m.w)
	}
	return left + faint.Render(strings.Repeat("─", fill)) + bold.Render(right)
}

func (m *Model) header(c cols) (string, string) {
	total := m.s.Total()
	frac := float64(total) / window
	const barW = 10
	filled := min(barW, int(frac*barW+0.5))
	barC := onC
	if frac > 0.25 {
		barC = manC
	}
	if frac > 0.5 {
		barC = warnC
	}
	bar := barC.Render(strings.Repeat("▰", filled)) + faint.Render(strings.Repeat("▱", barW-filled))
	right := dim.Render("session ") + bold.Render(data.Tokens(total)) + " " + bar + dim.Render(fmt.Sprintf(" %d%% of 200k", int(frac*100+0.5)))
	if lipgloss.Width(right)+60 > m.w { // narrow: keep the number, drop the words
		right = bold.Render(data.Tokens(total)) + " " + bar + dim.Render(fmt.Sprintf(" %d%%", int(frac*100+0.5)))
	}
	presets := "none"
	if len(m.s.Presets) > 0 {
		presets = strings.Join(m.s.Presets, ", ")
	}
	left := accent.Bold(true).Render("equip ") + text.Render(m.s.Project) + dim.Render("  preset ") + presetC.Render(presets)
	l1 := line(clamp(left, m.w-lipgloss.Width(right)-1), right, m.w)

	var l2 string
	if m.typing || m.filter != "" {
		caret := ""
		if m.typing {
			caret = accent.Render("▏")
		}
		hint := "enter keep · esc clear"
		if !m.typing {
			hint = "/ edit · esc clear"
		}
		l2 = accent.Render("/ ") + text.Render(m.filter) + caret + "  " + dim.Render(hint)
	} else {
		t := "  " + "  " + "  " + fit("NAME", c.name)
		if c.src > 0 {
			t += " " + fit("SOURCE", c.src)
		}
		if c.desc > 0 {
			t += " " + fit("DESCRIPTION", c.desc)
		}
		t += " AGENTS  " + fit("STATE", 11) + " " + rfit("COST", 6)
		l2 = faint.Render(t)
	}
	return l1, clamp(l2, m.w)
}

func (m *Model) detail() string {
	e := m.sel
	if e == nil {
		return dim.Render("  no extension matches the filter")
	}
	agents := map[data.Agent]string{data.Claude: "Claude Code", data.Codex: "Codex", data.Both: "Claude Code + Codex"}[e.Agents]
	var why string
	switch {
	case e.Parent != nil:
		why = e.State.String() + " by override, in plugin " + e.Parent.Name
		if e.Origin != "override" {
			why = e.State.String() + " by default, in plugin " + e.Parent.Name
		}
		if e.Parent.State != data.On {
			why += " (plugin is off, so nothing loads)"
		}
	case e.Origin == "override":
		why = e.State.String() + " by override in this project"
	case strings.HasPrefix(e.Origin, "preset"):
		why = e.State.String() + " from " + e.Origin
	default:
		why = "off: in no active preset"
	}
	s := fmt.Sprintf("  %s · %s · %s · %s", e.Name, e.Kind, agents, why)
	if e.Desc != "" {
		s += " · " + e.Desc
	}
	return clamp(dim.Render(s), m.w)
}

func (m *Model) help() string {
	switch {
	case m.confirm:
		return warnC.Render(fmt.Sprintf("  %d unsaved changes.", m.s.Unsaved())) + text.Render("  s save and quit · y quit without saving · any other key cancels")
	case m.flash != "":
		return overC.Render("  " + m.flash)
	}
	keys := [][2]string{{"space", "cycle"}, {"1-3", "set"}, {"←→", "fold"}, {"/", "filter"}, {"s", "save"}, {"q", "quit"}}
	var parts []string
	for _, k := range keys {
		parts = append(parts, text.Render(k[0])+" "+dim.Render(k[1]))
	}
	legend := onC.Render("●") + dim.Render(" on ") + manC.Render("◐") + dim.Render(" manual-only ") + offC.Render("○") + dim.Render(" off  ") +
		presetC.Render("P") + dim.Render(" preset ") + overC.Render("✎") + dim.Render(" override")
	return line("  "+strings.Join(parts, "  "), legend, m.w)
}

func (m *Model) View() string {
	if m.w < 20 || m.h < 5 {
		return clamp("equip: window too small", m.w)
	}
	c := m.cols()
	rs := m.rows()
	ci := m.cursor(rs)
	bh := m.bodyH()
	if ci >= 0 {
		if ci-1 >= 0 && rs[ci-1].e == nil && ci-1 < m.top {
			m.top = ci - 1 // keep a section's header in view with its first row
		}
		if ci < m.top {
			m.top = ci
		}
		if ci >= m.top+bh {
			m.top = ci - bh + 1
		}
	}
	m.top = max(0, min(m.top, len(rs)-bh))

	l1, l2 := m.header(c)
	out := []string{l1, l2}
	for i := m.top; i < m.top+bh; i++ {
		switch {
		case i >= len(rs):
			out = append(out, "")
		case rs[i].e == nil:
			out = append(out, m.renderSection(rs[i].sec, c))
		default:
			out = append(out, clamp(m.renderRow(rs[i], i == ci, c), m.w))
		}
	}
	out = append(out, m.detail(), m.help())
	return strings.Join(out, "\n")
}
