// Package b is a PROTOTYPE, throwaway: preset screens variant B.
// Presets workspace: one persistent three-pane screen (library, members,
// impact) where picking, editing, and confirming a preset happen side by side.
package b

import (
	"cmp"
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

const (
	paneLib = iota
	paneMembers
)

// item is one library row: the preset as written, and its working copy.
type item struct {
	orig string       // name in the library, "" until first written
	p    *data.Preset // working copy, edited in place until w
}

type Model struct {
	s              *data.Store
	w, h           int
	items          []*item
	cur, mcur      int
	mtop           int
	focus          int
	before         map[*data.Ext]data.State // states at opening
	total0         int
	naming         bool // inline name edit on the highlighted row
	buf            string
	adding         bool // add-members list over the middle pane
	searching      bool // typing a search in the add list
	query          string
	acur, atop     int
	confirm        string // "", "write", "delete", "activate"
	leaving, flash string
}

func New(s *data.Store) *Model { return &Model{s: s} }

func (m *Model) Name() string { return "workspace" }

func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

func (m *Model) Open() {
	m.items = nil
	for _, p := range m.s.Library {
		m.items = append(m.items, &item{p.Name, p.Clone()})
	}
	m.before = map[*data.Ext]data.State{}
	for _, e := range m.s.All() {
		m.before[e] = e.State
	}
	m.total0 = m.s.Total()
	m.cur, m.mcur, m.mtop, m.focus = 0, 0, 0, paneLib
	m.naming, m.adding, m.confirm, m.leaving, m.flash = false, false, "", "", ""
}

func (m *Model) item() *item {
	m.cur = max(0, min(m.cur, len(m.items)-1))
	if len(m.items) == 0 {
		return nil
	}
	return m.items[m.cur]
}

func (m *Model) dirty(it *item) bool {
	lib := m.s.Preset(it.orig)
	return lib == nil || lib.Name != it.p.Name || !maps.Equal(lib.Members, it.p.Members)
}

// rows is what the middle pane lists: the working copy's members plus the
// written members it dropped, so a pending removal stays visible.
func (m *Model) rows(it *item) []*data.Ext {
	lib := m.s.Preset(it.orig)
	var out []*data.Ext
	for _, e := range m.s.Exts {
		if it.p.Members[e.Name] || (lib != nil && lib.Members[e.Name]) {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, byKind)
	return out
}

func (m *Model) candidates(it *item) []*data.Ext {
	var out []*data.Ext
	for _, e := range m.s.Exts {
		if !it.p.Members[e.Name] && e.Match(strings.ToLower(m.query)) {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, byKind)
	return out
}

func byKind(a, b *data.Ext) int {
	return cmp.Or(cmp.Compare(a.Kind, b.Kind), strings.Compare(a.Name, b.Name))
}

func closeCmd() tea.Msg { return data.ClosePresets{} }

func (m *Model) Update(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}
	m.flash = ""
	key := k.String()
	it := m.item()
	switch {
	case m.leaving != "":
		m.leaving = ""
		if key == "y" {
			return closeCmd
		}
		return nil
	case m.confirm != "":
		switch {
		case key == "y" && m.confirm == "activate":
			m.s.SetActive(toggled(m.s.Presets, it.orig))
			m.confirm = ""
		case key == "y":
			created := it.orig == "" && m.confirm == "write"
			m.write(it)
			m.confirm = map[bool]string{true: "activate"}[created]
		case key == "n" || key == "esc":
			m.confirm = ""
		}
		return nil
	case m.naming:
		m.name(it, key, k.Text)
		return nil
	case m.adding:
		m.add(it, key, k.Text)
		return nil
	}

	switch key {
	case "esc":
		if n := len(slices.DeleteFunc(slices.Clone(m.items), func(it *item) bool { return !m.dirty(it) })); n > 0 {
			m.leaving = fmt.Sprintf(" %d presets have unwritten edits. Leave and drop them? y/n", n)
			return nil
		}
		return closeCmd
	case "tab", "shift+tab":
		m.focus = 1 - m.focus
	case "left", "h":
		m.focus = paneLib
	case "right", "l":
		m.focus = paneMembers
	case "up", "k", "down", "j", "pgup", "pgdown", "home", "end":
		d := map[string]int{"up": -1, "k": -1, "down": 1, "j": 1, "pgup": -10, "pgdown": 10, "home": -1 << 20, "end": 1 << 20}[key]
		if m.focus == paneLib {
			m.cur += d
			m.mcur, m.mtop = 0, 0
		} else {
			m.mcur += d
		}
	case "n":
		m.items = append(m.items, &item{p: &data.Preset{Members: map[string]bool{}}})
		m.cur, m.focus, m.naming, m.buf = len(m.items)-1, paneLib, true, ""
	}
	if it == nil {
		return nil
	}
	switch key {
	case "r":
		m.naming, m.buf, m.focus = true, it.p.Name, paneLib
	case "d":
		if it.orig == "" {
			m.items = slices.Delete(m.items, m.cur, m.cur+1)
		} else {
			m.confirm = "delete"
		}
	case "w":
		if m.dirty(it) {
			m.confirm = "write"
		} else {
			m.flash = sDim.Render("no unwritten edits in " + it.p.Name)
		}
	case "a":
		m.adding, m.searching, m.query, m.acur, m.atop, m.focus = true, false, "", 0, 0, paneMembers
	case "space", "x":
		if m.focus == paneMembers {
			if rows := m.rows(it); len(rows) > 0 {
				e := rows[max(0, min(m.mcur, len(rows)-1))]
				if it.p.Members[e.Name] {
					delete(it.p.Members, e.Name)
				} else if key == "space" {
					it.p.Members[e.Name] = true
				}
			}
		} else if key == "space" {
			m.toggleActive(it)
		}
	}
	return nil
}

func (m *Model) toggleActive(it *item) {
	if it.orig == "" {
		m.flash = sWarn.Render("write the new preset first (w), then make it active here")
		return
	}
	m.s.SetActive(toggled(m.s.Presets, it.orig))
}

func toggled(names []string, n string) []string {
	if slices.Contains(names, n) {
		return slices.DeleteFunc(slices.Clone(names), func(x string) bool { return x == n })
	}
	return append(slices.Clone(names), n)
}

func (m *Model) name(it *item, key, text string) {
	switch key {
	case "esc":
		m.naming = false
		if it.p.Name == "" {
			m.items = slices.Delete(m.items, m.cur, m.cur+1)
		}
	case "enter":
		n := strings.TrimSpace(m.buf)
		taken := slices.ContainsFunc(m.items, func(x *item) bool { return x != it && (x.p.Name == n || x.orig == n) })
		if n == "" || taken {
			m.flash = sWarn.Render("a preset needs a unique name")
			return
		}
		it.p.Name, m.naming = n, false
	case "backspace":
		if r := []rune(m.buf); len(r) > 0 {
			m.buf = string(r[:len(r)-1])
		}
	default:
		if text != "" && text != " " {
			m.buf += text
		}
	}
}

func (m *Model) add(it *item, key, text string) {
	c := m.candidates(it)
	move := map[string]int{"up": -1, "down": 1, "pgup": -10, "pgdown": 10}
	if !m.searching {
		move["k"], move["j"] = -1, 1
	}
	switch {
	case move[key] != 0:
		m.acur += move[key]
	case m.searching:
		switch key {
		case "esc":
			m.searching, m.query, m.acur = false, "", 0
		case "enter":
			m.searching = false
		case "backspace":
			if r := []rune(m.query); len(r) > 0 {
				m.query, m.acur = string(r[:len(r)-1]), 0
			}
		default:
			if text != "" {
				m.query, m.acur = m.query+text, 0
			}
		}
	case key == "esc":
		m.adding = false
	case key == "/":
		m.searching = true
	case key == "space" && len(c) > 0:
		it.p.Members[c[m.acur].Name] = true
	}
	m.acur = max(0, min(m.acur, len(m.candidates(it))-1))
}

func (m *Model) write(it *item) {
	users := len(m.s.UsersOf(it.orig))
	if m.confirm == "delete" {
		m.s.SavePreset(it.orig, nil)
		m.items = slices.Delete(m.items, m.cur, m.cur+1)
		m.flash = sOK.Render(fmt.Sprintf("deleted %s from %d projects (prototype: nothing written)", it.orig, users))
		return
	}
	m.s.SavePreset(it.orig, it.p.Clone())
	it.orig = it.p.Name
	m.flash = sOK.Render(fmt.Sprintf("wrote %s to %d projects (prototype: nothing written)", it.p.Name, users))
}

// tryWrite previews writing (or deleting) it: what changes here and the total after.
func (m *Model) tryWrite(it *item, del bool) ([]*data.Ext, int) {
	return m.s.Try(func() {
		s := m.s
		i := slices.IndexFunc(s.Library, func(p *data.Preset) bool { return p.Name == it.orig })
		if i < 0 {
			return
		}
		if del {
			s.Library = slices.Delete(s.Library, i, i+1)
			s.Presets = slices.DeleteFunc(s.Presets, func(n string) bool { return n == it.orig })
			return
		}
		s.Library[i] = it.p.Clone()
		for j, n := range s.Presets {
			if n == it.orig {
				s.Presets[j] = it.p.Name
			}
		}
	})
}

// delta sums up a Try result: how many turn on and off, and the session total.
func (m *Model) delta(changed []*data.Ext, total int) string {
	on := 0
	for _, e := range changed {
		if e.State == data.Off {
			on++
		}
	}
	return fmt.Sprintf("%s, %d off, session %s → %s",
		sOK.Render(fmt.Sprintf("%d on", on)), len(changed)-on,
		data.Tokens(m.s.Total()), sBold.Render(data.Tokens(total)))
}

// disagrees reports whether e has an override that differs from what p says.
func disagrees(e *data.Ext, p *data.Preset) bool {
	says := data.Off
	if p.Members[e.Name] {
		says = data.On
	}
	return e.Origin == "override" && e.State != says
}

// ---- view ----

func (m *Model) View() string {
	ph := m.h - 2
	lw, mw := 32, 46
	rw := m.w - lw - mw
	it := m.item()
	mid := m.middle(it, mw-4, ph-2) // clamps the cursors the right pane reads
	var right []string
	switch {
	case it == nil || m.confirm != "":
		right = m.impact(it, rw-4)
	case m.adding:
		right = m.detail(at(m.candidates(it), m.acur), rw-4, ph-2)
	case m.focus == paneMembers:
		right = m.detail(at(m.rows(it), m.mcur), rw-4, ph-2)
	default:
		right = m.impact(it, rw-4)
	}
	return fit(m.header(), m.w) + "\n" + lipgloss.JoinHorizontal(lipgloss.Top,
		box(m.library(lw-4), lw, ph, m.focus == paneLib && m.confirm == ""),
		box(mid, mw, ph, m.focus == paneMembers && m.confirm == ""),
		box(right, rw, ph, m.confirm != "")) + "\n" + fit(m.footer(), m.w)
}

func at(exts []*data.Ext, i int) *data.Ext {
	if i < 0 || i >= len(exts) {
		return nil
	}
	return exts[i]
}

func (m *Model) header() string {
	active := sAccent.Render(strings.Join(m.s.Presets, " + "))
	if len(m.s.Presets) == 0 {
		active = sDim.Render("none (agent defaults)")
	}
	on, off := 0, 0
	for e, st := range m.before {
		switch {
		case st != data.On && e.State == data.On:
			on++
		case st != data.Off && e.State == data.Off:
			off++
		}
	}
	h := sAccent.Render(" equip presets ") + sDim.Render(" active here: ") + active
	if on+off == 0 {
		return h + sDim.Render("  session ") + sBold.Render(data.Tokens(m.s.Total()))
	}
	return h + sDim.Render("  since opening: session "+data.Tokens(m.total0)+" → ") + sBold.Render(data.Tokens(m.s.Total())) +
		sDim.Render(", ") + sOK.Render(fmt.Sprintf("%d on", on)) + sDim.Render(", ") + fmt.Sprintf("%d off", off) +
		sWarn.Render("  unsaved, s on main")
}

func (m *Model) footer() string {
	switch {
	case m.leaving != "":
		return sWarn.Render(m.leaving)
	case m.confirm == "activate":
		return " " + m.flash + help("y", "make it active here", "n", "not now")
	case m.confirm != "":
		return help("y", m.confirm, "n", "cancel")
	case m.flash != "":
		return " " + m.flash
	case m.naming:
		return help("type", "name", "enter", "done", "esc", "cancel")
	case m.searching:
		return help("type", "search", "↑↓", "move", "enter", "done", "esc", "clear")
	case m.adding:
		return help("j/k", "move", "space", "add", "/", "search", "esc", "close")
	case m.focus == paneMembers:
		return help("↑↓", "member", "x/space", "remove (space re-adds)", "a", "add", "w", "write preset", "tab", "library", "esc", "back")
	}
	return help("↑↓", "preset", "space", "active here", "n", "new", "r", "rename", "d", "delete", "a", "add members", "w", "write preset", "tab", "members", "esc", "back")
}

func help(kv ...string) string {
	var b strings.Builder
	for i := 0; i < len(kv); i += 2 {
		b.WriteString(" " + sKey.Render(kv[i]) + " " + sDim.Render(kv[i+1]) + " ")
	}
	return b.String()
}

func mark(cur, focused bool) (string, lipgloss.Style) {
	switch {
	case cur && focused:
		return sAccent.Render("▸ "), sAccent
	case cur:
		return sAccent.Render("▸ "), sBold
	}
	return "  ", lipgloss.NewStyle()
}

func (m *Model) library(w int) []string {
	l := []string{sBold.Render("Library") + sDim.Render(fmt.Sprintf("  %d presets", len(m.items))), "",
		sDim.Render(fmt.Sprintf("  here %-*s  ext proj", w-17, "name"))}
	if len(m.items) == 0 {
		l = append(l, sDim.Render("  no presets, n creates one"))
	}
	for i, it := range m.items {
		mk, ns := mark(i == m.cur, m.focus == paneLib)
		chk := sDim.Render("[ ]")
		if slices.Contains(m.s.Presets, it.orig) && it.orig != "" {
			chk = "[" + sOK.Render("x") + "]"
		}
		name := trunc(it.p.Name, w-15)
		if m.naming && i == m.cur {
			name = trunc(m.buf, w-16) + sKey.Render("▏")
		}
		dirty := " "
		if m.dirty(it) {
			dirty = sWarn.Render("*")
		}
		users := len(m.s.UsersOf(it.orig))
		if it.orig == "" {
			users = 0
		}
		l = append(l, fit(mk+chk+" "+ns.Render(name)+dirty, w-8)+sDim.Render(fmt.Sprintf("%4d%4d", len(it.p.Members), users)))
	}
	return append(l, "", sDim.Render("[x] active here (space)"), sDim.Render(" *  unwritten edits (w)"))
}

func (m *Model) middle(it *item, w, h int) []string {
	if it == nil {
		return []string{sDim.Render("No preset selected")}
	}
	if m.adding {
		return m.popup(it, w, h)
	}
	rows := m.rows(it)
	lib := m.s.Preset(it.orig)
	cost := 0
	for _, e := range rows {
		if it.p.Members[e.Name] {
			cost += e.Tokens
		}
	}
	l := []string{sBold.Render("Members of "+it.p.Name) + sDim.Render(fmt.Sprintf("  %d, %s tokens when on", len(it.p.Members), data.Tokens(cost)))}
	if len(rows) == 0 {
		l = append(l, "", sDim.Render("  no members yet, a adds some"))
	}
	m.mcur = max(0, min(m.mcur, len(rows)-1))
	return append(l, grouped(rows, m.mcur, &m.mtop, h-1, func(i int, e *data.Ext) string {
		mk, ns := mark(i == m.mcur, m.focus == paneMembers)
		edit := " "
		switch {
		case !it.p.Members[e.Name]:
			edit, ns = sWarn.Render("-"), sDim.Strikethrough(true)
		case lib == nil || !lib.Members[e.Name]:
			edit = sOK.Render("+")
		}
		ovr := "   "
		if e.Origin == "override" {
			ovr = sDim.Render("ovr")
			if disagrees(e, it.p) {
				ovr = sWarn.Render("ovr")
			}
		}
		return fit(mk+sState[e.State].Render(glyph[e.State])+edit+" "+ns.Render(trunc(e.Name, w-15)), w-10) +
			ovr + fmt.Sprintf("%7s", data.Tokens(e.Tokens))
	})...)
}

func (m *Model) popup(it *item, w, h int) []string {
	c := m.candidates(it)
	search := sDim.Render("/ searches")
	switch {
	case m.searching:
		search = sKey.Render("/") + m.query + sKey.Render("▏")
	case m.query != "":
		search = sKey.Render("/") + m.query + sDim.Render("  / edits")
	}
	l := []string{sAccent.Render("Add to "+it.p.Name) + sDim.Render(fmt.Sprintf("  %d not in it", len(c))), search}
	if len(c) == 0 {
		l = append(l, "", sDim.Render("  nothing matches"))
	}
	return append(l, grouped(c, m.acur, &m.atop, h-2, func(i int, e *data.Ext) string {
		mk, ns := mark(i == m.acur, true)
		return fit(mk+sState[e.State].Render(glyph[e.State])+" "+ns.Render(trunc(e.Name, w-11)), w-7) + fmt.Sprintf("%7s", data.Tokens(e.Tokens))
	})...)
}

var kindHead = map[data.Kind]string{data.Skill: "Skills", data.Plugin: "Plugins", data.MCP: "MCP servers"}

// grouped renders exts (sorted by kind) under kind headers, scrolled so row
// cur stays in view within h lines.
func grouped(exts []*data.Ext, cur int, top *int, h int, line func(int, *data.Ext) string) []string {
	var l []string
	at := 0
	for i, e := range exts {
		if i == 0 || e.Kind != exts[i-1].Kind {
			n := 0
			for _, x := range exts {
				if x.Kind == e.Kind {
					n++
				}
			}
			l = append(l, "", sBold.Render(kindHead[e.Kind])+sDim.Render(fmt.Sprintf("  %d", n)))
		}
		if i == cur {
			at = len(l)
		}
		l = append(l, line(i, e))
	}
	*top = max(0, min(*top, at-2), at-h+1) // keep the group header above the first row in view
	return l[min(*top, len(l)):min(*top+h, len(l))]
}

func (m *Model) impact(it *item, w int) []string {
	if it == nil {
		return nil
	}
	if m.confirm != "" {
		return m.confirmView(it, w)
	}
	var l []string
	add := func(s ...string) { l = append(l, s...) }
	here := slices.Contains(m.s.Presets, it.orig) && it.orig != ""
	title := sAccent.Render(it.p.Name)
	switch {
	case it.orig == "":
		title += sWarn.Render("  new, not written")
	case here:
		title += sOK.Render("  active here")
	default:
		title += sDim.Render("  not active here")
	}
	add(title, "")

	users := m.s.UsersOf(it.orig)
	if it.orig == "" {
		users = nil
	}
	add(sBold.Render("Used by") + sDim.Render(fmt.Sprintf("  %d projects", len(users))))
	if len(users) == 0 {
		add(sDim.Render("  no project yet"))
	}
	for _, p := range users {
		if p == m.s.Project {
			add("  " + trunc(p, w-9) + sDim.Render(" (here)"))
		} else {
			add("  " + sDim.Render(trunc(p, w-2)))
		}
	}
	if it.orig != "" {
		verb := "activating"
		if here {
			verb = "deactivating"
		}
		add("", sDim.Render("Space, "+verb+" it here:"),
			"  "+m.delta(m.s.Try(func() { m.s.Presets = toggled(m.s.Presets, it.orig) })))
	}
	add("")

	if m.dirty(it) {
		add(sBold.Render("Unwritten edits") + sWarn.Render(" *"))
		lib := m.s.Preset(it.orig)
		var plus, minus []string
		for _, e := range m.s.Exts {
			switch was := lib != nil && lib.Members[e.Name]; {
			case it.p.Members[e.Name] && !was:
				plus = append(plus, e.Name)
			case !it.p.Members[e.Name] && was:
				minus = append(minus, e.Name)
			}
		}
		if lib != nil && lib.Name != it.p.Name {
			add("  renamed from " + lib.Name)
		}
		add(names(sOK.Render("  + "), plus, w)...)
		add(names(sWarn.Render("  - "), minus, w)...)
		if here {
			add(sDim.Render("  here, once written:"), "  "+m.delta(m.tryWrite(it, false)))
		} else {
			add(sDim.Render("  not active here: no change here"))
		}
		add(sDim.Render(fmt.Sprintf("  w writes it to %d projects", len(users))))
	}
	return l
}

var kindName = map[data.Kind]string{data.Skill: "Skill", data.Plugin: "Plugin", data.MCP: "MCP server"}
var sourceLabel = map[data.Kind]string{data.Skill: "Installed", data.Plugin: "Marketplace", data.MCP: "Configured"}

// detail shows the extension highlighted in the members pane or the add list,
// modeled on the main screen's detail pane.
func (m *Model) detail(e *data.Ext, w, h int) []string {
	if e == nil {
		return []string{sDim.Render("No extension highlighted")}
	}
	l := []string{sAccent.Render(e.Name) + sDim.Render("  "+kindName[e.Kind]), ""}
	add := func(s ...string) { l = append(l, s...) }
	field := func(k, v string) { add(sDim.Render(fmt.Sprintf("%-12s", k)) + v) }
	if e.Desc != "" {
		add(wrap(e.Desc, w)...)
		add("")
	}
	field(sourceLabel[e.Kind], trunc(e.Source, w-12))
	has := func(a data.Agent, name string) string {
		if e.Agents&a != 0 {
			return sOK.Render("✓ " + name)
		}
		return sDim.Render("✗ " + name)
	}
	field("Agents", has(data.Claude, "Claude Code")+"  "+has(data.Codex, "Codex"))
	state := sState[e.State].Render(glyph[e.State] + " " + e.State.String())
	switch {
	case e.Origin == "override":
		st, from := m.s.Fallback(e)
		field("State", state+"  "+sWarn.Render("ovr"))
		field("", sDim.Render(trunc("without it: "+st.String()+" ("+from+")", w-12)))
	case e.Origin == "default" && len(m.s.Presets) == 0:
		field("State", state+sDim.Render("  agent default"))
	case e.Origin == "default":
		field("State", state+sDim.Render("  no active preset"))
	default:
		field("State", state+sDim.Render(trunc("  "+e.Origin, w-12-lipgloss.Width(state))))
	}
	if e.Cost() > 0 {
		field("Cost", data.Tokens(e.Cost())+" tokens now")
	} else {
		field("Cost", "0 now, "+data.Tokens(e.Tokens)+" when on")
	}
	var in []string
	for _, p := range m.s.PresetsOf(e) {
		if slices.Contains(m.s.Presets, p) {
			p = sAccent.Render(p) + sDim.Render(" (active)")
		}
		in = append(in, p)
	}
	if len(in) == 0 {
		in = []string{sDim.Render("none")}
	}
	field("Presets", strings.Join(in, ", "))
	if e.Kind == data.Plugin {
		add("", sBold.Render(fmt.Sprintf("Contents (%d)", len(e.Children))))
		if len(e.Children) == 0 {
			add(sDim.Render("  no skills or MCP servers"))
		} else if e.State != data.On {
			add(sDim.Render("  plugin is off: none of these load"))
		}
		for _, c := range e.Children {
			kind := map[data.Kind]string{data.Skill: "skill", data.MCP: "mcp"}[c.Kind]
			g := sState[c.State].Render(glyph[c.State])
			if e.State != data.On {
				g = sDim.Render(glyph[c.State])
			}
			add(fit(g+" "+trunc(c.Name, w-14), w-12)+sDim.Render(fmt.Sprintf("%-6s", kind))+fmt.Sprintf("%6s", data.Tokens(c.Tokens)),
				"  "+sDim.Render(trunc(c.Desc, w-2)))
		}
	}
	if len(l) > h {
		l = append(l[:h-1], sDim.Render("  ↓ more"))
	}
	return l
}

// otherDiff is what writing (or deleting) it turns on and off in another
// project. Other projects have no overrides here: with presets, an extension
// is on if one of them has it; with none, the agents' defaults turn all on.
func (m *Model) otherDiff(it *item, presets []string, del bool) (on, off []string) {
	after := slices.Clone(presets)
	if del {
		after = slices.DeleteFunc(after, func(n string) bool { return n == it.orig })
	}
	draft := func(n string) *data.Preset {
		if n == it.orig {
			return it.p
		}
		return m.s.Preset(n)
	}
	has := func(names []string, get func(string) *data.Preset, e string) bool {
		return len(names) == 0 || slices.ContainsFunc(names, func(n string) bool { p := get(n); return p != nil && p.Members[e] })
	}
	for _, e := range m.s.Exts {
		switch was, now := has(presets, m.s.Preset, e.Name), has(after, draft, e.Name); {
		case now && !was:
			on = append(on, e.Name)
		case was && !now:
			off = append(off, e.Name)
		}
	}
	return on, off
}

func (m *Model) confirmView(it *item, w int) []string {
	if m.confirm == "activate" {
		return []string{sOK.Render("Wrote preset " + it.p.Name), "", sWarn.Render("Make it active here?"),
			sDim.Render("a pending project change,"), sDim.Render("saved with s on the main screen"), "",
			sDim.Render("Activating it here:"),
			"  " + m.delta(m.s.Try(func() { m.s.Presets = toggled(m.s.Presets, it.orig) })),
			"", help("y", "make it active", "n", "not now")}
	}
	del := m.confirm == "delete"
	verb := "Write"
	switch {
	case del:
		verb = "Delete"
	case it.orig == "":
		verb = "Create"
	}
	name := it.p.Name
	if it.orig != "" && it.orig != name {
		name = it.orig + " as " + name
	}
	l := []string{sWarn.Render(verb + " preset " + name + "?"), ""}
	users := m.s.UsersOf(it.orig)
	if it.orig == "" {
		users = nil
	}
	l = append(l, sBold.Render(fmt.Sprintf("Rewrites %d projects", len(users))))
	if len(users) == 0 {
		l = append(l, sDim.Render("  no project uses it yet"))
	}
	if del && len(users) > 0 {
		l = append(l, sDim.Render("  each stops using it"))
	}
	if slices.Contains(users, m.s.Project) {
		l = append(l, "  "+trunc(m.s.Project, w-16)+sDim.Render(" (here, below)"))
	}
	for _, p := range m.s.Projects {
		if !slices.Contains(users, p.Path) {
			continue
		}
		l = append(l, "  "+trunc(p.Path, w-2))
		on, off := m.otherDiff(it, p.Presets, del)
		if len(on)+len(off) == 0 {
			l = append(l, sDim.Render("    no change"))
		}
		l = append(l, names(sOK.Render("    + "), on, w)...)
		l = append(l, names(sWarn.Render("    - "), off, w)...)
	}
	changed, total := m.tryWrite(it, del)
	l = append(l, "", sBold.Render("This project")+sDim.Render(fmt.Sprintf("  %d state changes", len(changed))))
	if len(changed) == 0 {
		l = append(l, sDim.Render("  no extension changes state here"))
	}
	for i, e := range changed {
		if i == 10 {
			l = append(l, sDim.Render(fmt.Sprintf("  +%d more", len(changed)-10)))
			break
		}
		to := data.On
		if e.State == data.On {
			to = data.Off
		}
		l = append(l, "  "+sState[e.State].Render(glyph[e.State])+" → "+sState[to].Render(glyph[to])+" "+trunc(e.Name, w-8))
	}
	kept := 0
	for _, e := range m.s.All() {
		if e.Origin == "override" {
			kept++
		}
	}
	return append(l, "", sDim.Render("Session  ")+data.Tokens(m.s.Total())+" → "+sBold.Render(data.Tokens(total))+sDim.Render(" tokens"),
		sDim.Render(fmt.Sprintf("Overrides kept  %d", kept)), "", help("y", strings.ToLower(verb), "n", "cancel"))
}

// names wraps a comma list in w cells behind a sign; wrapped lines indent under it.
func names(sign string, list []string, w int) []string {
	if len(list) == 0 {
		return nil
	}
	pad := lipgloss.Width(sign)
	l := wrap(strings.Join(list, ", "), w-pad)
	for i := range l {
		if i == 0 {
			l[i] = sign + l[i]
		} else {
			l[i] = strings.Repeat(" ", pad) + l[i]
		}
	}
	return l
}

// ---- layout helpers, copied from mainscreen ----

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
