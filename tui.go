package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/svyatov/equip/internal/equip"
)

// styles are the lipgloss styles of the TUI.
type styles struct {
	top, dim, warn, bad, cur, key, head, agent, unmeasured, pane, focused lipgloss.Style
	states                                                                map[equip.State]lipgloss.Style
	kinds                                                                 map[equip.Kind]lipgloss.Style
}

// newStyles are the styles in the colours of Catppuccin Mocha,
// github.com/catppuccin/palette.
func newStyles() styles {
	mauve := "#cba6f7"
	pane := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#45475a")).
		Padding(0, 1)

	return styles{
		top:        colour(mauve).Bold(true),
		dim:        colour("#7f849c"),
		warn:       colour("#fab387").Bold(true),
		bad:        colour("#f38ba8").Bold(true),
		cur:        colour(mauve).Bold(true),
		key:        colour(mauve),
		head:       colour("#b4befe").Bold(true),
		agent:      colour("#89b4fa"),
		unmeasured: colour("#89dceb"),
		pane:       pane,
		focused:    pane.BorderForeground(lipgloss.Color(mauve)),
		states: map[equip.State]lipgloss.Style{
			equip.On: colour("#a6e3a1"), equip.ManualOnly: colour("#f9e2af"), equip.Off: colour("#6c7086"),
		},
		kinds: map[equip.Kind]lipgloss.Style{
			equip.Skill: colour("#89b4fa"), equip.Plugin: colour("#f5c2e7"), equip.MCPServer: colour("#94e2d5"),
		},
	}
}

// colour is the style of text in the colour hex.
func colour(hex string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)) }

// The sizes of the layout, in cells.
const (
	leftWidth    = 34      // the left pane's, the facet sidebar or the presets library, which a library row fills
	wideWidth    = 100     // the least terminal width that shows the sidebar
	minWidth     = 60      // the least terminal width the TUI draws in
	minPane      = 7       // the least pane height the TUI draws in, 3 rows of the list
	listShare    = 5       // the list takes 2 of this many parts of the width left of the sidebar
	minList      = 30      // the least width of the list's pane
	maxList      = 64      // the most width of the list's pane, which keeps a row's cost near its name
	pageRows     = 10      // the rows pgup and pgdown move
	halfPage     = 5       // the rows ctrl+d and ctrl+u move
	leftSections = 2       // the sections of the key list's left column, Move and Change
	endRows      = 1 << 20 // the rows home and end move, past any list's ends
	paneWidth    = 4       // the cells a pane's border and padding take across
	paneHeight   = 2       // the lines a pane's border takes down
	listHead     = 2       // the list's title and search lines
	rowMarks     = 6       // the cells of a list row's mark, glyph, unsaved marker and spaces
	facetMarks   = 6       // the cells of a sidebar line's mark and count
	pairSize     = 2       // a key and what it does, in the key help
)

// probedMsg reports that a probe of an MCP server ended, with its error.
type probedMsg struct{ err error }

// model is the Bubble Tea root model over a Session.
type model struct {
	style     styles
	s         *equip.Session
	home      string // the user's home dir, which the top line writes as ~
	flash     string
	query     string    // the search: the list keeps the rows whose names contain it
	key       string    // of the highlighted row, which the highlight stays on while the list has it
	pinned    string    // of a row the keys changed, which the list keeps until the highlight leaves it
	orphans   []string  // the records of a moved repo the first open offers, while it asks to adopt one
	ws        workspace // the presets workspace
	width     int       // of the terminal
	height    int       // of the terminal
	cur       int       // the highlighted row
	top       int       // the first row the list shows
	detailTop int       // the first line under its title the detail pane shows
	facet     int       // the picked facet
	server    int       // the highlighted MCP server among the highlighted plugin's contents
	focus     int       // the pane the keys are on: onFacets, onList or onDetail
	quitting  bool      // asking to quit with unsaved changes
	searching bool      // the keys type into the search
	showKeys  bool      // the key list covers the screen until the next key
}

// The panes of the main screen the keys can be on, left to right. In the
// detail pane they move among the MCP servers of a plugin, else scroll it.
const (
	onFacets = iota
	onList
	onDetail
)

// digitKeys is the count of the digit keys 1 to 9, which pick a record in
// the adopt prompt. ponytail: a tenth record is left out; page the prompt if
// one repo ever leaves that many behind.
const digitKeys = 9

// escKey is the key that backs out: of the search, or of the presets
// workspace.
const escKey = "esc"

