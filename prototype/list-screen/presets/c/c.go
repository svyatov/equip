// Package c is a PROTOTYPE, throwaway: variant C "Matrix" of the preset screens.
// One grid replaces the screen: rows are extensions, columns are presets plus
// this project's effective state, and every preset task happens on its cells.
package c

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/svyatov/equip/prototype/list-screen/data"
)

var (
	cAccent = lipgloss.Color("#af87ff")
	cOn     = lipgloss.Color("#5fd75f")
	cManual = lipgloss.Color("#d7af5f")
	cOff    = lipgloss.Color("#6c6c6c")
	cDim    = lipgloss.Color("#8a8a8a")

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

const (
	nameW  = 26
	labelW = 2 + nameW + 1 + 6 + 1 + 6 + 1 // mark, name, kind, cost, gap
	colW   = 13
	hereW  = 9
)

// col is one preset column: a working copy of a library preset, or a new one.
type col struct {
	orig    string // name in the library, "" when new
	p       *data.Preset
	deleted bool
}

type Model struct {
	s              *data.Store
	w, h           int
	cols           []*col
	row, col       int
	top, left      int
	query          string
	input, buf     string // input: "/" search, "n" new, "r" rename
	confirm, leave bool
	flash          string
	base           map[*data.Ext]data.State // states when the matrix opened
	baseTotal      int
}

func New(s *data.Store) *Model { return &Model{s: s} }

func (m *Model) Name() string { return "matrix" }

func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

func (m *Model) Open() {
	m.resetCols()
	m.row, m.col, m.top, m.left = 0, 0, 0, 0
	m.query, m.input, m.confirm, m.leave, m.flash = "", "", false, false, ""
	m.base = map[*data.Ext]data.State{}
	for _, e := range m.s.Exts {
		m.base[e] = e.State
	}
	m.baseTotal = m.s.Total()
}

func (m *Model) resetCols() {
	m.cols = nil
	for _, p := range m.s.Library {
		m.cols = append(m.cols, &col{orig: p.Name, p: p.Clone()})
	}
}

func (m *Model) dirty(c *col) bool {
	if c.orig == "" || c.deleted {
		return true
	}
	o := m.s.Preset(c.orig)
	return o.Name != c.p.Name || !maps.Equal(o.Members, c.p.Members)
}

func (m *Model) pending() (out []*col) {
	for _, c := range m.cols {
		if m.dirty(c) {
			out = append(out, c)
		}
	}
	return out
}

func (m *Model) active(c *col) bool { return c.orig != "" && slices.Contains(m.s.Presets, c.orig) }

func (m *Model) rows() []*data.Ext {
	var out []*data.Ext
	q := strings.ToLower(m.query)
	for _, e := range m.s.Exts {
		if e.Match(q) {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b *data.Ext) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// stage applies c to the store's library and active list. Only call it inside Try.
func (m *Model) stage(c *col) {
	s := m.s
	i := slices.IndexFunc(s.Library, func(p *data.Preset) bool { return p.Name == c.orig })
	switch {
	case c.deleted:
		s.Library = slices.Delete(s.Library, i, i+1)
		s.Presets = slices.DeleteFunc(s.Presets, func(n string) bool { return n == c.orig })
	case i < 0:
		s.Library = append(s.Library, c.p.Clone())
	default:
		s.Library[i] = c.p.Clone()
		for j, n := range s.Presets {
			if n == c.orig {
				s.Presets[j] = c.p.Name
			}
		}
	}
}

// preview is each extension's state here as if the pending columns (or only
// one of them) were written, the extensions that would change, and the total.
func (m *Model) preview(only *col) (st map[*data.Ext]data.State, changed []*data.Ext, total int) {
	st = map[*data.Ext]data.State{}
	changed, total = m.s.Try(func() {
		for _, c := range m.pending() {
			if only == nil || only == c {
				m.stage(c)
			}
		}
		m.s.Recompute()
		for _, e := range m.s.Exts {
			st[e] = e.State
		}
	})
	return st, changed, total
}

func closeCmd() tea.Msg { return data.ClosePresets{} }

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	m.flash = ""
	key := k.String()
	switch {
	case m.leave:
		m.leave = false
		if key == "y" {
			return closeCmd
		}
		return nil
	case m.confirm:
		switch key {
		case "y":
			m.write()
			m.confirm = false
		case "n", "esc":
			m.confirm = false
		}
		return nil
	case m.input != "":
		m.typing(k)
		return nil
	}

	rows := m.rows()
	m.row = max(0, min(m.row, len(rows)-1))
	m.col = max(0, min(m.col, len(m.cols)-1))
	var c *col
	if len(m.cols) > 0 {
		c = m.cols[m.col]
	}
	switch key {
	case "esc":
		switch {
		case m.query != "":
			m.query = ""
		case len(m.pending()) > 0:
			m.leave = true
		default:
			return closeCmd
		}
	case "up", "k", "down", "j", "pgup", "pgdown", "home", "end":
		d := map[string]int{"up": -1, "k": -1, "down": 1, "j": 1, "pgup": -10, "pgdown": 10, "home": -1 << 20, "end": 1 << 20}[key]
		m.row = max(0, min(len(rows)-1, m.row+d))
	case "left", "h":
		m.col = max(0, m.col-1)
	case "right", "l":
		m.col = min(len(m.cols)-1, m.col+1)
	case "space":
		if c == nil || len(rows) == 0 || c.deleted {
			break
		}
		if n := rows[m.row].Name; c.p.Members[n] {
			delete(c.p.Members, n)
		} else {
			c.p.Members[n] = true
		}
	case "enter", "a":
		m.toggleActive(c)
	case "n":
		m.input, m.buf = "n", ""
	case "r":
		if c != nil && !c.deleted {
			m.input, m.buf = "r", c.p.Name
		}
	case "d":
		switch {
		case c == nil:
		case c.orig == "":
			m.cols = slices.Delete(m.cols, m.col, m.col+1)
		default:
			c.deleted = !c.deleted
		}
	case "/":
		m.input, m.buf = "/", m.query
	case "w":
		if len(m.pending()) == 0 {
			m.flash = sDim.Render("no preset edits to write")
		} else {
			m.confirm = true
		}
	}
	return nil
}

func (m *Model) toggleActive(c *col) {
	switch {
	case c == nil:
		return
	case c.orig == "":
		m.flash = sWarn.Render("new preset: write it (w) before using it here")
		return
	case c.deleted:
		m.flash = sWarn.Render("pending delete: d again to keep it")
		return
	}
	var names []string
	for _, p := range m.s.Library {
		if slices.Contains(m.s.Presets, p.Name) != (p.Name == c.orig) {
			names = append(names, p.Name)
		}
	}
	m.s.SetActive(names)
}

func (m *Model) typing(k tea.KeyPressMsg) {
	switch k.String() {
	case "esc":
		if m.input == "/" {
			m.query = ""
		}
		m.input = ""
		return
	case "enter":
		var self *col
		if m.input == "r" {
			self = m.cols[m.col]
		}
		name := strings.TrimSpace(m.buf)
		switch {
		case m.input == "/":
		case name == "":
			m.flash = sWarn.Render("a preset needs a name")
		case slices.ContainsFunc(m.cols, func(c *col) bool { return c != self && (c.p.Name == name || c.orig == name) }):
			m.flash = sWarn.Render("a preset named " + name + " already exists")
			return
		case self == nil:
			m.cols = append(m.cols, &col{p: &data.Preset{Name: name, Members: map[string]bool{}}})
			m.col = len(m.cols) - 1
		default:
			self.p.Name = name
		}
		m.input = ""
		return
	case "backspace":
		if r := []rune(m.buf); len(r) > 0 {
			m.buf = string(r[:len(r)-1])
		}
	default:
		m.buf += k.Text
	}
	if m.input == "/" {
		m.query, m.row, m.top = m.buf, 0, 0
	}
}

func (m *Model) write() {
	pend := m.pending()
	projects := map[string]bool{}
	for _, c := range pend {
		for _, u := range m.s.UsersOf(c.orig) {
			projects[u] = true
		}
		if c.deleted {
			m.s.SavePreset(c.orig, nil)
		} else {
			m.s.SavePreset(c.orig, c.p.Clone())
		}
	}
	m.resetCols()
	m.flash = sOK.Render(fmt.Sprintf("wrote %d presets, rewrote %d projects (prototype: nothing written)", len(pend), len(projects)))
}

// ---- view ----

func (m *Model) View() string {
	if m.confirm {
		return m.confirmView()
	}
	rows := m.rows()
	m.row = max(0, min(m.row, len(rows)-1))
	m.col = max(0, min(m.col, len(m.cols)-1))
	prev, _, total := m.preview(nil)

	// column window
	ncol := max(1, (m.w-labelW-1-hereW)/colW)
	m.left = min(m.left, m.col)
	if m.col >= m.left+ncol {
		m.left = m.col - ncol + 1
	}
	m.left = max(0, min(m.left, len(m.cols)-ncol))
	vis := m.cols[m.left:min(len(m.cols), m.left+ncol)]
	gridW := labelW + 1 + len(vis)*colW

	var l []string
	add := func(s string) { l = append(l, s) }

	presets := sAccent.Render(strings.Join(m.s.Presets, " + "))
	if len(m.s.Presets) == 0 {
		presets = sDim.Render("none (agent defaults)")
	}
	add(sAccent.Render(" equip ") + sBold.Render(m.s.Project) + sDim.Render("  preset matrix   active here: ") + presets)
	add("")

	// header: 4 lines
	more := ""
	if len(vis) < len(m.cols) {
		more = fmt.Sprintf(" columns %d-%d of %d", m.left+1, m.left+len(vis), len(m.cols))
	}
	label := []string{
		sBold.Render(" Extensions") + sDim.Render(fmt.Sprintf("  %d shown", len(rows))),
		sDim.Render(more),
		"",
		sDim.Render(fmt.Sprintf("  %-*s %-6s %6s", nameW, "name", "kind", "ctx")),
	}
	for i := range 4 {
		line := fit(label[i], labelW) + sDim.Render("│")
		for j, c := range vis {
			line += fit(m.headCell(c, i, m.left+j == m.col), colW)
		}
		line = fit(line, gridW) + sDim.Render("│")
		line += []string{sBold.Render(" here"), sDim.Render(" * changed"), sDim.Render(" ovr:"), sDim.Render(" override")}[i]
		add(line)
	}
	add(sDim.Render(strings.Repeat("─", labelW) + "┼" + strings.Repeat("─", len(vis)*colW) + "┼" + strings.Repeat("─", hereW)))

	// body
	body := m.h - len(l) - 3
	m.top = min(m.top, m.row)
	if m.row >= m.top+body {
		m.top = m.row - body + 1
	}
	if len(rows) == 0 {
		add(sDim.Render("  nothing matches"))
	}
	for i := m.top; i < len(rows) && i < m.top+body; i++ {
		e := rows[i]
		mark, ns := "  ", lipgloss.NewStyle()
		if i == m.row {
			mark, ns = sAccent.Render("▸ "), sAccent
		}
		ctx := fmt.Sprintf("%6s", data.Tokens(onCost(e)))
		if prev[e] != data.On {
			ctx = sDim.Render(ctx)
		}
		line := mark + ns.Render(fmt.Sprintf("%-*s", nameW, trunc(e.Name, nameW))) + " " +
			sDim.Render(fmt.Sprintf("%-6s", e.Kind)) + " " + ctx + " " + sDim.Render("│")
		for j, c := range vis {
			line += m.cell(c, e, i == m.row && m.left+j == m.col)
		}
		line = fit(line, gridW) + sDim.Render("│")
		st := prev[e]
		here := " " + sState[st].Render(glyph[st])
		if st != m.base[e] {
			here += sWarn.Render("*")
		} else {
			here += " "
		}
		if e.Origin == "override" {
			here += sWarn.Render("ovr")
		}
		add(line + here)
	}
	for len(l) < m.h-3 {
		add("")
	}

	// summary, status, help
	var on, off, ovr int
	for _, e := range m.s.Exts {
		if prev[e] != m.base[e] {
			if prev[e] == data.On {
				on++
			} else {
				off++
			}
		}
		if e.Origin == "override" {
			ovr++
		}
	}
	sum := sDim.Render(" session ") + sBold.Render(data.Tokens(m.baseTotal)) + sDim.Render(" → ") + sBold.Render(data.Tokens(total)) +
		sDim.Render(" tokens since opening · ") + sOK.Render(fmt.Sprintf("%d turned on", on)) + sDim.Render(" · ") +
		fmt.Sprintf("%d turned off", off) + sDim.Render(fmt.Sprintf(" · %d overrides kept", ovr))
	if n := len(m.pending()); n > 0 {
		sum += sDim.Render(" · ") + sWarn.Render(fmt.Sprintf("%d unwritten presets", n)) + sDim.Render(" (w)")
	}
	add(sum)
	add(m.status())
	add(help("←→↑↓", "move", "space", "member", "enter/a", "active here", "n", "new", "r", "rename", "d", "delete", "/", "search", "w", "write", "esc", "back"))
	return block(l, m.w, m.h)
}

func (m *Model) headCell(c *col, line int, cur bool) string {
	switch line {
	case 0:
		ns := sBold
		if cur {
			ns = sAccent.Reverse(true)
		}
		s := " " + ns.Render(trunc(c.p.Name, colW-3))
		if m.dirty(c) {
			s += sWarn.Render("*")
		}
		return s
	case 1:
		switch {
		case c.deleted:
			return sWarn.Render(" deleted")
		case c.orig == "":
			return sWarn.Render(" new")
		case m.active(c):
			return sOK.Render(" ● active")
		}
		return sDim.Render(" · inactive")
	case 2:
		return sDim.Render(fmt.Sprintf(" %d ext", len(c.p.Members)))
	}
	return sDim.Render(fmt.Sprintf(" %d proj", len(m.s.UsersOf(c.orig))))
}

func (m *Model) cell(c *col, e *data.Ext, cur bool) string {
	member := c.p.Members[e.Name]
	was := false
	if o := m.s.Preset(c.orig); o != nil {
		was = o.Members[e.Name]
	}
	g, st := "·", sDim
	if member {
		g, st = "●", lipgloss.NewStyle()
	}
	switch {
	case member != was:
		st = sWarn
	case c.deleted:
		st = sDim
	case member && m.active(c):
		st = sOK
	}
	if cur {
		st = st.Reverse(true)
	}
	return st.Render(" "+g+" ") + strings.Repeat(" ", colW-3)
}

func (m *Model) status() string {
	switch {
	case m.leave:
		return sWarn.Render(fmt.Sprintf(" %d preset edits are not written. Leave and drop them? y/n", len(m.pending())))
	case m.input != "":
		p := map[string]string{"/": " /", "n": " new preset name: ", "r": " rename to: "}[m.input]
		return sKey.Render(p) + m.buf + sKey.Render("▏") + sDim.Render("  enter ok, esc cancel")
	case m.flash != "":
		return " " + m.flash
	case m.query != "":
		return sKey.Render(" /") + m.query + sDim.Render("  esc clears")
	}
	return ""
}

func (m *Model) confirmView() string {
	pend := m.pending()
	var l []string
	add := func(s ...string) { l = append(l, s...) }
	add(sAccent.Render(fmt.Sprintf(" Write %d preset edits", len(pend)))+
		sDim.Render("  presets are shared: every project that uses one is rewritten"), "")
	for _, c := range pend {
		old := m.s.Preset(c.orig)
		switch {
		case c.orig == "":
			add(" " + sBold.Render(c.p.Name) + sWarn.Render("  new preset"))
		case c.deleted:
			add(" " + sBold.Render(c.orig) + sWarn.Render("  delete"))
		case c.orig != c.p.Name:
			add(" " + sBold.Render(c.orig) + sWarn.Render("  renamed to ") + sBold.Render(c.p.Name))
		default:
			add(" " + sBold.Render(c.p.Name) + sWarn.Render("  members edited"))
		}
		if !c.deleted {
			var adds, drops []string
			for _, n := range slices.Sorted(maps.Keys(c.p.Members)) {
				if old == nil || !old.Members[n] {
					adds = append(adds, n)
				}
			}
			if old != nil {
				for _, n := range slices.Sorted(maps.Keys(old.Members)) {
					if !c.p.Members[n] {
						drops = append(drops, n)
					}
				}
			}
			if len(adds) > 0 {
				add(wrapped("   adds     ", sOK, adds, m.w)...)
			}
			if len(drops) > 0 {
				add(wrapped("   drops    ", sState[data.Off], drops, m.w)...)
			}
		}
		users := m.s.UsersOf(c.orig)
		if len(users) == 0 {
			add(sDim.Render("   no project uses it: only the library changes"))
		} else {
			add(sDim.Render(fmt.Sprintf("   rewrites %d projects:", len(users))))
			for _, u := range users {
				if u == m.s.Project {
					u += sAccent.Render("  (this project)")
				}
				add("     " + u)
			}
		}
		st, changed, _ := m.preview(c)
		var ons, offs []string
		for _, e := range changed {
			if st[e] == data.On {
				ons = append(ons, e.Name)
			} else {
				offs = append(offs, e.Name)
			}
		}
		if len(changed) == 0 {
			add(sDim.Render("   here: no change"))
		}
		if len(ons) > 0 {
			add(wrapped("   here on  ", sOK, ons, m.w)...)
		}
		if len(offs) > 0 {
			add(wrapped("   here off ", sState[data.Off], offs, m.w)...)
		}
		add("")
	}
	_, _, after := m.preview(nil)
	add(" Session total here: "+sBold.Render(data.Tokens(m.s.Total()))+sDim.Render(" → ")+sBold.Render(data.Tokens(after))+" tokens",
		sDim.Render(" Overrides here stay as they are."))
	if len(l) > m.h-2 {
		l = append(l[:m.h-3], sDim.Render(" … more not shown"))
	}
	for len(l) < m.h-1 {
		add("")
	}
	add(help("y", "write all", "n", "back to matrix"))
	return block(l, m.w, m.h)
}

// wrapped renders a labelled, comma-separated list, continuing under the label.
func wrapped(label string, st lipgloss.Style, names []string, w int) []string {
	lines := wrap(strings.Join(names, ", "), w-len(label)-1)
	for i := range lines {
		lines[i] = st.Render(lines[i])
		if i == 0 {
			lines[i] = sDim.Render(label) + lines[i]
		} else {
			lines[i] = strings.Repeat(" ", len(label)) + lines[i]
		}
	}
	return lines
}

// onCost is what e adds to a session when on.
func onCost(e *data.Ext) int {
	if e.Children == nil {
		return e.Tokens
	}
	n := 0
	for _, c := range e.Children {
		if c.State == data.On {
			n += c.Tokens
		}
	}
	return n
}

// ---- layout helpers, copied from mainscreen ----

func help(kv ...string) string {
	var b strings.Builder
	for i := 0; i < len(kv); i += 2 {
		b.WriteString(" " + sKey.Render(kv[i]) + " " + sDim.Render(kv[i+1]) + " ")
	}
	return b.String()
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
	for word := range strings.FieldsSeq(s) {
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
