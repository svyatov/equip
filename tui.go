package main

import (
	"errors"
	"fmt"
	"math"
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
	dim, warn, bad, ok, cur, key, chip, head, agent, unmeasured, muted, pane, focused lipgloss.Style
	logo                                                                              string
	states                                                                            map[equip.State]lipgloss.Style
	kinds                                                                             map[equip.Kind]lipgloss.Style
	bars                                                                              []lipgloss.Style // of each of bars
}

// bars are the bars of a cost, from the least to the most tokens.
const bars = "▁▂▃▄▅▆▇█"

// newStyles are the styles in the colours of Catppuccin Mocha,
// github.com/catppuccin/palette.
func newStyles() styles {
	mauve, green, yellow, peach, red := "#cba6f7", "#a6e3a1", "#f9e2af", "#fab387", "#f38ba8"
	pane := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#45475a")).
		Padding(0, 1)

	return styles{
		dim:        colour("#7f849c"),
		warn:       colour(peach).Bold(true),
		bad:        colour(red).Bold(true),
		ok:         colour(green).Bold(true),
		cur:        colour(mauve).Bold(true),
		key:        colour(mauve),
		chip:       colour(mauve).Bold(true).Background(lipgloss.Color("#313244")).Padding(0, 1),
		head:       colour("#b4befe").Bold(true),
		agent:      colour("#89b4fa"),
		unmeasured: colour("#89dceb"),
		muted:      colour("#585b70"),
		pane:       pane,
		focused:    pane.BorderForeground(lipgloss.Color(mauve)),
		// The shades of an ANSI art logo, as on a BBS.
		logo: colour(mauve).Render("░▒▓") +
			colour("#1e1e2e").Background(lipgloss.Color(mauve)).Bold(true).Render(" equip ") +
			colour(mauve).Render("▓▒░"),
		states: map[equip.State]lipgloss.Style{
			equip.On: colour(green), equip.ManualOnly: colour(yellow), equip.Off: colour("#6c7086"),
		},
		kinds: map[equip.Kind]lipgloss.Style{
			equip.Skill: colour("#89b4fa"), equip.Plugin: colour("#f5c2e7"), equip.MCPServer: colour("#94e2d5"),
		},
		bars: []lipgloss.Style{
			colour(green), colour(green), colour(green), colour(green), colour(yellow), colour(yellow), colour(peach),
			colour(red),
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
	rowMarks     = 6       // the cells of a list row's mark, glyph, unsaved marker and spaces
	facetMarks   = 6       // the cells of a sidebar line's mark and count
	pairSize     = 2       // a key and what it does, in the key help
	partGap      = 2       // the cells between two parts of the status or the key help
	corners      = 2       // the cells of a pane's top corners
	rightMarks   = 3       // the cells of a space each side of a pane's right title, and a line after
	titleMarks   = 4       // the cells of a line each side of a pane's title, and a space each side
	markWidth    = 2       // the cells of the highlight's mark
	markers      = 2       // the lines of a window that may say how many more are above and below
	barsPerTen   = 2       // the bars of a cost a power of ten apart
	minPath      = 12      // the least width the status shows the project's path in
)

// probedMsg reports that a probe of an MCP server ended, with its error, and
// whether opening started it.
type probedMsg struct {
	err  error
	auto bool
}

// probeSlots is how many MCP servers opening measures at once.
const probeSlots = 4

// model is the Bubble Tea root model over a Session.
type model struct {
	style     styles
	s         *equip.Session
	slots     chan struct{} // one per probe that runs, up to probeSlots
	home      string        // the user's home dir, which the top line writes as ~
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
	probing   int       // the MCP servers opening measures, until each probe ends
	failed    int       // of those, the ones whose probe failed
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
		focus: onList, quitting: false, showKeys: false, probing: 0, failed: 0, slots: make(chan struct{}, probeSlots),
		searching: false, flash: "", query: "", key: "", pinned: "", orphans: orphans[:min(len(orphans), digitKeys)],
		ws: newWorkspace(view),
	}
	tui.clamp()

	return tui
}

// Init measures every MCP server equip may start and has not measured, in
// the background.
func (m *model) Init() tea.Cmd {
	keys := m.s.Unmeasured()
	m.probing = len(keys)

	cmds := make([]tea.Cmd, 0, len(keys))
	for _, key := range keys {
		cmds = append(cmds, m.probe(key, true))
	}

	return tea.Batch(cmds...)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = size.Width, size.Height
		m.focus = max(m.focus, m.leftmost())

		return m, nil
	}

	if probed, ok := msg.(probedMsg); ok {
		m.probed(probed)

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
	screen, footer := m.mainView, m.footer()
	if m.ws.open {
		screen, footer = m.workspaceView, m.workspaceFooter()
	}

	// The status and the panes take the lines above the footer.
	height := m.height - lipgloss.Height(footer)

	content := "terminal too small"

	switch {
	case m.width < minWidth || height <= minPane:
	case m.showKeys:
		keys := m.keyList()
		content = block(lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center,
			m.box("keys", "", keys, lipgloss.Width(keys)+paneWidth, lipgloss.Height(keys)+paneHeight, true)),
			m.width, m.height)
	default:
		content = screen(height) + "\n" + block(footer, m.width, lipgloss.Height(footer))
	}

	view := tea.NewView(content)
	view.AltScreen = true

	return view
}