// enterKey ends typing: of the search, or of a preset's name.
const enterKey = "enter"

// spaceKey toggles what the keys are on.
const spaceKey = "space"

// step is the move of key, reporting whether it is an arrow key, a page key,
// home or end, or the vi key of one.
func step(key string) (int, bool) {
	delta, isStep := map[string]int{
		"up": -1, "k": -1, "down": 1, "j": 1,
		"ctrl+u": -halfPage, "ctrl+d": halfPage,
		"pgup": -pageRows, "ctrl+b": -pageRows, "pgdown": pageRows, "ctrl+f": pageRows,
		"home": -endRows, "g": -endRows, "end": endRows, "G": endRows,
	}[key]

	return delta, isStep
}

// pane is the move of key among the panes, reporting whether it moves: h and
// l and the arrows stop at the ends, tab and shift+tab wrap around, and enter
// goes in.
func pane(key string) (int, bool, bool) {
	switch key {
	case "h", "left":
		return -1, false, true
	case "l", "right", enterKey:
		return 1, false, true
	case "shift+tab":
		return -1, true, true
	case "tab":
		return 1, true, true
	}

	return 0, false, false
}

// newTUI is the model of a fresh TUI over session, for the user with home.
func newTUI(session *equip.Session, home string) *model {
	view := session.View()
	orphans := view.Orphans
	tui := &model{
		s: session, home: home, style: newStyles(), width: 0, height: 0, cur: 0, top: 0, detailTop: 0, facet: 0, server: 0,
		focus: onList, quitting: false, showKeys: false,
		searching: false, flash: "", query: "", key: "", pinned: "", orphans: orphans[:min(len(orphans), digitKeys)],
		ws: newWorkspace(view),
	}
	tui.clamp()

	return tui
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		m.focus = max(m.focus, m.leftmost())

		return m, nil
	}

	if probed, ok := msg.(probedMsg); ok {
		m.flash = ""
		if probed.err != nil {
			m.flash = m.style.bad.Render("measure failed: " + probed.err.Error())
		}

		return m, nil
	}

	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}

	key := keyMsg.String()

	m.flash = ""
	if m.quitting {
		m.quitting = false
		if key == "y" {
			return m, tea.Quit
		}

		return m, nil
	}

	cmd := m.dispatch(keyMsg)
	m.clamp()

	return m, cmd
}

func (m *model) View() tea.View {
	screen := m.mainView
	if m.ws.open {
		screen = m.workspaceView
	}

	footer := m.footer()
	if m.ws.open {
		footer = m.workspaceFooter()
	}

	// The panes take the lines between the top line and the footer.
	height := m.height - 1 - lipgloss.Height(footer)

	content := "terminal too small"

	switch {
	case m.width < minWidth || height < minPane:
	case m.showKeys:
		content = block(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			m.style.focused.Render(m.keyList())), m.width, m.height)
	default:
		content = screen(height) + "\n" + block(footer, m.width, lipgloss.Height(footer))
	}

	view := tea.NewView(content)
	view.AltScreen = true

	return view
}

// mainView is the main screen's top line and panes, height lines tall.
func (m *model) mainView(height int) string {
	session := m.s.View()

	top := []string{m.style.top.Render("equip") + "  " + tilde(session.Project.Path, m.home)}
	if len(session.Presets) > 0 {
		top = append(top, m.style.dim.Render("presets ")+m.style.cur.Render(strings.Join(session.Presets, " + ")))
	}

	for _, agent := range equip.Agents() {
		top = append(top, agentTotal(agent, session.Totals[agent], m.style))
	}

	if session.Unsaved > 0 {
		top = append(top, m.style.warn.Render(fmt.Sprintf("%d unsaved", session.Unsaved)))
	}

	rows := m.rows(session)

	var panes []string

	rest := m.width
	if m.leftmost() == onFacets {
		panes = append(panes, m.box(m.sidebar(session.Facets), leftWidth, height, m.focus == onFacets))
		rest -= leftWidth
	}

	listWidth := min(max(minList, rest*2/listShare), maxList)
	panes = append(panes, m.box(m.list(rows, session.Facets[m.facet].Name, listWidth-paneWidth, height-paneHeight),
		listWidth, height, m.focus == onList))

	detail := ""

	if m.cur < len(rows) {
		row, server := rows[m.cur], ""
		if servers := m.servers(rows); m.focus == onDetail && len(servers) > 0 {
			server = servers[m.server]
		}

		detail = m.detail(session, row, m.s.Detail(row.Key), server, rest-listWidth-paneWidth, height-paneHeight)
	}

	panes = append(panes, m.box(detail, rest-listWidth, height, m.focus == onDetail))

	return fit(strings.Join(top, "  "), m.width) + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, panes...)
}

