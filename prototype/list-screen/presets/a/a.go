// Package a is a PROTOTYPE, throwaway: preset screens variant A.
// A compact picker modal over the dimmed main screen; e/n push a full-screen
// preset editor page; saving or deleting pops a propagation confirm modal.
package a

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/svyatov/equip/prototype/list-screen/data"
	"github.com/svyatov/equip/prototype/list-screen/mainscreen"
)

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
	sFaint  = lipgloss.NewStyle().Foreground(lipgloss.Color("#3a3a3a"))
	sKey    = lipgloss.NewStyle().Foreground(cAccent)
	sWarn   = lipgloss.NewStyle().Foreground(cManual).Bold(true)
	sState  = map[data.State]lipgloss.Style{
		data.On:     lipgloss.NewStyle().Foreground(cOn),
		data.Manual: lipgloss.NewStyle().Foreground(cManual),
		data.Off:    lipgloss.NewStyle().Foreground(cOff),
	}
	glyph    = map[data.State]string{data.On: "●", data.Manual: "◐", data.Off: "○"}
	kindHead = map[data.Kind]string{data.Skill: "Skills", data.Plugin: "Plugins", data.MCP: "MCP servers"}
)

// modal floats over the current page (the picker, or the editor on top of it).
const (
	modalNone = iota
	modalConfirm
	modalDelete
	modalDiscard
)

type Model struct {
	s     *data.Store
	main  *mainscreen.Model
	w, h  int
	modal int

	// picker
	pcur int
	sel  map[string]bool

	// editor page, open while ed != nil
	orig             string       // preset being edited, "" for a new one
	ed               *data.Preset // working copy
	ecur, etop       int
	query            string
	typing, renaming bool
	nameBuf, flash   string
}

func New(s *data.Store) *Model { return &Model{s: s, main: mainscreen.New(s)} }

func (m *Model) Name() string { return "overlay picker + editor page" }

func (m *Model) SetSize(w, h int) { m.w, m.h = w, h; m.main.SetSize(w, h) }

func (m *Model) Open() {
	m.ed, m.modal, m.pcur = nil, modalNone, 0
	m.sel = map[string]bool{}
	for _, n := range m.s.Presets {
		m.sel[n] = true
	}
}

func closeCmd() tea.Msg { return data.ClosePresets{} }

// ---- update ----

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	key := k.String()
	m.flash = ""
	if m.modal != modalNone {
		m.updateModal(key)
		return nil
	}
	if m.ed != nil {
		m.updateEditor(k, key)
		return nil
	}
	lib := m.s.Library
	switch key {
	case "up", "k":
		m.pcur = max(0, m.pcur-1)
	case "down", "j":
		m.pcur = min(len(lib)-1, m.pcur+1)
	case "space":
		n := lib[m.pcur].Name
		m.sel[n] = !m.sel[n]
	case "enter":
		m.s.SetActive(m.chosen())
		return closeCmd
	case "esc", "q":
		return closeCmd
	case "e":
		m.openEditor(lib[m.pcur].Name, lib[m.pcur].Clone())
	case "n":
		m.openEditor("", &data.Preset{Members: map[string]bool{}})
		m.renaming, m.nameBuf = true, ""
	}
	return nil
}

// chosen lists the checked presets in library order.
func (m *Model) chosen() []string {
	var out []string
	for _, p := range m.s.Library {
		if m.sel[p.Name] {
			out = append(out, p.Name)
		}
	}
	return out
}

func (m *Model) openEditor(orig string, p *data.Preset) {
	m.orig, m.ed = orig, p
	m.ecur, m.etop, m.query, m.typing, m.renaming = 0, 0, "", false, false
}

func (m *Model) dirty() bool {
	if m.orig == "" {
		return m.ed.Name != "" || len(m.ed.Members) > 0
	}
	o := m.s.Preset(m.orig)
	return m.ed.Name != o.Name || !maps.Equal(m.ed.Members, o.Members)
}