// probe is the command that measures the MCP server with key, once one of
// the probe slots frees, so the TUI stays live meanwhile. auto marks one
// opening started.
func (m *model) probe(key string, auto bool) tea.Cmd {
	probe, slots := m.s.ProbeCost(key), m.slots

	return func() tea.Msg {
		slots <- struct{}{}
		defer func() { <-slots }()

		return probedMsg{err: probe(), auto: auto}
	}
}

// probed takes the end of a probe: the one m started says why it failed,
// and those opening started count down, then say how many failed.
func (m *model) probed(probed probedMsg) {
	if !probed.auto {
		m.flash = ""
		if probed.err != nil {
			m.flash = m.style.bad.Render("measure failed: " + probed.err.Error())
		}

		return
	}

	// A server that refuses equip for good says why in the detail pane.
	m.probing--
	if probed.err != nil && !errors.Is(probed.err, equip.ErrRefused) {
		m.failed++
	}

	if m.probing == 0 && m.failed > 0 {
		m.flash = m.style.warn.Render(fmt.Sprintf("%d MCP %s could not be measured, m on one says why",
			m.failed, plural(m.failed, "server", "servers")))
	}
}

// plural is one when count is 1, else many.
func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}

	return many
}

// mainView is the main screen's status and panes, height lines tall.
func (m *model) mainView(height int) string {
	session := m.s.View()

	var parts []string
	if len(session.Presets) > 0 {
		parts = append(parts, m.style.dim.Render("presets ")+m.style.cur.Render(strings.Join(session.Presets, " + ")))
	}

	for _, agent := range equip.Agents() {
		parts = append(parts, agentTotal(agent, session.Totals[agent], m.style))
	}

	if m.probing > 0 {
		parts = append(parts, m.style.unmeasured.Render(fmt.Sprintf("measuring %d…", m.probing)))
	}

	if session.Unsaved > 0 {
		parts = append(parts, m.style.warn.Render(fmt.Sprintf("%d unsaved", session.Unsaved)))
	}

	top := m.status(tilde(session.Project.Path, m.home), parts...)
	height -= lipgloss.Height(top)
	rows := m.rows(session)

	var panes []string

	rest := m.width
	if m.leftmost() == onFacets {
		panes = append(panes, m.box("Facets", "", m.sidebar(session, height-paneHeight), leftWidth, height,
			m.focus == onFacets))
		rest -= leftWidth
	}

	listWidth := min(max(minList, rest*2/listShare), maxList)
	title, search, list := m.list(session, rows, listWidth-paneWidth, height-paneHeight)
	panes = append(panes, m.box(title, search, list, listWidth, height, m.focus == onList))

	title, detail := "", ""

	if m.cur < len(rows) {
		row, server := rows[m.cur], ""
		if servers := m.servers(rows); m.focus == onDetail && len(servers) > 0 {
			server = servers[m.server]
		}

		title = m.detailTitle(row)
		detail = m.detail(session, row, m.s.Detail(row.Key), server, rest-listWidth-paneWidth, height-paneHeight)
	}

	panes = append(panes, m.box(title, "", detail, rest-listWidth, height, m.focus == onDetail))

	return block(top, m.width, lipgloss.Height(top)) + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, panes...)
}