// tilde is path with the dir home at its start written as ~.
func tilde(path, home string) string {
	sep := string(filepath.Separator)
	if rest, ok := strings.CutPrefix(path, home+sep); ok && home != "" {
		return "~" + sep + rest
	}

	return path
}

// box is text in a pane of width by height cells, its border in the accent
// when focused.
func (m *model) box(text string, width, height int, focused bool) string {
	style := m.style.pane
	if focused {
		style = m.style.focused
	}

	return style.Render(block(text, width-paneWidth, height-paneHeight))
}

// block is the lines of text cut or padded to width by height cells.
func block(text string, width, height int) string {
	lines := strings.Split(text, "\n")
	out := make([]string, height)

	for i := range out {
		if i < len(lines) {
			out[i] = fit(lines[i], width)
		} else {
			out[i] = strings.Repeat(" ", width)
		}
	}

	return strings.Join(out, "\n")
}

// cut is one line cut with … to at most width cells.
func cut(line string, width int) string {
	if lipgloss.Width(line) <= width {
		return line
	}

	return lipgloss.NewStyle().MaxWidth(max(width-1, 0)).Render(line) + "…"
}

// fit is one line cut with … or padded to width cells.
func fit(line string, width int) string {
	line = cut(line, width)

	return line + strings.Repeat(" ", max(width-lipgloss.Width(line), 0))
}

// window is the at most height of lines from top, moved the least to keep
// line cur among them, and the top it moved to. When lines go on below, the
// last line says how many more.
func (m *model) window(lines []string, top, cur, height int) ([]string, int) {
	if len(lines) <= height {
		return lines, 0
	}

	above := height - 1 // the lines above the last, which may say how many more
	top = min(max(min(top, cur), cur-above+1, 0), len(lines)-height)
	shown := slices.Clone(lines[top : top+height])

	if more := len(lines) - top - height + 1; more > 1 {
		shown[height-1] = m.style.dim.Render(fmt.Sprintf("  ↓ %d more", more))
	}

	return shown, top
}

// help is the key help of pairs of a key and what it does, wrapped at the
// terminal width.
func (m *model) help(pairs ...string) string {
	var lines []string

	line := ""

	for pair := range slices.Chunk(pairs, pairSize) {
		part := m.style.key.Render(pair[0]) + " " + m.style.dim.Render(pair[len(pair)-1])

		switch {
		case line == "":
			line = part
		case lipgloss.Width(line+"  "+part) > m.width:
			lines, line = append(lines, line), part
		default:
			line += "  " + part
		}
	}

	return strings.Join(append(lines, line), "\n")
}

// glyph is the list mark of st in its colour.
func (m *model) glyph(st equip.State) string { return m.style.states[st].Render(glyph(st)) }

// dispatch acts on keyMsg where the keys go: the adopt prompt, the presets
// workspace, the search, or the main screen. ctrl+c quits from any of them.
func (m *model) dispatch(keyMsg tea.KeyPressMsg) tea.Cmd {
	key := keyMsg.String()

	switch {
	case key == "ctrl+c":
		return m.quit()
	case m.showKeys:
		m.showKeys = false
	case len(m.orphans) > 0:
		m.adopt(key)
	case key == "?" && !m.typing():
		m.showKeys = true
	case m.ws.open:
		m.inWorkspace(keyMsg)
	case m.searching:
		m.search(keyMsg)
	case !m.narrow(key):
		return m.press(key)
	}

	return nil
}

// adopt acts on key while asking to adopt the record of a moved repo: a
// number adopts the record it picks among the orphans, and n keeps the
// states imported at open.
func (m *model) adopt(key string) {
	if key == "n" {
		m.orphans = nil

		return
	}

	i, err := strconv.Atoi(key)
	if err != nil || i < 1 || i > len(m.orphans) {
		return
	}

	err = m.s.Adopt(m.orphans[i-1])
	if err != nil {
		m.flash = m.style.bad.Render("adopt failed: " + err.Error())
	}

	m.orphans = nil
}