func (m *Model) updateEditor(k tea.KeyPressMsg, key string) {
	if m.renaming {
		switch key {
		case "esc":
			m.renaming = false
		case "enter":
			n := strings.TrimSpace(m.nameBuf)
			switch {
			case n == "":
				m.flash = "a preset needs a name"
			case n != m.orig && m.s.Preset(n) != nil:
				m.flash = "a preset named " + n + " already exists"
			default:
				m.ed.Name, m.renaming = n, false
			}
		case "backspace":
			m.nameBuf = dropLast(m.nameBuf)
		default:
			m.nameBuf += k.Text
		}
		return
	}
	if m.typing {
		switch key {
		case "esc":
			m.query, m.typing = "", false
		case "enter":
			m.typing = false
		case "backspace":
			m.query, m.ecur = dropLast(m.query), 0
		case "up", "down":
			m.move(key)
		default:
			if k.Text != "" {
				m.query, m.ecur = m.query+k.Text, 0
			}
		}
		return
	}
	switch key {
	case "esc":
		switch {
		case m.query != "":
			m.query = ""
		case m.dirty():
			m.modal = modalDiscard
		default:
			m.ed = nil
		}
	case "/":
		m.typing = true
	case "r":
		m.renaming, m.nameBuf = true, m.ed.Name
	case "space":
		if vis := m.visible(); len(vis) > 0 {
			n := vis[m.ecur].Name
			if m.ed.Members[n] {
				delete(m.ed.Members, n)
			} else {
				m.ed.Members[n] = true
			}
		}
	case "d":
		if m.orig == "" {
			m.ed = nil
		} else {
			m.modal = modalDelete
		}
	case "enter":
		switch {
		case m.ed.Name == "":
			m.flash = "name the preset first: r"
		case !m.dirty():
			m.flash = "nothing changed"
		default:
			m.modal = modalConfirm
		}
	default:
		m.move(key)
	}
}

func (m *Model) move(key string) {
	d := map[string]int{"up": -1, "k": -1, "down": 1, "j": 1, "pgup": -10, "pgdown": 10, "home": -1 << 20, "end": 1 << 20}[key]
	m.ecur = max(0, min(len(m.visible())-1, m.ecur+d))
}

func (m *Model) updateModal(key string) {
	switch {
	case key == "y" && m.modal == modalDiscard:
		m.ed = nil
	case key == "y":
		var p *data.Preset
		if m.modal == modalConfirm {
			p = m.ed
		}
		m.s.SavePreset(m.orig, p)
		if m.sel[m.orig] {
			delete(m.sel, m.orig)
			if p != nil {
				m.sel[p.Name] = true
			}
		}
		m.pcur = min(m.pcur, len(m.s.Library)-1)
		if m.orig == "" {
			m.pcur = len(m.s.Library) - 1
		}
		m.ed = nil
	case key != "n" && key != "esc":
		return
	}
	m.modal = modalNone
}

func dropLast(s string) string {
	if r := []rune(s); len(r) > 0 {
		return string(r[:len(r)-1])
	}
	return s
}

// visible is every top-level extension matching the search, grouped by kind.
func (m *Model) visible() []*data.Ext {
	q := strings.ToLower(m.query)
	var out []*data.Ext
	for _, e := range m.s.Exts {
		if e.Match(q) {
			out = append(out, e)
		}
	}
	slices.SortStableFunc(out, func(a, b *data.Ext) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), strings.Compare(a.Name, b.Name))
	})
	return out
}

// preview is what this project gets if the preset named old takes p's
// members (nil p: deleted, so it leaves the active list). The copy keeps the
// old name so the active list still finds it; a rename alone changes nothing.
func (m *Model) preview(old string, p *data.Preset) ([]*data.Ext, int) {
	return m.s.Try(func() {
		if p == nil {
			m.s.Presets = slices.DeleteFunc(m.s.Presets, func(n string) bool { return n == old })
		} else if q := m.s.Preset(old); q != nil {
			q.Members = p.Members
		}
	})
}

// flipped is the state a preset change moves e to: overrides never move, so
// it is always on to off or back.
func flipped(e *data.Ext) data.State {
	if e.State == data.Off {
		return data.On
	}
	return data.Off
}

// ---- view ----