// status is the lines above the panes: the logo, then parts, wrapped at the
// terminal width, with path in what the first line has left, cut from its
// start.
func (m *model) status(path string, parts ...string) string {
	lines := m.wrap(append([]string{m.style.logo}, parts...))
	if room := m.width - lipgloss.Width(lines[0]) - partGap; path != "" && room >= minPath {
		lines[0] = m.style.logo + "  " + cutStart(path, room) + strings.TrimPrefix(lines[0], m.style.logo)
	}

	return strings.Join(lines, "\n")
}

// cutStart is text cut with … at its start to at most width cells.
func cutStart(text string, width int) string {
	runes := []rune(text)
	if len(runes) <= width {
		return text
	}

	return "…" + string(runes[len(runes)-width+1:])
}

// tilde is path with the dir home at its start written as ~.
func tilde(path, home string) string {
	sep := string(filepath.Separator)
	if rest, ok := strings.CutPrefix(path, home+sep); ok && home != "" {
		return "~" + sep + rest
	}

	return path
}

// box is text in a pane of width by height cells, with title set into the
// left of its top border and right into the right of it, its border in the
// accent when focused.
func (m *model) box(title, right, text string, width, height int, focused bool) string {
	style := m.style.pane
	if focused {
		style = m.style.focused
	}

	border := lipgloss.NewStyle().Foreground(style.GetBorderTopForeground())
	inner := width - corners

	tail := 0 // the cells of right and its marks
	if right != "" {
		tail = lipgloss.Width(right) + rightMarks
	}

	title = cut(title, max(inner-tail-titleMarks, 0))
	head := 1 // the cells of the line before title, and title with a space each side

	if title != "" {
		title = " " + title + " "
		head += lipgloss.Width(title)
	}

	top := border.Render("╭─") + title + border.Render(strings.Repeat("─", max(inner-head-tail, 0)))
	if right != "" {
		top += " " + right + border.Render(" ─")
	}

	return top + border.Render("╮") + "\n" +
		style.BorderTop(false).Render(block(text, width-paneWidth, height-paneHeight))
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
// line cur among them, and the top it moved to. When lines go on above or
// below, the first or last line says how many more, so cur stays off them.
func (m *model) window(lines []string, top, cur, height int) ([]string, int) {
	if len(lines) <= height {
		return lines, 0
	}

	top = min(max(min(top, cur-1), cur-height+markers, 0), len(lines)-height)
	shown := slices.Clone(lines[top : top+height])

	if top > 0 {
		shown[0] = m.style.dim.Render(fmt.Sprintf("  ↑ %d more", top+1))
	}

	if more := len(lines) - top - height + 1; more > 1 {
		shown[height-1] = m.style.dim.Render(fmt.Sprintf("  ↓ %d more", more))
	}

	return shown, top
}

// help is the key help of pairs of a key and what it does, wrapped at the
// terminal width.
func (m *model) help(pairs ...string) string {
	parts := make([]string, 0, len(pairs)/pairSize)
	for pair := range slices.Chunk(pairs, pairSize) {
		parts = append(parts, m.style.chip.Render(pair[0])+" "+m.style.dim.Render(pair[len(pair)-1]))
	}

	return strings.Join(m.wrap(parts), "\n")
}

// wrap is parts joined into lines, two spaces apart, wrapped at the terminal
// width.
func (m *model) wrap(parts []string) []string {
	var lines []string

	line := ""

	for _, part := range parts {
		switch {
		case line == "":
			line = part
		case lipgloss.Width(line+"  "+part) > m.width:
			lines, line = append(lines, line), part
		default:
			line += "  " + part
		}
	}

	return append(lines, line)
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
			"enter", "into the next pane, or to a plugin skill's plugin",
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
	return m.style.logo + "\n\n" + lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(blocks[:leftSections], "\n\n"),
		"   ", strings.Join(blocks[leftSections:], "\n\n")) + "\n\n" + m.style.dim.Render("any key closes")
}