// footer is the bottom line: the quit guard, the adopt prompt, the flash, or
// the keys.
func (m *model) footer() string {
	switch {
	case len(m.orphans) > 0:
		lines := []string{m.style.warn.Render("Adopt this repo's record from a path that is gone?")}
		for i, path := range m.orphans {
			lines = append(lines, fmt.Sprintf("  %d %s", i+1, path))
		}

		return strings.Join(append(lines, m.style.dim.Render("its number adopts it  n start fresh")), "\n")
	case m.quitting:
		return m.style.warn.Render(fmt.Sprintf("%d unsaved changes. Quit without saving? y/n", m.unsaved()))
	case m.flash != "":
		return m.flash
	case m.searching:
		return m.help("type", "to search", "enter", "done", "esc", "clear")
	case m.focus == onFacets:
		return m.help("j/k", "facet", "l", "list", "/", "search", "?", "keys", "q", "quit")
	case m.focus == onDetail && len(m.servers(m.rows(m.s.View()))) > 0:
		return m.help("j/k", "MCP server", "space", "cycle state", "x", "drop override", "m", "measure", "h", "list",
			"s", "save", "?", "keys", "q", "quit")
	case m.focus == onDetail:
		return m.help("j/k", "scroll", "space", "cycle state", "h", "list", "s", "save", "?", "keys", "q", "quit")
	}

	return m.help("j/k", "move", "h/l", "pane", "space", "cycle state", "m", "measure", "/", "search", "p", "presets",
		"s", "save", "?", "keys", "q", "quit")
}

// typing reports whether the keys type text: into a search or a preset's
// name.
func (m *model) typing() bool { return m.searching || m.ws.searching || m.ws.naming != "" }

// leftmost is the leftmost pane the keys can be on: the sidebar, unless the
// terminal is too narrow to show it.
func (m *model) leftmost() int {
	if m.width < wideWidth {
		return onList
	}

	return onFacets
}

// keyList is every key of the TUI, which ? shows.
func (m *model) keyList() string {
	sections := []struct {
		head  string
		pairs []string
	}{
		{"Move", []string{
			"j k ↑ ↓", "up and down", "g G", "first and last", "ctrl+d ctrl+u", "half a page down and up",
			"pgdn pgup", "a page down and up", "h l ← →", "pane left and right", "tab shift+tab", "next and previous pane",
			"enter", "into the next pane",
		}},
		{"Change", []string{
			"space", "cycle state", "1 2 3", "set state", "x", "drop override", "m", "measure an MCP server", "s", "save",
		}},
		{"Find", []string{"/", "search by name", "[ ]", "previous and next facet", "esc", "clear, back to the list"}},
		{"Presets", []string{
			"p", "open the presets", "space", "make active here, or add and remove", "n r d", "new, rename, delete",
			"a w", "add members, write the preset", "esc", "back",
		}},
		{"Other", []string{"?", "these keys", "q", "quit"}},
	}

	blocks := make([]string, 0, len(sections))

	for _, section := range sections {
		lines := []string{m.style.head.Render(section.head)}

		for pair := range slices.Chunk(section.pairs, pairSize) {
			lines = append(lines, "  "+m.style.key.Render(fmt.Sprintf("%-14s", pair[0]))+" "+pair[len(pair)-1])
		}

		blocks = append(blocks, strings.Join(lines, "\n"))
	}

	// Move and Change go in the left column, the rest in the right.
	return lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(blocks[:leftSections], "\n\n"), "   ",
		strings.Join(blocks[leftSections:], "\n\n")) + "\n\n" + m.style.dim.Render("any key closes")
}

// mark is the start of a list line: the highlight's when on.
func (m *model) mark(on bool) string {
	if on {
		return m.style.cur.Render("▸ ")
	}

	return "  "
}

// sidebar is the facet sidebar, with the picked one of facets highlighted.
func (m *model) sidebar(facets []equip.Facet) string {
	lines := make([]string, 0, len(facets))

	for index, facet := range facets {
		if facet.NewGroup {
			lines = append(lines, "")
		}

		name, count := fmt.Sprintf("%-*s", leftWidth-paneWidth-facetMarks, facet.Name), fmt.Sprintf(" %3d", facet.Count())
		if index == m.facet {
			lines = append(lines, m.style.cur.Render("▸ "+name+count))
		} else {
			lines = append(lines, "  "+name+m.style.dim.Render(count))
		}
	}

	return strings.Join(lines, "\n")
}

