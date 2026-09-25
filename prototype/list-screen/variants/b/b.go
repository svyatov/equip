// Package b is a PROTOTYPE variant, throwaway: master-detail. A facet sidebar
// filters a lean list; a detail pane shows and sets the highlighted extension.
package b

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/svyatov/equip/prototype/list-screen/data"
)

const (
	paneFacets = iota
	paneList
	paneDetail
)

// A deep facet also lists matching plugin contents as their own rows.
type facet struct {
	name string
	deep bool
	ok   func(m *Model, e *data.Ext) bool
}

var facets = []facet{
	{"All", false, func(*Model, *data.Ext) bool { return true }},
	{"Skills", false, func(_ *Model, e *data.Ext) bool { return e.Kind == data.Skill }},
	{"Plugins", false, func(_ *Model, e *data.Ext) bool { return e.Kind == data.Plugin }},
	{"MCP servers", false, func(_ *Model, e *data.Ext) bool { return e.Kind == data.MCP }},
	{"Claude Code only", false, func(_ *Model, e *data.Ext) bool { return e.Agents == data.Claude }},
	{"Codex only", false, func(_ *Model, e *data.Ext) bool { return e.Agents == data.Codex }},
	{"On", false, func(_ *Model, e *data.Ext) bool { return e.State == data.On }},
	{"Manual-only", true, func(_ *Model, e *data.Ext) bool { return e.State == data.Manual }},
	{"Off", false, func(_ *Model, e *data.Ext) bool { return e.State == data.Off }},
	{"Overrides", true, func(_ *Model, e *data.Ext) bool { return e.Origin == "override" }},
	{"Unsaved changes", true, func(m *Model, e *data.Ext) bool { return m.base[e] != e.State }},
}

// a blank line goes before these facet indexes in the sidebar
var facetGap = map[int]bool{4: true, 6: true, 9: true}

var (
	cAccent = lipgloss.Color("#af87ff")
	cOn     = lipgloss.Color("#5fd75f")
	cManual = lipgloss.Color("#d7af5f")
	cOff    = lipgloss.Color("#6c6c6c")
	cDim    = lipgloss.Color("#8a8a8a")
	cBorder = lipgloss.Color("#444444")

	sAccent = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sBold   = lipgloss.NewStyle().Bold(true)
	sDim    = lipgloss.NewStyle().Foreground(cDim)
	sKey    = lipgloss.NewStyle().Foreground(cAccent)
	sWarn   = lipgloss.NewStyle().Foreground(cManual).Bold(true)
	sOK     = lipgloss.NewStyle().Foreground(cOn)
	sState  = map[data.State]lipgloss.Style{
		data.On:     lipgloss.NewStyle().Foreground(cOn),
		data.Manual: lipgloss.NewStyle().Foreground(cManual),
		data.Off:    lipgloss.NewStyle().Foreground(cOff),
	}
	glyph = map[data.State]string{data.On: "●", data.Manual: "◐", data.Off: "○"}
)

type Model struct {
	s                *data.Store
	w, h             int
	focus            int
	facet, cur, top  int
	dtop, child      int // child: highlighted plugin content row in the detail pane
	query            string
	typing, quitting bool
	flash            string
	base             map[*data.Ext]data.State // states at last save
}

func New(s *data.Store) *Model {
	m := &Model{s: s, focus: paneList}
	m.snapshot()
	return m
}

func (m *Model) Name() string     { return "Master-detail" }
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

// ponytail: Store keeps its baseline private, so this mirrors it and resyncs
// whenever the store reports nothing unsaved (e.g. another variant saved).
func (m *Model) snapshot() {
	m.base = map[*data.Ext]data.State{}
	for _, e := range m.s.All() {
		m.base[e] = e.State
	}
}

func (m *Model) narrow() bool { return m.w < 100 }

func (m *Model) dirty(e *data.Ext) bool { return m.base[e] != e.State }

