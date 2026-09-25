// Package c is a PROTOTYPE variant, "State board". Throwaway.
//
// The state is the layout: three columns ON | MANUAL-ONLY | OFF, and changing
// an extension's state means moving it to another column.
package c

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/svyatov/equip/prototype/list-screen/data"
)

var colStates = [3]data.State{data.On, data.Manual, data.Off}

var (
	kindColor  = [3]lipgloss.Style{fg("#5fafff"), fg("#d787ff"), fg("#ffaf5f")}
	stateColor = map[data.State]lipgloss.Style{data.On: fg("#5fd75f"), data.Manual: fg("#ffd75f"), data.Off: fg("#8a8a8a")}
	dim        = fg("#6c6c6c")
	faint      = fg("#444444")
	bold       = lipgloss.NewStyle().Bold(true)
	ccStyle    = fg("#ff875f")
	cxStyle    = fg("#afafff")
	ovStyle    = fg("#ffaf00")
	curStyle   = lipgloss.NewStyle().Background(lipgloss.Color("#5f00af")).Foreground(lipgloss.Color("#ffffff")).Bold(true)
	curIdle    = lipgloss.NewStyle().Background(lipgloss.Color("#303030"))
	flashStyle = fg("#5fd75f").Bold(true)
	warnStyle  = fg("#ffaf00").Bold(true)
)

func fg(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }

type Model struct {
	s        *data.Store
	w, h     int
	col      int
	cur, off [3]int
	query    string
	typing   bool
	grouped  bool
	detail   bool
	dcur     int // cursor in the details overlay's plugin contents
	confirm  bool
	flash    string
}

func New(s *data.Store) *Model    { return &Model{s: s, w: 120, h: 38} }
func (m *Model) Name() string     { return "State board" }
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }
func (m *Model) narrow() bool     { return m.w < 90 }
func (m *Model) colWidth(c int) int {
	return [3]int{(m.w - 2) / 3, (m.w - 2) / 3, m.w - 2 - 2*((m.w-2)/3)}[c]
}

func (m *Model) matches(e *data.Ext) bool {
	if e.Match(m.query) {
		return true
	}
	return slices.ContainsFunc(e.Children, func(ch *data.Ext) bool { return ch.Match(m.query) })
}