func (m *Model) View() string {
	if m.ed == nil {
		return overlay(m.main.View(), m.picker(), m.w, m.h)
	}
	page := m.editor()
	switch m.modal {
	case modalConfirm, modalDelete:
		return overlay(page, m.confirm(), m.w, m.h)
	case modalDiscard:
		return overlay(page, box([]string{
			sWarn.Render("Discard your changes to " + cmp.Or(m.orig, m.ed.Name, "the new preset") + "?"),
			sDim.Render("Nothing was saved; no project changes."), "",
			help("y", "discard", "n", "keep editing"),
		}, 50, 6, true), m.w, m.h)
	}
	return page
}

func (m *Model) picker() string {
	const w = 84
	iw := w - 4
	l := []string{sAccent.Render("Presets") + sDim.Render("  for "+m.s.Project),
		sDim.Render("Check any number. The change stays unsaved until s on the main screen."), ""}
	for i, p := range m.s.Library {
		cb := sDim.Render("[ ]")
		if m.sel[p.Name] {
			cb = sAccent.Render("[x]")
		}
		mark, ns := "  ", lipgloss.NewStyle()
		if i == m.pcur {
			mark, ns = sAccent.Render("▸ "), sAccent
		}
		now := ""
		if slices.Contains(m.s.Presets, p.Name) {
			now = sDim.Render("active now")
		}
		l = append(l, mark+cb+" "+fit(ns.Render(p.Name), 20)+
			sDim.Render(fmt.Sprintf("%3d extensions   %2d projects   ", len(p.Members), len(m.s.UsersOf(p.Name))))+now)
	}
	l = append(l, "", sDim.Render(strings.Repeat("─", iw)))

	names := m.chosen()
	after := sAccent.Render(strings.Join(names, " + "))
	if len(names) == 0 {
		after = sDim.Render("none (agent defaults: everything on)")
	}
	l = append(l, sBold.Render("Preview ")+after)
	changed, total := m.s.Try(func() { m.s.Presets = names })
	l = append(l, changes(changed, iw)...)
	l = append(l, sessionLine(m.s.Total(), total))
	var ovr []string
	for _, e := range m.s.All() {
		if e.Origin == "override" {
			ovr = append(ovr, sState[e.State].Render(glyph[e.State])+" "+e.Name)
		}
	}
	l = append(l, "", sDim.Render(fmt.Sprintf("Overrides kept (%d), they win over any preset:", len(ovr))))
	l = append(l, wrapStyled(ovr, iw)...)
	l = append(l, "", help("↑↓", "move", "space", "check", "enter", "apply", "e", "edit preset", "n", "new preset", "esc", "cancel"))
	return box(l, w, len(l)+2, true)
}

// changes lists what turns on and off, at most two lines each.
func changes(changed []*data.Ext, w int) []string {
	var on, off []string
	for _, e := range changed {
		if e.Parent != nil {
			continue // plugin contents follow their plugin
		}
		to := flipped(e)
		n := sState[to].Render(glyph[to]) + " " + e.Name
		if to == data.Off {
			off = append(off, n)
		} else {
			on = append(on, n)
		}
	}
	if len(on)+len(off) == 0 {
		return []string{sDim.Render("  no extension changes state here")}
	}
	var l []string
	for _, g := range []struct {
		label string
		es    []string
	}{{"turns on", on}, {"turns off", off}} {
		if len(g.es) == 0 {
			continue
		}
		lines := wrapStyled(g.es, w-18)
		if len(lines) > 2 {
			lines = []string{lines[0], lines[1] + sDim.Render(" …")}
		}
		for i, s := range lines {
			lbl := ""
			if i == 0 {
				lbl = fmt.Sprintf("%s (%d)", g.label, len(g.es))
			}
			l = append(l, "  "+sDim.Render(fmt.Sprintf("%-15s", lbl))+s)
		}
	}
	return l
}

func sessionLine(before, after int) string {
	d, sign := after-before, "+"
	if d < 0 {
		sign, d = "-", -d
	}
	return "  " + sDim.Render(fmt.Sprintf("%-15s", "session")) + data.Tokens(before) + " → " + sBold.Render(data.Tokens(after)) +
		sDim.Render(fmt.Sprintf(" tokens (%s%s)", sign, data.Tokens(d)))
}