// list is the list pane, width by height cells: the facet's name and row
// count, the search, and the rows it scrolls to keep the highlight in.
func (m *model) list(rows []equip.Row, facet string, width, height int) string {
	title := m.style.head.Render(facet) + m.style.dim.Render(fmt.Sprintf("  %d", len(rows)))
	if m.width < wideWidth {
		title += m.style.dim.Render("  [ ] facet")
	}

	search := ""
	if m.searching || m.query != "" {
		search = m.style.cur.Render("/" + m.query)
	}

	if len(rows) == 0 {
		return title + "\n" + search + "\n" + m.style.dim.Render("  nothing matches")
	}

	lines := make([]string, 0, len(rows))

	for index, row := range rows {
		cost := costOf(row.CostUnknown, row.Cost)
		name := cut(row.Name, width-rowMarks-len(cost))

		if index == m.cur {
			name = m.style.cur.Render(name)
		}

		if row.Unsaved {
			name += m.style.warn.Render("*")
		}

		lines = append(lines, fit(m.mark(index == m.cur)+m.glyph(row.State)+" "+name, width-len(cost))+
			m.style.dim.Render(cost))
	}

	lines, m.top = m.window(lines, m.top, m.cur, height-listHead)

	return title + "\n" + search + "\n" + strings.Join(lines, "\n")
}

// search acts on keyMsg while the keys type into the search: enter ends it,
// and esc clears it too. A key that types nothing does nothing.
func (m *model) search(keyMsg tea.KeyPressMsg) {
	switch keyMsg.String() {
	case enterKey:
		m.searching = false
	case escKey:
		m.searching, m.query = false, ""
	default:
		m.query = typeInto(m.query, keyMsg)
		if keyMsg.Text != "" {
			m.first()
		}
	}
}

// first highlights the first row of a list narrowed anew.
func (m *model) first() { m.cur, m.top, m.key, m.pinned = 0, 0, "", "" }

// clamp keeps the highlight on its row while the list has it, else at its
// place in the list. The list drops a pinned row once the highlight leaves
// it, and the keys leave the detail pane of a row the highlight leaves.
func (m *model) clamp() {
	rows := m.rows(m.s.View())
	if i := index(rows, m.key); i >= 0 {
		m.cur = i
	}

	m.cur = max(min(m.cur, len(rows)-1), 0)

	key := ""
	if m.cur < len(rows) {
		key = rows[m.cur].Key
	}

	if key != m.pinned {
		m.pinned = ""
		rows = m.rows(m.s.View())
		m.cur = max(index(rows, key), 0)
	}

	if key != m.key {
		m.server, m.detailTop = 0, 0
		if m.focus == onDetail {
			m.focus = onList
		}
	}

	m.key = key
	m.server = max(min(m.server, len(m.servers(rows))-1), 0)
}

// index is the index of the row with key among rows, or -1.
func index(rows []equip.Row, key string) int {
	return slices.IndexFunc(rows, func(row equip.Row) bool { return row.Key == key })
}

// rows are the rows of session the picked facet and the search keep, and the
// pinned row.
func (m *model) rows(session equip.View) []equip.Row {
	facet, query := session.Facets[m.facet], strings.ToLower(m.query)

	var rows []equip.Row

	for _, row := range session.Rows {
		if row.Key == m.pinned || facet.Has(row) && m.matches(row, query) {
			rows = append(rows, row)
		}
	}

	return rows
}

// matches reports whether the name of row, or of one of its MCP servers,
// contains query, in lower case.
func (m *model) matches(row equip.Row, query string) bool {
	return strings.Contains(strings.ToLower(row.Name), query) ||
		slices.ContainsFunc(m.s.Detail(row.Key).Contents, func(content equip.Content) bool {
			return content.Kind == equip.MCPServer && strings.Contains(strings.ToLower(content.Name), query)
		})
}

// press acts on key on the main screen.
func (m *model) press(key string) tea.Cmd {
	rows := m.rows(m.s.View())
	servers := m.servers(rows)

	if delta, ok := step(key); ok {
		m.move(delta, len(servers))

		return nil
	}

	if delta, wraps, ok := pane(key); ok {
		m.moveFocus(delta, wraps)

		return nil
	}

	switch key {
	case "q":
		return m.quit()
	case "1", "2", "3", "x", "m", spaceKey:
		if target, ok := m.target(rows, servers); ok {
			// The list keeps the row even when the key takes it out of the
			// facet, so a second press does not act on the next row.
			m.pinned = m.key

			return m.act(key, target)
		}
	case "p":
		m.openWorkspace()
	case "s":
		err := m.s.Save()
		if err != nil {
			m.flash = m.style.bad.Render("save failed: " + err.Error())
		}
	}

	return nil
}