// potential is what e would cost if it were on: a plugin counts its contents that are on.
func potential(e *data.Ext) int {
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

// items is column c's extensions, filtered and sorted by cost (and kind when grouped).
func (m *Model) items(c int) []*data.Ext {
	var out []*data.Ext
	for _, e := range m.s.Exts {
		if e.State == colStates[c] && m.matches(e) {
			out = append(out, e)
		}
	}
	slices.SortStableFunc(out, func(a, b *data.Ext) int {
		if m.grouped && a.Kind != b.Kind {
			return cmp.Compare(a.Kind, b.Kind)
		}
		return cmp.Or(cmp.Compare(potential(b), potential(a)), cmp.Compare(a.Name, b.Name))
	})
	return out
}

func (m *Model) clamp() {
	for c := range 3 {
		m.cur[c] = max(0, min(m.cur[c], len(m.items(c))-1))
	}
}

func (m *Model) sel() *data.Ext {
	m.clamp()
	if it := m.items(m.col); len(it) > 0 {
		return it[m.cur[m.col]]
	}
	return nil
}

// move sets the highlighted extension to the next state in direction d,
// skipping columns it can't be in, and follows it with the cursor.
func (m *Model) move(d int) {
	e := m.sel()
	if e == nil {
		return
	}
	for c := m.col + d; c >= 0 && c < 3; c += d {
		if slices.Contains(e.States(), colStates[c]) {
			m.set(e, colStates[c])
			return
		}
	}
}

// set changes a top-level extension's state and follows it with the cursor.
func (m *Model) set(e *data.Ext, s data.State) {
	e.Set(s)
	m.col = slices.Index(colStates[:], s)
	m.cur[m.col] = slices.Index(m.items(m.col), e)
}

// detailKey handles keys while the details overlay is open. In a plugin the
// cursor walks its contents; otherwise space and 1-3 act on the extension.
func (m *Model) detailKey(k string) {
	e := m.sel()
	if e == nil {
		m.detail = false
		return
	}
	target := e
	if len(e.Children) > 0 {
		m.dcur = max(0, min(m.dcur, len(e.Children)-1))
		target = e.Children[m.dcur]
	}
	setTarget := func(s data.State) {
		switch {
		case !slices.Contains(target.States(), s):
		case target == e:
			m.set(e, s)
		default:
			target.Set(s)
		}
	}
	switch k {
	case "esc", "enter", "q":
		m.detail = false
	case "up", "k":
		m.dcur = max(0, m.dcur-1)
	case "down", "j":
		m.dcur = min(len(e.Children)-1, m.dcur+1)
	case "space":
		ss := target.States()
		setTarget(ss[(slices.Index(ss, target.State)+1)%len(ss)])
	case "1":
		setTarget(data.On)
	case "2":
		setTarget(data.Manual)
	case "3":
		setTarget(data.Off)
	case "shift+left", "<":
		m.move(-1)
	case "shift+right", ">":
		m.move(1)
	}
}

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	k := key.String()
	m.flash = ""
	if m.confirm {
		switch k {
		case "y":
			return tea.Quit
		case "s":
			m.s.Save()
			return tea.Quit
		case "n", "esc":
			m.confirm = false
		}
		return nil
	}
	if m.detail {
		m.detailKey(k)
		return nil
	}
	switch k {
	case "left":
		m.col = max(0, m.col-1)
	case "right":
		m.col = min(2, m.col+1)
	case "tab":
		m.col = (m.col + 1) % 3
	case "up":
		m.cur[m.col]--
	case "down":
		m.cur[m.col]++
	case "pgup":
		m.cur[m.col] -= 10
	case "pgdown":
		m.cur[m.col] += 10
	case "home":
		m.cur[m.col] = 0
	case "end":
		m.cur[m.col] = 1 << 30
	case "shift+left", "<":
		m.move(-1)
	case "shift+right", ">":
		m.move(1)
	case "esc":
		m.query, m.typing = "", false
	case "enter":
		if m.typing {
			m.typing = false
		} else {
			m.detail, m.dcur = m.sel() != nil, 0
		}
	case "backspace":
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
		}
		m.typing = m.query != ""
	default:
		if !m.typing {
			switch k {
			case "h":
				m.col = max(0, m.col-1)
				return nil
			case "l":
				m.col = min(2, m.col+1)
				return nil
			case "k":
				m.cur[m.col]--
				return nil
			case "j":
				m.cur[m.col]++
				return nil
			case "g":
				m.grouped = !m.grouped
				return nil
			case "s":
				m.flash = fmt.Sprintf("saved %d changes (prototype: nothing written)", m.s.Save())
				return nil
			case "q":
				if m.s.Unsaved() == 0 {
					return tea.Quit
				}
				m.confirm = true
				return nil
			case "/":
				m.typing = true
				return nil
			}
		}
		if t := key.Text; t != "" && (m.typing || t != " ") {
			m.typing = true
			m.query += strings.ToLower(t)
		}
	}
	m.clamp()
	return nil
}

// fit truncates or pads plain s to exactly n cells.
func fit(s string, n int) string {
	r := []rune(s)
	switch {
	case n <= 0:
		return ""
	case len(r) > n:
		return string(r[:n-1]) + "…"
	}
	return s + strings.Repeat(" ", n-len(r))
}

// spread puts left and right on one line of width w, left wins on overlap.
func spread(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) View() string {
	if m.w < 30 || m.h < 8 {
		return "terminal too small"
	}
	m.clamp()
	lines := m.top()
	lines = append(lines, m.board(m.h-len(lines)-1)...)
	lines = append(lines, m.help())
	clip := lipgloss.NewStyle().MaxWidth(m.w)
	for i := range lines {
		lines[i] = clip.Render(lines[i])
	}
	base := strings.Join(lines, "\n")
	if !m.detail {
		return base
	}
	pop := m.popup()
	x := max(0, (m.w-lipgloss.Width(pop))/2)
	y := max(0, (m.h-1-lipgloss.Height(pop))/2)
	return lipgloss.NewCanvas(m.w, m.h).
		Compose(lipgloss.NewCompositor(lipgloss.NewLayer(base), lipgloss.NewLayer(pop).X(x).Y(y).Z(1))).
		Render()
}