// mark is the start of a list line: the highlight's when on.
func (m *model) mark(on bool) string {
	if on {
		return m.style.cur.Render("▸ ")
	}

	return "  "
}

// sidebar is the facet sidebar of session, height lines tall, with the
// picked facet highlighted, and the legend of the list's marks at its foot
// when it has room. Each facet counts the rows the search keeps.
func (m *model) sidebar(session equip.View, height int) string {
	facets := m.facets(session)
	legend := []string{
		"  " + m.glyph(equip.On) + m.style.dim.Render(" on ") + m.glyph(equip.ManualOnly) +
			m.style.dim.Render(" manual-only ") + m.glyph(equip.Off) + m.style.dim.Render(" off"),
		"  " + m.style.bars[1].Render("▂") + m.style.bars[3].Render("▄") + m.style.bars[5].Render("▆") +
			m.style.bars[7].Render("█") + m.style.dim.Render(" tokens a session"),
		"  " + m.style.unmeasured.Render("?") + m.style.dim.Render("    not measured yet"),
	}

	if gap := height - len(facets) - len(legend); gap > 0 {
		facets = append(append(facets, make([]string, gap)...), legend...)
	}

	return strings.Join(facets, "\n")
}

// facets are the sidebar's lines of the facets of session, with the picked
// one highlighted.
func (m *model) facets(session equip.View) []string {
	lines := make([]string, 0, len(session.Facets))

	for index, facet := range session.Facets {
		if facet.NewGroup {
			lines = append(lines, "")
		}

		name := fmt.Sprintf("%-*s", leftWidth-paneWidth-facetMarks, facet.Name)
		count := fmt.Sprintf(" %3d", m.matching(session, facet))

		if index == m.facet {
			lines = append(lines, m.style.cur.Render("▸ "+name+count))
		} else {
			lines = append(lines, "  "+name+m.style.dim.Render(count))
		}
	}

	return lines
}

// list is the list pane of rows of session, width by height cells, with the
// title and search its border shows: the facet's name and row count, and
// the rows it scrolls to keep the highlight in.
func (m *model) list(session equip.View, rows []equip.Row, width, height int) (string, string, string) {
	facet := session.Facets[m.facet]

	title := m.style.head.Render(facet.Name) + m.style.dim.Render(fmt.Sprintf(" · %d", len(rows)))
	if m.leftmost() == onList {
		title += m.style.dim.Render("  [ ] facet")
	}

	search := m.searchTag(m.query, m.searching, "")

	if len(rows) == 0 {
		none := "  nothing matches"
		if all := m.matching(session, session.Facets[0]); m.facet != 0 && all > 0 {
			none += fmt.Sprintf(", %d in All", all)
		}

		return title, search, m.style.dim.Render(none)
	}

	lines := make([]string, 0, len(rows))
	for index, row := range rows {
		lines = append(lines, m.entry(row, index == m.cur, width))
	}

	lines, m.top = m.window(lines, m.top, m.cur, height)

	return title, search, strings.Join(lines, "\n")
}