// wrapStyled joins styled items with ", " into lines of at most w cells.
func wrapStyled(items []string, w int) []string {
	var lines []string
	line := ""
	for _, it := range items {
		if line != "" && lipgloss.Width(line)+2+lipgloss.Width(it) > w {
			lines = append(lines, line+",")
			line = ""
		}
		if line != "" {
			line += ", "
		}
		line += it
	}
	return append(lines, line)
}

func (m *Model) editor() string {
	vis := m.visible()
	m.ecur = max(0, min(m.ecur, len(vis)-1))
	changed, _ := m.preview(m.orig, m.ed)

	title := "editing " + m.orig
	if m.orig == "" {
		title = "new preset"
	}
	head := sAccent.Render(" equip ") + sBold.Render(m.s.Project) + sDim.Render("  › presets › ") + sAccent.Render(title)
	if m.dirty() {
		head += "  " + sWarn.Render("edited, not saved")
	}
	name := sBold.Render(m.ed.Name)
	if m.ed.Name == "" {
		name = sDim.Render("(unnamed)")
	}
	if m.renaming {
		name = sKey.Render("▏") + m.nameBuf + sKey.Render("▏") + sDim.Render("  enter keeps, esc cancels")
	}
	users := 0
	if m.orig != "" {
		users = len(m.s.UsersOf(m.orig))
	}
	nameLine := sDim.Render(" Name  ") + name + "   " + sDim.Render(fmt.Sprintf("%d members, used by %d projects", len(m.ed.Members), users))
	if !m.renaming {
		nameLine += "  " + sKey.Render("r") + sDim.Render(" rename")
	}

	var top string
	switch {
	case m.typing:
		top = sKey.Render("/") + m.query + sKey.Render("▏")
	case m.query != "":
		top = sKey.Render("/") + m.query + sDim.Render("  esc clears")
	default:
		top = sDim.Render(fmt.Sprintf("    %-28s%-24s%-4s%6s  %s", "extension", "state here", "", "cost", "also in (")) +
			sAccent.Render("active here") + sDim.Render(")")
	}

	var lines []string
	curLine := 0
	kind := data.Kind(-1)
	for i, e := range vis {
		if e.Kind != kind {
			kind = e.Kind
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			n, in := 0, 0
			for _, x := range m.s.Exts {
				if x.Kind == kind {
					n++
					if m.ed.Members[x.Name] {
						in++
					}
				}
			}
			lines = append(lines, sBold.Render(kindHead[kind])+sDim.Render(fmt.Sprintf("  %d of %d in this preset", in, n)))
		}
		mark, ns := "  ", lipgloss.NewStyle()
		if i == m.ecur {
			mark, ns, curLine = sAccent.Render("▸ "), sAccent, len(lines)
		}
		cb := sDim.Render("[ ] ")
		if m.ed.Members[e.Name] {
			cb = sAccent.Render("[x] ")
		}
		st := sState[e.State].Render(glyph[e.State] + " " + e.State.String())
		if slices.Contains(changed, e) {
			to := flipped(e)
			st += sWarn.Render(" → ") + sState[to].Render(glyph[to]+" "+to.String())
		}
		ovr := ""
		if e.Origin == "override" {
			ovr = sWarn.Render("ovr")
		}
		var also []string
		for _, p := range m.s.PresetsOf(e) {
			switch {
			case p == m.orig:
			case slices.Contains(m.s.Presets, p):
				also = append(also, sAccent.Render(p))
			default:
				also = append(also, sDim.Render(p))
			}
		}
		lines = append(lines, mark+cb+fit(ns.Render(trunc(e.Name, 27)), 28)+fit(st, 24)+fit(ovr, 4)+
			sDim.Render(fmt.Sprintf("%6s  ", data.Tokens(e.Tokens)))+strings.Join(also, sDim.Render(", ")))
	}
	if len(vis) == 0 {
		lines = append(lines, sDim.Render("  nothing matches"))
	}
	rows := m.h - 6 // header, name line, borders, column line, footer
	m.etop = min(m.etop, max(0, curLine-1))
	if curLine >= m.etop+rows {
		m.etop = curLine - rows + 1
	}
	m.etop = max(0, min(m.etop, len(lines)-rows))
	lines = append([]string{top}, lines[m.etop:]...)

	foot := help("↑↓", "move", "space", "member", "/", "search", "r", "rename", "d", "delete", "enter", "save…", "esc", "back")
	if m.flash != "" {
		foot = " " + sWarn.Render(m.flash)
	}
	return fit(head, m.w) + "\n" + fit(nameLine, m.w) + "\n" + box(lines, m.w, m.h-3, true) + "\n" + fit(foot, m.w)
}