// top is the title line and the context budget strip.
func (m *Model) top() []string {
	var byKind [3]int
	for _, e := range m.s.Exts {
		byKind[e.Kind] += e.Cost()
	}
	total := m.s.Total()

	left := bold.Render(" equip ") + dim.Render(m.s.Project) + "  " + dim.Render("presets: ") + strings.Join(m.s.Presets, ", ")
	right := ""
	switch {
	case m.confirm:
		right = warnStyle.Render(fmt.Sprintf("quit with %d unsaved changes? y quit · s save and quit · n cancel ", m.s.Unsaved()))
	case m.flash != "":
		right = flashStyle.Render(m.flash + " ")
	case m.typing:
		right = "filter: " + bold.Render(m.query) + "▏ "
	case m.query != "":
		right = "filter: " + bold.Render(m.query) + dim.Render(" (esc clears) ")
	}
	if right != "" && lipgloss.Width(left)+lipgloss.Width(right)+1 > m.w {
		left = bold.Render(" equip ")
	}
	title := spread(left, right, m.w)

	// Budget bar: the session total split by kind, full width.
	bw := m.w - 2
	var bar strings.Builder
	used, last := 0, -1
	var seg [3]int
	for k := range 3 {
		if total > 0 {
			seg[k] = byKind[k] * bw / total
			used += seg[k]
		}
		if byKind[k] > 0 {
			last = k
		}
	}
	if last >= 0 {
		seg[last] += bw - used
		used = bw
	}
	for k := range 3 {
		bar.WriteString(kindColor[k].Render(strings.Repeat("█", seg[k])))
	}
	bar.WriteString(faint.Render(strings.Repeat("░", bw-used)))

	names := [3]string{"skills", "plugins", "MCP servers"}
	var legend []string
	for k := range 3 {
		legend = append(legend, kindColor[k].Render("■ ")+names[k]+" "+dim.Render(data.Tokens(byKind[k])))
	}
	sum := "session " + bold.Render(data.Tokens(total)) + " tokens "
	return []string{title, " " + bar.String(), spread(" "+strings.Join(legend, "   "), sum, m.w)}
}

// board renders the three columns into h lines.
func (m *Model) board(h int) []string {
	cols := make([][]string, 3)
	for c := range 3 {
		cols[c] = m.column(c, h)
	}
	sep := faint.Render("│")
	out := make([]string, h)
	for i := range h {
		out[i] = cols[0][i] + sep + cols[1][i] + sep + cols[2][i]
	}
	return out
}

type line struct {
	e    *data.Ext
	head string
}

func (m *Model) column(c, h int) []string {
	w := m.colWidth(c)
	items := m.items(c)
	st := colStates[c]

	// Header: state, count, context cost.
	label := strings.ToUpper(st.String())
	if m.narrow() && st == data.Manual {
		label = "MANUAL"
	}
	cost := 0
	for _, e := range items {
		cost += e.Cost()
	}
	costText := data.Tokens(cost) + " tokens"
	if st != data.On {
		costText = "0 in context"
		if m.narrow() {
			costText = "0 ctx"
		}
	}
	hs := stateColor[st].Bold(true)
	rule := stateColor[st]
	if c == m.col {
		hs = hs.Underline(true)
	} else {
		rule = faint
	}
	head := spread(" "+hs.Render(label)+" "+dim.Render(fmt.Sprint(len(items))), costText+" ", w)

	// Body lines, with kind subheaders when grouped.
	var ls []line
	curLine := 0
	names := [3]string{"skills", "plugins", "MCP servers"}
	for i, e := range items {
		if m.grouped && (i == 0 || items[i-1].Kind != e.Kind) {
			n, t := 0, 0
			for _, x := range items {
				if x.Kind == e.Kind {
					n++
					t += potential(x)
				}
			}
			ls = append(ls, line{head: fmt.Sprintf("%s · %d · %s", names[e.Kind], n, data.Tokens(t))})
		}
		if i == m.cur[c] {
			curLine = len(ls)
		}
		ls = append(ls, line{e: e})
	}

	body := h - 2
	off := &m.off[c]
	if curLine < *off {
		*off = curLine
	}
	if curLine >= *off+body {
		*off = curLine - body + 1
	}
	*off = max(0, min(*off, len(ls)-body))

	out := []string{head, rule.Render(strings.Repeat("─", w))}
	for i := *off; i < *off+body; i++ {
		switch {
		case i < len(ls) && ls[i].e == nil:
			out = append(out, dim.Render(fit("  "+ls[i].head, w)))
		case i < len(ls):
			out = append(out, m.row(ls[i].e, w, c, i == curLine))
		case len(items) == 0 && i == 0:
			msg := "  empty"
			if m.query != "" {
				msg = "  no match"
			}
			out = append(out, faint.Render(fit(msg, w)))
		default:
			out = append(out, strings.Repeat(" ", w))
		}
	}
	// Scroll hints on the rule line.
	if *off > 0 || *off+body < len(ls) {
		hint := fmt.Sprintf(" %d-%d/%d ", *off+1, min(*off+body, len(ls)), len(ls))
		if lipgloss.Width(hint) < w-2 {
			out[1] = rule.Render(strings.Repeat("─", w-len(hint)-1)) + dim.Render(hint) + rule.Render("─")
		}
	}
	return out
}