// searchTag is the search a pane's border shows: query with a cursor while
// typing, query once typed, and idle with no query.
func (m *model) searchTag(query string, typing bool, idle string) string {
	switch {
	case typing:
		return m.style.cur.Render("/"+query) + m.style.key.Render("▏")
	case query != "":
		return m.style.cur.Render("/" + query)
	}

	return idle
}

// matching counts the rows of session that facet and the search keep.
func (m *model) matching(session equip.View, facet equip.Facet) int {
	if m.query == "" {
		return facet.Count()
	}

	query := strings.ToLower(m.query)

	return len(slices.DeleteFunc(slices.Clone(session.Rows), func(row equip.Row) bool {
		return !facet.Has(row) || !m.matches(row, query)
	}))
}

// entry is the list line of row, width cells: the highlight's mark when
// highlighted, the state glyph, the name, and the cost at the right edge. A
// By-name or manual-only skill's line is muted, as the model does not call it.
func (m *model) entry(row equip.Row, highlighted bool, width int) string {
	cost := m.costCell(row.State, row.ByName, row.CostUnknown, row.Cost)
	style, mark := lipgloss.NewStyle(), m.glyph(row.State)

	switch {
	case highlighted:
		style = m.style.cur
	case row.ByName || row.State == equip.ManualOnly:
		style, mark = m.style.muted, m.style.muted.Render(glyph(row.State))
	}

	base, rest, isPlugin := strings.Cut(row.Name, "@")
	switch {
	case isPlugin:
		rest = "@" + rest
	case row.Follows():
		pluginName, _, _ := strings.Cut(row.Plugin, "@")
		rest = "  " + pluginName
	}

	name := m.name(base, rest, width-rowMarks-lipgloss.Width(cost), style)
	if row.Unsaved {
		name += m.style.warn.Render("*")
	}

	return fit(m.mark(highlighted)+mark+" "+name, width-lipgloss.Width(cost)) + cost
}

// name is the name of a row, base in style then rest dimmed, cut to width
// cells. The rest, a plugin's @marketplace or a plugin's skill's plugin, is
// cut before the base is.
func (m *model) name(base, rest string, width int, style lipgloss.Style) string {
	if rest == "" || lipgloss.Width(base)+2 > width {
		return style.Render(cut(base+rest, width))
	}

	return style.Render(base) + m.style.dim.Render(cut(rest, width-lipgloss.Width(base)))
}