// anyOf reports whether f holds for e or one of its plugin contents.
func anyOf(e *data.Ext, f func(*data.Ext) bool) bool {
	return f(e) || slices.ContainsFunc(e.Children, f)
}

// target is what space and 1-3 act on: the highlighted plugin content row when
// the detail pane has focus, otherwise the highlighted extension.
func (m *Model) target(e *data.Ext) *data.Ext {
	if e != nil && m.focus == paneDetail && len(e.Children) > 0 {
		m.child = max(0, min(m.child, len(e.Children)-1))
		return e.Children[m.child]
	}
	return e
}

func (m *Model) visible() []*data.Ext { return m.rows(facets[m.facet], strings.ToLower(m.query)) }

func (m *Model) rows(f facet, q string) []*data.Ext {
	var out []*data.Ext
	for _, e := range m.s.Exts {
		if f.ok(m, e) && anyOf(e, func(x *data.Ext) bool { return x.Match(q) }) {
			out = append(out, e)
		}
		for _, c := range e.Children {
			if f.deep && f.ok(m, c) && c.Match(q) {
				out = append(out, c)
			}
		}
	}
	slices.SortStableFunc(out, func(a, b *data.Ext) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// selected clamps the cursor and returns the highlighted extension, or nil.
func (m *Model) selected(vis []*data.Ext) *data.Ext {
	m.cur = max(0, min(m.cur, len(vis)-1))
	if len(vis) == 0 {
		return nil
	}
	return vis[m.cur]
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	if m.s.Unsaved() == 0 {
		m.snapshot()
	}
	m.flash = ""
	key := k.String()

	if m.quitting {
		m.quitting = false
		if key == "y" {
			return tea.Quit
		}
		return nil
	}
	if m.typing {
		switch key {
		case "esc":
			m.query, m.typing = "", false
		case "enter", "tab":
			m.typing = false
		case "backspace":
			if r := []rune(m.query); len(r) > 0 {
				m.query = string(r[:len(r)-1])
			}
		case "up", "down":
			m.move(key)
		default:
			if k.Text != "" {
				m.query += k.Text
				m.cur, m.top, m.dtop, m.child = 0, 0, 0, 0
			}
		}
		return nil
	}

	e := m.target(m.selected(m.visible()))
	switch key {
	case "q":
		if m.s.Unsaved() > 0 {
			m.quitting = true
			return nil
		}
		return tea.Quit
	case "esc":
		m.query = ""
	case "/":
		m.typing, m.focus = true, paneList
	case "tab", "right", "l":
		m.cycleFocus(1)
	case "shift+tab", "left", "h":
		m.cycleFocus(-1)
	case "enter":
		if m.focus == paneFacets {
			m.focus = paneList
		}
	case "[", "]":
		m.setFacet(map[string]int{"[": -1, "]": 1}[key])
	case "p":
		m.flash = sWarn.Render("preset picker and editor: designed in the next ticket")
	case "s":
		n := m.s.Save()
		m.snapshot()
		m.flash = sOK.Render(fmt.Sprintf("saved %d changes (prototype: nothing written)", n))
	case "up", "k", "down", "j", "pgup", "pgdown", "home", "end":
		m.move(key)
	case "space":
		if e != nil && m.focus != paneFacets {
			e.Cycle()
		}
	case "1", "2", "3":
		if e == nil || m.focus == paneFacets {
			break
		}
		if ss := e.States(); int(key[0]-'1') < len(ss) {
			e.Set(ss[key[0]-'1'])
		}
	}
	return nil
}

func (m *Model) cycleFocus(d int) {
	lo := paneFacets
	if m.narrow() {
		lo = paneList
	}
	n := paneDetail - lo + 1
	m.focus = lo + ((m.focus-lo+d)%n+n)%n
}

func (m *Model) setFacet(d int) {
	m.facet = (m.facet + d + len(facets)) % len(facets)
	m.cur, m.top, m.dtop, m.child = 0, 0, 0, 0
}

func (m *Model) move(key string) {
	d := map[string]int{"up": -1, "k": -1, "down": 1, "j": 1, "pgup": -10, "pgdown": 10, "home": -1 << 20, "end": 1 << 20}[key]
	switch m.focus {
	case paneFacets:
		m.facet = max(0, min(len(facets)-1, m.facet+d))
		m.cur, m.top, m.dtop, m.child = 0, 0, 0, 0
	case paneList:
		m.cur += d
		m.selected(m.visible())
		m.dtop, m.child = 0, 0
	case paneDetail:
		if e := m.selected(m.visible()); e != nil && len(e.Children) > 0 {
			m.child = max(0, min(len(e.Children)-1, m.child+d))
		} else {
			m.dtop = max(0, m.dtop+d) // clamped in View
		}
	}
}

// ---- view ----

func (m *Model) View() string {
	if m.w < 40 || m.h < 8 {
		return block([]string{"terminal too small"}, max(m.w, 0), max(m.h, 0))
	}
	vis := m.visible()
	e := m.selected(vis)
	ph := m.h - 4

	var panes []string
	rest := m.w
	if !m.narrow() {
		sideW := 26
		panes = append(panes, box(m.sidebar(), sideW, ph, m.focus == paneFacets))
		rest -= sideW
	}
	listW := max(30, rest*2/5)
	panes = append(panes,
		box(m.list(vis, listW-4, ph-2), listW, ph, m.focus == paneList),
		box(m.detail(e, rest-listW-4, ph-2), rest-listW, ph, m.focus == paneDetail))

	return fit(m.header(), m.w) + "\n" + m.budget() + "\n" +
		lipgloss.JoinHorizontal(lipgloss.Top, panes...) + "\n" +
		fit(m.footer(), m.w)
}

func (m *Model) header() string {
	un := ""
	if n := m.s.Unsaved(); n > 0 {
		un = "  " + sWarn.Render(fmt.Sprintf("%d unsaved", n))
	}
	app := sAccent.Render(" equip ")
	if m.narrow() {
		app = " "
	}
	return app + sBold.Render(m.s.Project) +
		sDim.Render("  presets: ") + strings.Join(m.s.Presets, ", ") + " " + sKey.Render("p") + sDim.Render(" change") + un
}

var kindColor = [3]lipgloss.Style{
	lipgloss.NewStyle().Foreground(lipgloss.Color("#5fafff")),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#d787ff")),
	lipgloss.NewStyle().Foreground(lipgloss.Color("#ffaf5f")),
}

// budget is a full-width bar of the session total split by kind, and its legend.
func (m *Model) budget() string {
	var byKind [3]int
	for _, e := range m.s.Exts {
		byKind[e.Kind] += e.Cost()
	}
	total, bw := m.s.Total(), m.w-2
	var bar, legend strings.Builder
	used := 0
	for k, n := range byKind {
		seg := 0
		if total > 0 {
			seg = n * bw / total
		}
		if k == 2 && total > 0 {
			seg = bw - used // rounding leftovers go to the last segment
		}
		used += seg
		bar.WriteString(kindColor[k].Render(strings.Repeat("█", seg)))
		legend.WriteString(kindColor[k].Render("■ ") + [3]string{"skills", "plugins", "MCP servers"}[k] + " " + sDim.Render(data.Tokens(n)) + "   ")
	}
	bar.WriteString(sDim.Render(strings.Repeat("░", bw-used)))
	sum := "session " + sBold.Render(data.Tokens(total)) + " tokens"
	gap := max(1, m.w-2-lipgloss.Width(legend.String())-lipgloss.Width(sum))
	return fit(" "+bar.String(), m.w) + "\n" + fit(" "+legend.String()+strings.Repeat(" ", gap)+sum, m.w)
}

func (m *Model) footer() string {
	if m.quitting {
		return sWarn.Render(fmt.Sprintf(" %d unsaved changes. Quit without saving? y/n", m.s.Unsaved()))
	}
	if m.flash != "" {
		return " " + m.flash
	}
	if m.typing {
		return help("type", "search", "enter", "done", "esc", "clear")
	}
	switch m.focus {
	case paneFacets:
		return help("↑↓", "facet", "enter", "list", "tab", "pane", "/", "search", "s", "save", "q", "quit")
	case paneDetail:
		if e := m.selected(m.visible()); e != nil && len(e.Children) > 0 {
			return help("↑↓", "contents", "space", "cycle", "1-3", "set content state", "tab", "pane", "s", "save", "q", "quit")
		}
		return help("↑↓", "scroll", "space", "cycle", "1-3", "set state", "tab", "pane", "s", "save", "q", "quit")
	}
	return help("↑↓", "move", "space", "cycle", "1-3", "set state", "tab", "pane", "/", "search", "p", "presets", "s", "save", "q", "quit", "[ ]", "facet")
}

func help(kv ...string) string {
	var b strings.Builder
	for i := 0; i < len(kv); i += 2 {
		b.WriteString(" " + sKey.Render(kv[i]) + " " + sDim.Render(kv[i+1]) + " ")
	}
	return b.String()
}

func (m *Model) sidebar() []string {
	lines := []string{sBold.Render("Show"), ""}
	for i, f := range facets {
		if facetGap[i] {
			lines = append(lines, "")
		}
		lines = append(lines, row(i == m.facet, m.focus == paneFacets, f.name, fmt.Sprint(len(m.rows(f, ""))), 22))
	}
	return lines
}

// row renders a cursor mark, a label, and a right-aligned value in width w.
func row(cur, focused bool, label, val string, w int) string {
	mark, st := "  ", lipgloss.NewStyle()
	if cur {
		mark, st = sAccent.Render("▸ "), sBold
		if focused {
			st = sAccent
		}
	}
	label = trunc(label, w-2-len(val)-1)
	return mark + st.Render(label) + strings.Repeat(" ", w-2-len([]rune(label))-len(val)) + sDim.Render(val)
}

func (m *Model) list(vis []*data.Ext, w, h int) []string {
	title := sBold.Render(facets[m.facet].name) + sDim.Render(fmt.Sprintf("  %d", len(vis)))
	if m.narrow() {
		title += sDim.Render("  [ ] facet")
	}
	lines := []string{title}
	switch {
	case m.typing:
		lines = append(lines, sKey.Render("/")+m.query+sKey.Render("▏"))
	case m.query != "":
		lines = append(lines, sKey.Render("/")+m.query+sDim.Render("  esc clears"))
	default:
		lines = append(lines, "")
	}
	rows := h - len(lines)
	if m.cur < m.top {
		m.top = m.cur
	}
	if m.cur >= m.top+rows {
		m.top = m.cur - rows + 1
	}
	if len(vis) == 0 {
		lines = append(lines, sDim.Render("  nothing matches"))
	}
	for i := m.top; i < len(vis) && i < m.top+rows; i++ {
		e := vis[i]
		dirty := " "
		if anyOf(e, m.dirty) {
			dirty = sWarn.Render("*")
		}
		name := trunc(e.Name, w-12)
		in := ""
		if e.Parent != nil {
			in = trunc(" in "+e.Parent.Name, w-12-len([]rune(name)))
		}
		ns := lipgloss.NewStyle()
		mark := "  "
		if i == m.cur {
			mark, ns = sAccent.Render("▸ "), sBold
			if m.focus == paneList {
				ns = sAccent
			}
		}
		pad := strings.Repeat(" ", w-12-len([]rune(name+in)))
		lines = append(lines, mark+stateGlyph(e)+" "+ns.Render(name)+sDim.Render(in)+dirty+pad+cost(e))
	}
	return lines
}

// stateGlyph is e's state glyph, dimmed when e is plugin content of an off plugin.
func stateGlyph(e *data.Ext) string {
	if e.Parent != nil && e.Parent.State != data.On {
		return sDim.Render(glyph[e.State])
	}
	return sState[e.State].Render(glyph[e.State])
}

// cost is what e adds now, or dimmed what it would add when on.
func cost(e *data.Ext) string {
	if n := e.Cost(); n > 0 {
		return fmt.Sprintf("%6s", data.Tokens(n))
	}
	return sDim.Render(fmt.Sprintf("%6s", data.Tokens(e.Tokens)))
}

var kindName = map[data.Kind]string{data.Skill: "Skill", data.Plugin: "Plugin", data.MCP: "MCP server"}
var sourceLabel = map[data.Kind]string{data.Skill: "Installed in", data.Plugin: "Marketplace", data.MCP: "Configured in"}

func (m *Model) detail(e *data.Ext, w, h int) []string {
	if e == nil {
		return []string{sDim.Render("No extension selected")}
	}
	var l []string
	add := func(s ...string) { l = append(l, s...) }
	field := func(k, v string) { add(sDim.Render(fmt.Sprintf("%-13s", k)) + v) }

	title := sAccent.Render(e.Name) + sDim.Render("  "+kindName[e.Kind])
	h-- // the title stays put while the rest scrolls
	add("")
	if e.Desc != "" {
		add(wrap(e.Desc, w)...)
		add("")
	}
	if e.Parent != nil {
		field("Plugin", e.Source+sDim.Render(" ("+e.Parent.State.String()+")"))
	} else {
		field(sourceLabel[e.Kind], e.Source)
	}
	has := func(a data.Agent, name string) string {
		if e.Agents&a != 0 {
			return sOK.Render("✓ " + name)
		}
		return sDim.Render("✗ " + name)
	}
	field("Agents", has(data.Claude, "Claude Code")+"   "+has(data.Codex, "Codex"))
	in := m.s.PresetsOf(e)
	var active []string
	for _, p := range in {
		if slices.Contains(m.s.Presets, p) {
			active = append(active, p)
		}
	}
	switch {
	case e.Parent != nil:
		field("Presets", sDim.Render("follows its plugin"))
	case len(in) == 0:
		field("Presets", sDim.Render("in no preset"))
	default:
		var names []string
		for _, p := range in {
			if slices.Contains(active, p) {
				names = append(names, sAccent.Render(p)+sDim.Render(" (active)"))
			} else {
				names = append(names, p)
			}
		}
		field("Presets", strings.Join(names, ", "))
	}
	switch e.Origin {
	case "override":
		fallback := "off (default)"
		switch {
		case e.Parent != nil:
			fallback = "on (default)"
		case len(active) > 0:
			fallback = "on (preset " + strings.Join(active, ", ") + ")"
		}
		field("Origin", sWarn.Render("override")+", set by hand here")
		field("", sDim.Render("without it: "+fallback))
	case "default":
		if e.Parent != nil {
			field("Origin", "default, on while its plugin is on")
		} else {
			field("Origin", "default, no active preset has it")
		}
	default:
		field("Origin", e.Origin)
	}
	if m.base[e] != e.State {
		field("Unsaved", sWarn.Render("was "+m.base[e].String()+" at last save"))
	}

	add("", sBold.Render("State"))
	for i, st := range e.States() {
		radio := sDim.Render("( ) ")
		label := st.String()
		if st == e.State {
			radio = sState[st].Render("(" + glyph[st] + ") ")
			label = sState[st].Bold(true).Render(label)
		}
		add("  " + radio + sKey.Render(fmt.Sprint(i+1)) + " " + label)
	}

	add("", sBold.Render("Context cost"))
	if total := m.s.Total(); e.Cost() > 0 {
		add(fmt.Sprintf("  %s tokens, %.1f%% of session %s", data.Tokens(e.Cost()), 100*float64(e.Cost())/float64(total), data.Tokens(total)))
	} else {
		add(fmt.Sprintf("  0 now, %s tokens when on", data.Tokens(e.Tokens)))
	}

	sel, selEnd := -1, -1 // lines of the highlighted content row and its description
	if e.Kind == data.Plugin {
		head := fmt.Sprintf("Contents (%d)", len(e.Children))
		add("", sBold.Render(head)+strings.Repeat(" ", max(1, w-24-len(head)))+
			sDim.Render(fmt.Sprintf("%-10s%6s %6s", "state", "kind", "ctx")))
		if len(e.Children) == 0 {
			add(sDim.Render("  no skills or MCP servers"))
		}
		if e.State != data.On && len(e.Children) > 0 {
			add(sDim.Render("  plugin is off: none of these load"))
		}
		for i, c := range e.Children {
			kind := "skill"
			if c.Kind == data.MCP {
				kind = "MCP"
			}
			tag := "   "
			if c.Origin == "override" {
				tag = "ovr"
			}
			dirty := " "
			if m.dirty(c) {
				dirty = sWarn.Render("*")
			}
			name := trunc(c.Name, w-29)
			mark, ns := "  ", lipgloss.NewStyle()
			if e.State != data.On {
				ns = sDim
			}
			if m.focus == paneDetail && i == m.child {
				mark, ns, sel = sAccent.Render("▸ "), sAccent, len(l)
			}
			add(mark + stateGlyph(c) + " " + ns.Render(name) + dirty +
				strings.Repeat(" ", w-29-len([]rune(name))) +
				sState[c.State].Render(fmt.Sprintf("%-7s", map[data.State]string{data.On: "on", data.Manual: "manual", data.Off: "off"}[c.State])) +
				sWarn.Render(tag) + sDim.Render(fmt.Sprintf("%6s", kind)) + " " + cost(c))
			// at most 2 lines of description, cut with … when longer, then a gap
			desc := wrap(c.Desc, w-4)
			if len(desc) > 2 {
				desc = []string{desc[0], trunc(strings.Join(desc[1:], " "), w-4)}
			}
			for _, d := range desc {
				add("    " + sDim.Render(d))
			}
			if sel == len(l)-1-len(desc) {
				selEnd = len(l) - 1
			}
			if i < len(e.Children)-1 {
				add("")
			}
		}
	}

	// scroll to keep the highlighted content row and its description in view
	if sel >= 0 {
		m.dtop = min(m.dtop, sel)
		if selEnd >= m.dtop+h-1 {
			m.dtop = selEnd - h + 2
		}
	}
	m.dtop = max(0, min(m.dtop, len(l)-h))
	l = l[m.dtop:]
	if len(l) > h {
		l = append(l[:h-1], sDim.Render("  ↓ more below (tab here, ↑↓)"))
	}
	return append([]string{title}, l...)
}

// ---- layout helpers ----

// box draws lines in a rounded border of exactly w x h cells.
func box(lines []string, w, h int, focused bool) string {
	bc := cBorder
	if focused {
		bc = cAccent
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(bc).Padding(0, 1).
		Render(block(lines, w-4, h-2))
}

// block pads or cuts lines to exactly w x h cells.
func block(lines []string, w, h int) string {
	out := make([]string, h)
	for i := range out {
		if i < len(lines) {
			out[i] = fit(lines[i], w)
		} else {
			out[i] = strings.Repeat(" ", w)
		}
	}
	return strings.Join(out, "\n")
}

// fit truncates or pads one styled line to exactly w cells.
func fit(s string, w int) string {
	if lipgloss.Width(s) > w {
		s = lipgloss.NewStyle().MaxWidth(w).Render(s)
	}
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}

func trunc(s string, w int) string {
	r := []rune(s)
	if w <= 0 {
		return ""
	}
	if len(r) <= w {
		return s
	}
	return string(r[:w-1]) + "…"
}

func wrap(s string, w int) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(s) {
		if line != "" && len(line)+1+len(word) > w {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	return append(lines, line)
}