func (m *Model) row(e *data.Ext, w, c int, cursor bool) string {
	glyph := [3]string{"S", "P", "M"}[e.Kind]
	aw, sw := 5, 0
	if m.narrow() {
		aw = 2
	}
	if w >= 50 {
		sw = 16
	}
	nw := w - 13 - aw - sw
	if sw > 0 {
		nw--
	}

	var cc, cx string
	if m.narrow() {
		cc, cx = "·", "·"
		if e.Agents&data.Claude != 0 {
			cc = "C"
		}
		if e.Agents&data.Codex != 0 {
			cx = "X"
		}
	} else {
		cc, cx = "··", "··"
		if e.Agents&data.Claude != 0 {
			cc = "CC"
		}
		if e.Agents&data.Codex != 0 {
			cx = "CX"
		}
	}
	sepA := " "
	if m.narrow() {
		sepA = ""
	}
	mark := " "
	if e.Origin == "override" {
		mark = "✎"
	}
	cost := fmt.Sprintf("%5s", data.Tokens(potential(e)))
	hint := ""
	if n, on := len(e.Children), onCount(e); on < n {
		hint = fmt.Sprintf(" %d/%d", on, n)
		if !m.narrow() {
			hint += " on"
		}
	}
	name := fit(e.Name, nw-len(hint))
	src := ""
	if sw > 0 {
		src = fit(e.Source, sw) + " "
	}

	if cursor {
		s := curStyle
		if c != m.col {
			s = curIdle
		}
		return s.Render(" " + glyph + " " + name + hint + " " + src + cc + sepA + cx + " " + mark + " " + cost + " ")
	}
	agent := func(s string, st lipgloss.Style) string {
		if strings.Trim(s, "·") == "" {
			return faint.Render(s)
		}
		return st.Render(s)
	}
	costStyle := lipgloss.NewStyle()
	if e.State != data.On {
		costStyle = dim
	}
	return " " + kindColor[e.Kind].Render(glyph) + " " + name + ovStyle.Render(hint) + " " + dim.Render(src) +
		agent(cc, ccStyle) + sepA + agent(cx, cxStyle) + " " + ovStyle.Render(mark) + " " + costStyle.Render(cost) + " "
}

func onCount(e *data.Ext) (n int) {
	for _, c := range e.Children {
		if c.State == data.On {
			n++
		}
	}
	return n
}