// narrow acts on key if it narrows the list: / starts the search, esc clears
// it and puts the keys back on the list, and [ and ] pick the previous and
// next facet. It reports whether it did.
func (m *model) narrow(key string) bool {
	facets := len(m.s.View().Facets)

	switch key {
	case "/":
		m.searching, m.focus = true, onList
	case escKey:
		m.query, m.focus = "", onList
	case "[", "]":
		m.facet = (m.facet + map[string]int{"[": -1, "]": 1}[key] + facets) % facets
		m.first()
	default:
		return false
	}

	return true
}

// move moves the highlight by delta in the pane the keys are on: among the
// facets, among the rows, or among the count servers of the highlighted
// plugin, else it scrolls the detail pane. clamp keeps it on them, and the
// detail pane keeps its scroll in its lines.
func (m *model) move(delta, servers int) {
	switch {
	case m.focus == onFacets:
		m.facet = max(min(m.facet+delta, len(m.s.View().Facets)-1), 0)
		m.first()
	case m.focus == onList:
		m.cur, m.key = m.cur+delta, ""
	case servers > 0:
		m.server += delta
	default:
		m.detailTop = max(m.detailTop+delta, 0)
	}
}

// moveFocus moves the keys by delta among the panes, wrapping around at the
// ends when wraps.
func (m *model) moveFocus(delta int, wraps bool) {
	lo := m.leftmost()
	if wraps {
		count := onDetail - lo + 1
		m.focus = lo + (m.focus-lo+delta+count)%count
	} else {
		m.focus = max(min(m.focus+delta, onDetail), lo)
	}
}

// servers are the keys of the MCP servers among the contents of the
// highlighted row of rows.
func (m *model) servers(rows []equip.Row) []string {
	if m.cur >= len(rows) {
		return nil
	}

	var keys []string

	for _, content := range m.s.Detail(rows[m.cur].Key).Contents {
		if content.Key != "" {
			keys = append(keys, content.Key)
		}
	}

	return keys
}

// target is the key the state keys act on: the highlighted MCP server of
// servers while the keys are on the detail pane, else the highlighted row of
// rows. It reports whether there is one.
func (m *model) target(rows []equip.Row, servers []string) (string, bool) {
	if m.focus == onDetail && len(servers) > 0 {
		return servers[m.server], true
	}

	if m.cur < len(rows) {
		return rows[m.cur].Key, true
	}

	return "", false
}

// act acts on the extension with target: x drops its Override, m measures
// its cost, space sets the state after its own, and a number key sets the
// state it picks.
func (m *model) act(key, target string) tea.Cmd {
	switch key {
	case "x":
		m.s.DropOverride(target)
	case spaceKey:
		states := m.s.Detail(target).States
		m.setState(target, (slices.Index(states, m.state(target))+1)%max(len(states), 1))
	case "m":
		// The probe runs as a command, so the TUI stays live meanwhile.
		probe := m.s.ProbeCost(target)
		m.flash = m.style.dim.Render("measuring " + target + "…")

		return func() tea.Msg { return probedMsg{err: probe()} }
	default:
		m.setState(target, int(key[0]-'1'))
	}

	return nil
}

// state is the pending state of the extension with key: the highlighted row,
// or one of its MCP servers.
func (m *model) state(key string) equip.State {
	rows := m.rows(m.s.View())
	for _, content := range m.s.Detail(rows[m.cur].Key).Contents {
		if content.Key == key {
			return content.State
		}
	}

	return rows[m.cur].State
}

// setState sets the extension with key to the state the key numbered i picks
// among the states it offers.
func (m *model) setState(key string, i int) {
	if states := m.s.Detail(key).States; i < len(states) {
		m.s.SetState(key, states[i])
	}
}

// quit quits, or first asks to confirm with unsaved changes.
func (m *model) quit() tea.Cmd {
	if m.unsaved() > 0 {
		m.quitting = true

		return nil
	}

	return tea.Quit
}

// unsaved counts the changes quitting drops: the pending ones, and a preset
// with unwritten edits.
func (m *model) unsaved() int {
	count := m.s.View().Unsaved
	if m.s.Unwritten() {
		count++
	}

	return count
}