func (m *Model) confirm() string {
	const w = 88
	iw := w - 4
	p := m.ed
	verb := "Save preset " + m.ed.Name
	switch {
	case m.modal == modalDelete:
		verb, p = "Delete preset "+m.orig, nil
	case m.orig == "":
		verb = "Create preset " + m.ed.Name
	case m.ed.Name != m.orig:
		verb += sDim.Render(", renamed from ") + m.orig
	}
	l := []string{sAccent.Render(verb), ""}

	if p != nil && m.orig != "" {
		var diff []string
		o := m.s.Preset(m.orig)
		for _, n := range slices.Sorted(maps.Keys(p.Members)) {
			if !o.Members[n] {
				diff = append(diff, sState[data.On].Render("+ ")+n)
			}
		}
		added := len(diff)
		for _, n := range slices.Sorted(maps.Keys(o.Members)) {
			if !p.Members[n] {
				diff = append(diff, sState[data.Off].Render("- ")+n)
			}
		}
		l = append(l, sBold.Render("Members")+sDim.Render(fmt.Sprintf("  %d added, %d removed", added, len(diff)-added)))
		if len(diff) > 0 {
			l = append(l, wrapStyled(diff, iw)...)
		}
		l = append(l, "")
	}

	users := m.s.UsersOf(m.orig)
	switch {
	case m.orig == "":
		l = append(l, sDim.Render("No project uses it yet. Check it in the picker to use it here."))
	case len(users) == 0:
		l = append(l, sDim.Render("No project uses it."))
	default:
		l = append(l, sBold.Render(fmt.Sprintf("Rewrites the %d projects that use it", len(users))))
		for _, u := range users {
			before := m.presetsOf(u)
			var after []string
			for _, n := range before {
				switch {
				case n != m.orig:
					after = append(after, n)
				case p != nil:
					after = append(after, p.Name)
				}
			}
			tag := sDim.Render(strings.Join(before, " + "))
			if !slices.Equal(before, after) {
				to := strings.Join(after, " + ")
				if len(after) == 0 {
					to = sWarn.Render("none (agent defaults)")
				}
				tag += sDim.Render(" → ") + to
			}
			if u == m.s.Project {
				u += sAccent.Render(" (this)")
			}
			l = append(l, "  "+fit(u, 44)+tag)
		}
	}

	changed, total := m.preview(m.orig, p)
	l = append(l, "", sBold.Render("This project"))
	l = append(l, changes(changed, iw)...)
	l = append(l, sessionLine(m.s.Total(), total), sDim.Render("  overrides and pending toggles here stay as they are"),
		"", help("y", "confirm", "n", "back to the editor"))
	return box(l, w, len(l)+2, true)
}

func (m *Model) presetsOf(path string) []string {
	if path == m.s.Project {
		return m.s.Presets
	}
	for _, p := range m.s.Projects {
		if p.Path == path {
			return p.Presets
		}
	}
	return nil
}

// overlay centers fg over a dimmed, colorless bg.
func overlay(bg, fg string, w, h int) string {
	lines := strings.Split(ansi.Strip(bg), "\n")
	for i, l := range lines {
		lines[i] = sFaint.Render(l)
	}
	x, y := max(0, (w-lipgloss.Width(fg))/2), max(0, (h-lipgloss.Height(fg))/2)
	return lipgloss.NewCompositor(lipgloss.NewLayer(block(lines, w, h)), lipgloss.NewLayer(fg).X(x).Y(y).Z(1)).Render()
}

// ---- layout helpers, copied from mainscreen ----

func help(kv ...string) string {
	var b strings.Builder
	for i := 0; i < len(kv); i += 2 {
		b.WriteString(" " + sKey.Render(kv[i]) + " " + sDim.Render(kv[i+1]) + " ")
	}
	return b.String()
}

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