// costCell is the cost of a row or a plugin's content, as the list and the
// contents show it in state: by name for a By-name skill, manual-only for a
// manual-only one no agent lists, ? while unmeasured, else its tokens then a
// bar of their size. The bar takes the last cell, blank with none, so the
// numbers and the bars each line up.
func (m *model) costCell(state equip.State, byName, unknown bool, tokens int) string {
	switch {
	case byName:
		return m.style.muted.Render("by name") + "  "
	case state == equip.ManualOnly && tokens == 0:
		return m.style.muted.Render("manual-only") + "  "
	case unknown && tokens == 0:
		return m.style.unmeasured.Render("?") + "  "
	case tokens == 0:
		return m.style.dim.Render("~0") + "  "
	}

	text := m.style.dim.Render(fmt.Sprintf("~%d", tokens))
	if unknown {
		text += m.style.unmeasured.Render("+?")
	}

	// Two bars a power of ten: 1, 10, 100 and 1,000 tokens start bars 1, 3, 5
	// and 7.
	level := min(int(math.Log10(float64(tokens))*barsPerTen), len(m.style.bars)-1)

	return text + " " + m.style.bars[level].Render(string([]rune(bars)[level]))
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

// matches reports whether the name of row, of the plugin a plugin's skill
// follows, or of one of its MCP servers, contains query, in lower case.
func (m *model) matches(row equip.Row, query string) bool {
	return strings.Contains(strings.ToLower(row.Name), query) ||
		strings.Contains(strings.ToLower(row.Plugin), query) ||
		slices.ContainsFunc(m.s.Detail(row.Key).Contents, func(content equip.Content) bool {
			return content.Kind == equip.MCPServer && strings.Contains(strings.ToLower(content.Name), query)
		})
}

// press acts on key on the main screen.
func (m *model) press(key string) tea.Cmd {
	rows := m.rows(m.s.View())
	servers := m.servers(rows)

	if m.follows(rows, key) {
		return nil
	}

	if delta, ok := step(key); ok {
		m.move(delta, len(servers))

		return nil
	}

	if delta, wraps, ok := pane(key); ok {
		m.moveFocus(delta, wraps)

		return nil
	}

	switch {
	case key == "q":
		return m.quit()
	case stateKey(key):
		if target, ok := m.target(rows, servers); ok {
			// The list keeps the row even when the key takes it out of the
			// facet, so a second press does not act on the next row.
			m.pinned = m.key

			return m.act(key, target)
		}
	case key == "p":
		m.openWorkspace()
	case key == "s":
		m.save()
	}

	return nil
}

// follows acts on key when the highlighted row of rows is a plugin's skill,
// which follows its plugin: enter in the list moves to the plugin, and a key
// that would change the skill says what it follows. It reports whether it
// acted.
func (m *model) follows(rows []equip.Row, key string) bool {
	if m.cur >= len(rows) || !rows[m.cur].Follows() {
		return false
	}

	plugin := rows[m.cur].Plugin

	switch {
	case key == enterKey && m.focus == onList:
		// Pinned, so the list shows the plugin whatever the facet and search;
		// its detail pane starts at its top, as clamp starts a new row's.
		m.key, m.pinned, m.detailTop, m.server = plugin, plugin, 0, 0
	case stateKey(key):
		m.flash = m.style.dim.Render("follows " + plugin + ", enter goes to it")
	default:
		return false
	}

	return true
}

// stateKey reports whether key acts on the state of the highlighted
// extension: sets it, drops its Override, or measures it.
func stateKey(key string) bool {
	return slices.Contains([]string{"1", "2", "3", "x", "m", spaceKey}, key)
}

// save saves the pending changes, and says how many it wrote or why it
// failed.
func (m *model) save() {
	count := m.s.View().Unsaved

	err := m.s.Save()

	switch {
	case err != nil:
		m.flash = m.style.bad.Render("save failed: " + err.Error())
	case count == 0:
		m.flash = m.style.dim.Render("nothing to save")
	default:
		m.flash = m.style.ok.Render(fmt.Sprintf("saved %d %s", count, plural(count, "change", "changes")))
	}
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
// its cost, space sets the state after its own, and 1, 2 and 3 set on,
// manual-only and off, or say the extension has no such state.
func (m *model) act(key, target string) tea.Cmd {
	switch key {
	case "x":
		m.s.DropOverride(target)
	case spaceKey:
		states := m.s.Detail(target).States
		state, _ := m.stateAndKind(target)
		m.s.SetState(target, states[(slices.Index(states, state)+1)%len(states)])
	case "m":
		m.flash = m.style.dim.Render("measuring " + target + "…")

		return m.probe(target, false)
	default:
		state := equip.States()[key[0]-'1']
		if !slices.Contains(m.s.Detail(target).States, state) {
			_, kind := m.stateAndKind(target)
			m.flash = m.style.dim.Render(fmt.Sprintf("%ss have no %s state", kind, state))

			return nil
		}

		m.s.SetState(target, state)
	}

	return nil
}

// stateAndKind are the pending state and the kind of the extension with key:
// the highlighted row, or one of its MCP servers.
func (m *model) stateAndKind(key string) (equip.State, equip.Kind) {
	rows := m.rows(m.s.View())
	for _, content := range m.s.Detail(rows[m.cur].Key).Contents {
		if content.Key == key {
			return content.State, content.Kind
		}
	}

	return rows[m.cur].State, rows[m.cur].Kind
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