func (m *Model) help() string {
	type kv struct{ k, v string }
	keys := []kv{{"←→", "column"}, {"↑↓", "move"}, {"< >", "set state"}, {"enter", "details"}, {"g", "group by kind"}, {"s", "save"}, {"q", "quit"}, {"type", "filter"}, {"✎", "override"}}
	if m.narrow() {
		keys = []kv{{"←→↑↓", ""}, {"<>", "state"}, {"enter", "info"}, {"g", "group"}, {"s", "save"}, {"q", "quit"}, {"type", "filter"}, {"✎", "override"}}
	}
	if m.detail {
		keys = []kv{{"esc", "close"}, {"space", "cycle"}, {"1 2 3", "on/manual-only/off"}, {"< >", "move on board"}}
		if m.narrow() {
			keys = []kv{{"esc", "close"}, {"space", "cycle"}, {"1-3", "on/manual/off"}, {"<>", "board"}}
		}
		if e := m.sel(); e != nil && len(e.Children) > 0 {
			keys = append([]kv{{"↑↓", "contents"}}, keys...)
		}
	}
	var parts []string
	for _, p := range keys {
		part := bold.Render(p.k)
		if p.v != "" {
			part += " " + dim.Render(p.v)
		}
		parts = append(parts, part)
	}
	return " " + strings.Join(parts, dim.Render("  "))
}

func (m *Model) popup() string {
	e := m.sel()
	pw := min(m.w-4, 70)
	iw := pw - 4
	agents := map[data.Agent]string{data.Claude: "Claude Code", data.Codex: "Codex", data.Both: "Claude Code, Codex"}[e.Agents]
	origin := map[string]string{
		"default":   "default: not in any preset",
		"override":  "override: set by hand for this project",
		"preset go": "from preset go",
	}[e.Origin]
	states := make([]string, 0, 3)
	for _, s := range e.States() {
		t := s.String()
		if s == e.State {
			t = stateColor[s].Bold(true).Render("[" + t + "]")
		} else {
			t = dim.Render(t)
		}
		states = append(states, t)
	}
	cost := data.Tokens(e.Cost()) + " tokens in every session"
	if e.State != data.On {
		cost = "0 now, " + data.Tokens(potential(e)) + " tokens when on"
	}
	field := func(k, v string) string { return dim.Render(fmt.Sprintf("%-8s", k)) + v }
	ls := []string{
		kindColor[e.Kind].Bold(true).Render(e.Kind.String()) + "  " + bold.Render(e.Name),
		lipgloss.NewStyle().Width(iw).Render(e.Desc),
		"",
		field("source", e.Source),
		field("agents", agents),
		field("state", strings.Join(states, " ")),
		field("origin", origin),
		field("cost", cost),
	}
	if n := len(e.Children); n > 0 {
		room := max(3, min(16, m.h-3-lipgloss.Height(strings.Join(ls, "\n"))-2))
		off := max(0, min(m.dcur-room/2, n-room))
		title := fmt.Sprintf("contents · %d/%d on", onCount(e), n)
		if n > room {
			title += fmt.Sprintf(" · %d-%d of %d", off+1, min(n, off+room), n)
		}
		if e.State != data.On {
			title += " · plugin is off, nothing loads"
		}
		ls = append(ls, "", dim.Render(title))
		for i := off; i < min(n, off+room); i++ {
			ch := e.Children[i]
			mark := " "
			if ch.Origin == "override" {
				mark = "✎"
			}
			glyph := map[data.State]string{data.On: "●", data.Manual: "◐", data.Off: "○"}[ch.State]
			kind := [3]string{"S", "P", "M"}[ch.Kind]
			name := fit(ch.Name, iw-14)
			tk := fmt.Sprintf("%5s", data.Tokens(ch.Tokens))
			switch {
			case i == m.dcur:
				ls = append(ls, curStyle.Render(fit("▸ "+glyph+" "+kind+" "+name+" "+mark+" "+tk, iw)))
			case e.State != data.On:
				ls = append(ls, faint.Render("  "+glyph+" "+kind+" "+name+" "+mark+" "+tk))
			default:
				tkStyle := lipgloss.NewStyle()
				if ch.State != data.On {
					tkStyle = dim
				}
				ls = append(ls, "  "+stateColor[ch.State].Render(glyph)+" "+kindColor[ch.Kind].Render(kind)+" "+name+" "+ovStyle.Render(mark)+" "+tkStyle.Render(tk))
			}
		}
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#5f00af")).
		Padding(0, 1).Width(pw).Render(strings.Join(ls, "\n"))
}
