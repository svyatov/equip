package main

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/svyatov/equip/internal/equip"
)

// The questions the workspace's right pane asks.
const (
	askWrite    = "write"    // write the preset with unwritten edits
	askActivate = "activate" // make the preset the write created active here
	askLeave    = "leave"    // write or discard the unwritten edits before moving off
	askDelete   = "delete"   // delete the highlighted preset
)

// What the name typed is for.
const (
	nameNew    = "new"    // a new preset
	nameRename = "rename" // the highlighted preset
)

// workspace is the state of the presets workspace.
type workspace struct {
	name   string // typed while naming
	naming string // what the name typed is for, nameNew or nameRename; empty when not naming
	asking string // the question the right pane asks; empty with none
	// leave is the key that moved off the preset with unwritten edits, done
	// once they are written or discarded.
	leave     string
	created   string        // the id of the preset the last write created
	query     string        // the add list's search
	before    equip.View    // at the workspace's opening
	preview   equip.Preview // of the write or delete the right pane asks for
	preset    int           // the highlighted preset
	member    int           // the highlighted member, or extension in the add list
	top       int           // the first line the members pane or the add list shows
	open      bool
	inMembers bool // the keys act on the members pane
	adding    bool // the members pane lists the extensions to add
	searching bool // the keys type into the add list's search
}

// newWorkspace is a closed workspace that compares with before.
func newWorkspace(before equip.View) workspace {
	return workspace{
		before: before, open: false, name: "", naming: "", asking: "", leave: "", created: "", query: "", preset: 0,
		member: 0, top: 0, inMembers: false, adding: false, searching: false,
		preview: equip.Preview{Before: before, After: before, Others: nil},
	}
}

// openWorkspace opens the presets workspace, which compares what the keys
// change there with the view at opening.
func (m *model) openWorkspace() {
	m.ws = newWorkspace(m.s.View())
	m.ws.open = true
}

// inWorkspace acts on keyMsg in the presets workspace.
func (m *model) inWorkspace(keyMsg tea.KeyPressMsg) {
	presets := m.s.Presets()

	switch {
	case m.ws.asking != "":
		m.answer(keyMsg.String(), presets)
	case m.ws.naming != "":
		m.typeName(keyMsg, presets)
	case m.ws.adding:
		m.inAddList(keyMsg, presets)
	default:
		m.onPanes(keyMsg.String(), presets)
	}

	presets = m.s.Presets()
	m.ws.preset = max(min(m.ws.preset, len(presets)-1), 0)

	listed := 0
	if cur, ok := m.current(presets); ok && m.ws.adding {
		listed = len(m.candidates(cur))
	} else if ok {
		listed = len(cur.Members)
	}

	m.ws.member = max(min(m.ws.member, listed-1), 0)
}

// onPanes acts on key in the library or the members pane: the arrows move,
// space makes the highlighted preset active here or adds or removes the
// highlighted member, n creates a preset, r renames it, a adds members, w
// writes it, h, l, tab and the arrows move between the panes, and esc goes
// back to the main screen. A key that moves off the preset with unwritten
// edits asks first.
func (m *model) onPanes(key string, presets []equip.Preset) {
	if delta, isStep := step(key); isStep {
		m.moveInPanes(key, delta, presets)

		return
	}

	if delta, wraps, isPane := pane(key); isPane {
		m.movePane(delta, wraps)

		return
	}

	switch {
	case key == escKey && !m.guard(key):
		m.ws.open = false
	case key == "n" && !m.guard(key):
		m.ws.naming, m.ws.name = nameNew, ""
	default:
		if cur, found := m.current(presets); found {
			m.onPreset(key, cur)
		}
	}
}

// movePane moves the keys by delta between the library and the members
// pane. Of two panes, a move that wraps lands on the other one.
func (m *model) movePane(delta int, wraps bool) {
	m.ws.inMembers = wraps && !m.ws.inMembers || !wraps && delta > 0
}

// moveInPanes moves the highlight by delta, as the arrow key does: among
// the members, else among presets.
func (m *model) moveInPanes(key string, delta int, presets []equip.Preset) {
	next := max(min(m.ws.preset+delta, len(presets)-1), 0)

	switch {
	case m.ws.inMembers:
		m.ws.member += delta
	case next != m.ws.preset && !m.guard(key):
		m.ws.preset, m.ws.member = next, 0
	}
}

// onPreset acts on key on the highlighted preset cur. A missing preset can
// only be made active here or no longer active.
func (m *model) onPreset(key string, cur equip.Preset) {
	if cur.Missing && key != spaceKey {
		m.flash = m.style.warn.Render("preset " + cur.Name + " missing: sync its file and reopen equip")

		return
	}

	switch key {
	case "r":
		m.ws.naming, m.ws.name = nameRename, cur.Name
	case "a":
		m.ws.adding, m.ws.inMembers, m.ws.searching, m.ws.query, m.ws.member = true, true, false, "", 0
	case "d":
		m.confirmDelete(cur)
	case "w":
		m.ws.leave = ""
		if cur.Unwritten {
			m.confirmWrite()
		} else {
			m.flash = m.style.dim.Render("no unwritten edits in " + cur.Name)
		}
	case spaceKey:
		if m.ws.inMembers {
			m.toggleMember(cur)
		} else {
			m.toggleActive(cur)
		}
	}
}

// confirmDelete asks to delete preset. A preset not written yet goes at once,
// as nothing uses it.
func (m *model) confirmDelete(preset equip.Preset) {
	if preset.New {
		m.s.DiscardPreset()
	} else {
		m.ws.asking = askDelete
		m.ws.preview = m.s.PreviewDelete(preset.ID)
	}
}

// confirmWrite asks to write the preset with unwritten edits. It previews the
// write once here, as the preview opens each other Project that uses it.
func (m *model) confirmWrite() {
	m.ws.asking = askWrite
	m.ws.preview = m.s.PreviewWrite()
}

// guard reports whether a preset has unwritten edits, and then asks to
// write or discard them before key moves off it.
func (m *model) guard(key string) bool {
	if !m.s.Unwritten() {
		return false
	}

	m.ws.asking, m.ws.leave = askLeave, key

	return true
}

// toggleMember removes the highlighted member of preset, or adds it back
// when an unwritten edit removed it.
func (m *model) toggleMember(preset equip.Preset) {
	if m.ws.member >= len(preset.Members) {
		return
	}

	member, change := preset.Members[m.ws.member], m.s.RemoveMember
	if member.Removed {
		change = m.s.AddMember
	}

	err := change(preset.ID, member.Key)
	if err != nil {
		m.flash = m.style.warn.Render(err.Error())
	}
}

// toggleActive makes preset active here or no longer active. A preset not
// written yet cannot be.
func (m *model) toggleActive(preset equip.Preset) {
	if preset.New {
		m.flash = m.style.warn.Render("write the new preset first (w), then make it active here")

		return
	}

	m.s.TogglePreset(preset.ID)
}

// typeName acts on keyMsg while the keys type a preset's name: enter creates
// or renames the preset, and esc cancels.
func (m *model) typeName(keyMsg tea.KeyPressMsg, presets []equip.Preset) {
	switch keyMsg.String() {
	case enterKey:
		m.giveName(strings.TrimSpace(m.ws.name), presets)
	case escKey:
		m.ws.naming = ""
	default:
		m.ws.name = typeInto(m.ws.name, keyMsg)
	}
}

// giveName gives name to a new preset, or to the highlighted one of presets,
// and highlights it.
func (m *model) giveName(name string, presets []equip.Preset) {
	var (
		presetID string
		err      error
	)

	if cur, found := m.current(presets); m.ws.naming == nameRename && found {
		presetID, err = cur.ID, m.s.RenamePreset(cur.ID, name)
	} else {
		presetID, err = m.s.CreatePreset(name)
	}

	if err != nil {
		m.flash = m.style.warn.Render(err.Error())

		return
	}

	m.ws.naming = ""
	m.ws.preset = slices.IndexFunc(m.s.Presets(), func(p equip.Preset) bool { return p.ID == presetID })
}

// typeInto is text as keyMsg edits it: backspace deletes the last character,
// and a key that types text adds it.
func typeInto(text string, keyMsg tea.KeyPressMsg) string {
	if keyMsg.String() == "backspace" {
		runes := []rune(text)

		return string(runes[:max(len(runes)-1, 0)])
	}

	return text + keyMsg.Text
}

// inAddList acts on keyMsg in the add list: the arrows move, space adds the
// highlighted extension, / starts the search, and esc closes the list.
func (m *model) inAddList(keyMsg tea.KeyPressMsg, presets []equip.Preset) {
	key := keyMsg.String()
	delta, isStep := step(key)

	switch {
	case m.ws.searching:
		m.typeSearch(keyMsg)
	case isStep:
		m.ws.member += delta
	case key == escKey:
		m.ws.adding, m.ws.member = false, 0
	case key == "/":
		m.ws.searching = true
	case key == spaceKey:
		cur, _ := m.current(presets)
		if candidates := m.candidates(cur); m.ws.member < len(candidates) {
			err := m.s.AddMember(cur.ID, candidates[m.ws.member].Key)
			if err != nil {
				m.flash = m.style.warn.Render(err.Error())
			}
		}
	}
}

// typeSearch acts on keyMsg while the keys type into the add list's search:
// enter ends it, and esc clears it too.
func (m *model) typeSearch(keyMsg tea.KeyPressMsg) {
	switch keyMsg.String() {
	case enterKey:
		m.ws.searching = false
	case escKey:
		m.ws.searching, m.ws.query = false, ""
	default:
		m.ws.query, m.ws.member = typeInto(m.ws.query, keyMsg), 0
	}
}

// answer acts on key as the answer to the question the right pane asks. Once
// the unwritten edits are written or discarded, the key that moved off them
// moves on.
func (m *model) answer(key string, presets []equip.Preset) {
	asked := m.ws.asking
	m.ws.asking = ""

	switch asked {
	case askWrite:
		if key == "y" {
			m.write(presets)

			return
		}

		m.ws.leave = ""
	case askActivate:
		if key == "y" {
			m.s.TogglePreset(m.ws.created)
		}
	case askDelete:
		if key == "y" {
			m.deletePreset(presets)
		}
	case askLeave:
		m.answerLeave(key)

		return
	}

	m.moveOn()
}

// answerLeave acts on key as the answer to write or discard the unwritten
// edits before moving off them.
func (m *model) answerLeave(key string) {
	switch key {
	case "w":
		m.confirmWrite()

		return
	case "d":
		m.s.DiscardPreset()
	default:
		m.ws.leave = ""
	}

	m.moveOn()
}

// moveOn does the key that moved off the unwritten edits, if one did.
func (m *model) moveOn() {
	key := m.ws.leave
	m.ws.leave = ""

	if key != "" {
		m.onPanes(key, m.s.Presets())
	}
}

// write writes the preset with unwritten edits, the highlighted one of
// presets, and offers to make it active here when the write created it. The
// workspace then compares with the view after the write, which saved what
// it changed here.
func (m *model) write(presets []equip.Preset) {
	cur, _ := m.current(presets)

	err := m.s.WritePreset()

	switch {
	case errors.Is(err, equip.ErrPresetDeleted):
		m.flash, m.ws.leave = m.style.warn.Render("preset file deleted outside equip since open: w writes it again"), ""
	case errors.Is(err, equip.ErrPresetChanged):
		m.flash, m.ws.leave = m.style.warn.Render(err.Error()+": merged into the edits, w writes them"), ""
	case errors.Is(err, equip.ErrChangedSinceOpen):
		m.flash, m.ws.leave = m.style.bad.Render("changed outside equip: press w to write, then esc and s to save"), ""
	case err != nil:
		m.flash, m.ws.leave = m.style.bad.Render("write failed: "+err.Error()), ""
	case cur.New:
		m.ws.before, m.ws.asking, m.ws.created = m.s.View(), askActivate, cur.ID
	default:
		m.ws.before = m.s.View()
		m.moveOn()
	}
}

// deletePreset deletes the highlighted preset of presets. The workspace then
// compares with the view after the delete, which saved what it changed here.
func (m *model) deletePreset(presets []equip.Preset) {
	cur, _ := m.current(presets)

	err := m.s.DeletePreset(cur.ID)
	if err != nil {
		m.flash = m.style.bad.Render("delete failed: " + err.Error())
	}

	m.ws.before = m.s.View()
}

// current is the highlighted preset of presets, reporting whether there is one.
func (m *model) current(presets []equip.Preset) (equip.Preset, bool) {
	var none equip.Preset
	if m.ws.preset < 0 || m.ws.preset >= len(presets) {
		return none, false
	}

	return presets[m.ws.preset], true
}

// candidates are the rows the add list offers for preset: every extension
// that is not a member, by kind, that the search keeps.
func (m *model) candidates(preset equip.Preset) []equip.Row {
	query := strings.ToLower(m.ws.query)
	rows := slices.DeleteFunc(m.s.View().Rows, func(row equip.Row) bool {
		return !strings.Contains(strings.ToLower(row.Name), query) ||
			slices.ContainsFunc(preset.Members, func(member equip.Member) bool {
				return member.Key == row.Key && !member.Removed
			})
	})
	slices.SortStableFunc(rows, func(a, b equip.Row) int { return cmp.Compare(a.Kind, b.Kind) })

	return rows
}

const (
	// sharing is the count of the panes right of the library, the members
	// pane and the right pane, which share its width.
	sharing = 2
	// presetsLines are the lines under the extension's detail in the right
	// pane: a blank line and its presets.
	presetsLines = 2
)

// libraryHead is the library's column heads, over its rows: the name, the
// count of members and the count of projects that use it.
const libraryHead = "    preset          ext used"

// workspaceView is the presets workspace's status and panes, height lines
// tall: the library, the highlighted preset's members or the add list, and
// the right pane. With no preset, the middle pane says what a preset is.
func (m *model) workspaceView(height int) string {
	presets := m.s.Presets()
	top := m.workspaceTop()
	height -= lipgloss.Height(top)
	// As wide as the main screen's list, so a member's cost stays near its name.
	middle := min((m.width-leftWidth)/sharing, maxList)
	right := m.width - leftWidth - middle

	head := ""
	if len(presets) > 0 {
		head = m.style.dim.Render(libraryHead) + "\n"
	}

	library, _ := m.window(m.library(presets), 0, m.ws.preset, height-paneHeight-lipgloss.Height(head)+1)
	panes := []string{
		m.box("Library", "", head+strings.Join(library, "\n"), leftWidth, height, !m.ws.inMembers && m.ws.asking == ""),
	}

	cur, found := m.current(presets)
	if !found {
		intro := m.style.dim.Width(middle - paneWidth).Render("A preset is a named set of extensions for one kind " +
			"of project, such as Ruby or Writing.\n\nMake presets active in a project, and the extensions they " +
			"name turn on there while the rest turn off. A project can have several active at once.")

		return block(top, m.width, lipgloss.Height(top)) + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, append(panes,
			m.box("Presets", "", intro+"\n\n"+m.style.key.Render("n")+" creates one", middle, height, false),
			m.box("", "", "", right, height, false))...)
	}

	var candidates []equip.Row

	title, search, lines, highlighted := m.members(cur, middle-paneWidth)
	if m.ws.adding {
		candidates = m.candidates(cur)
		title, search, lines, highlighted = m.addList(cur, candidates, middle-paneWidth)
	}

	lines, m.ws.top = m.window(lines, m.ws.top, max(highlighted, 0), height-paneHeight)
	rightTitle, rightText := m.right(cur, presets, candidates, right-paneWidth, height-paneHeight)
	panes = append(panes,
		m.box(title, search, strings.Join(lines, "\n"), middle, height, m.ws.inMembers && m.ws.asking == ""),
		m.box(rightTitle, "", rightText, right, height, m.ws.asking != ""))

	return block(top, m.width, lipgloss.Height(top)) + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, panes...)
}

// workspaceFooter is the workspace's bottom line: the quit guard, the flash,
// the name typed, or the keys.
func (m *model) workspaceFooter() string {
	switch {
	case m.quitting || m.flash != "":
		return m.footer()
	case m.ws.naming != "":
		return "name: " + m.ws.name + m.style.cur.Render("▏") + "  " + m.help("enter", "done", "esc", "cancel")
	case m.ws.asking == askLeave:
		return m.help("w", "write", "d", "discard", "esc", "stay")
	case m.ws.asking != "":
		return m.help("y", "yes", "n", "no")
	case m.ws.searching:
		return m.help("type", "to search", "enter", "done", "esc", "clear")
	case m.ws.adding:
		return m.help("j/k", "move", "space", "add", "/", "search", "esc", "close")
	case m.ws.inMembers:
		return m.help("j/k", "member", "space", "remove or add back", "a", "add", "w", "write preset", "h", "library",
			"?", "keys", "esc", "back")
	}

	return m.help("j/k", "preset", "space", "active here", "n", "new", "r", "rename", "d", "delete", "a", "add members",
		"w", "write preset", "l", "members", "?", "keys", "esc", "back")
}

// workspaceTop is the workspace's top line: the active presets, and each
// agent's session total with how many extensions turn on and off since the
// workspace opened.
func (m *model) workspaceTop() string {
	now := m.s.View()

	active := m.style.dim.Render("none (agent defaults)")
	if len(now.Presets) > 0 {
		active = m.style.cur.Render(strings.Join(now.Presets, " + "))
	}

	turnedOn, turnedOff := turned(now.TurnedSince(m.ws.before))
	changed := turnedOn+turnedOff > 0
	top := []string{m.style.head.Render("presets") + m.style.dim.Render("  active here: ") + active}

	for _, agent := range equip.Agents() {
		total := agentTotal(agent, now.Totals[agent], m.style)
		if changed {
			total = agentTotal(agent, m.ws.before.Totals[agent], m.style) + " → " + totalOf(now.Totals[agent], m.style)
		}

		top = append(top, total)
	}

	if changed {
		top = append(top, m.style.warn.Render(fmt.Sprintf("%d on, %d off, unsaved, s on main", turnedOn, turnedOff)))
	}

	return m.status("", top...)
}

// turned counts the rows that turned on and off.
func turned(rows []equip.Row) (int, int) {
	counts := map[equip.State]int{}
	for _, row := range rows {
		counts[row.State]++
	}

	return counts[equip.On], counts[equip.Off]
}

// library is the library pane's lines under its title: each preset with
// whether it is active here, its member count and its project count. A *
// marks unwritten edits.
func (m *model) library(presets []equip.Preset) []string {
	var lines []string
	if len(presets) == 0 {
		lines = append(lines, m.style.dim.Render("  no presets, n creates one"))
	}

	for index, preset := range presets {
		check, name := m.style.states[equip.Off].Render(glyph(equip.Off)), preset.Name
		if preset.Active {
			check = m.glyph(equip.On)
		}

		if preset.Unwritten {
			name += "*"
		}

		members := len(slices.DeleteFunc(slices.Clone(preset.Members), func(m equip.Member) bool { return m.Removed }))
		line := check + fmt.Sprintf(" %-15s %3d %4d", name, members, len(preset.Projects))

		// A missing preset's line says so in place of its counts.
		if preset.Missing {
			line = check + m.style.dim.Render(fmt.Sprintf(" %-15s %8s", name, "missing"))
		}

		lines = append(lines, m.mark(index == m.ws.preset, false)+line)
	}

	return lines
}

// members is the members pane of preset, width cells: its title and no
// search, and its members grouped by kind, each with its state here, cost
// and Override mark, with the index of the highlighted one's line; -1 with
// none. + marks an unwritten addition and - an unwritten removal, struck
// through. A member not installed here shows greyed.
func (m *model) members(preset equip.Preset, width int) (string, string, []string, int) {
	var lines []string
	if len(preset.Members) == 0 {
		lines = append(lines, m.style.dim.Render("  no members yet, a adds some"))
	}

	highlighted := -1

	for index, member := range preset.Members {
		if index == 0 || member.Kind != preset.Members[index-1].Kind {
			lines = append(lines, "", m.style.kinds[member.Kind].Render(member.Kind.String()))
		}

		on := m.ws.inMembers && index == m.ws.member
		if on {
			highlighted = len(lines)
		}

		lines = append(lines, m.mark(on, false)+m.memberLine(member, width-markWidth))
	}

	return m.style.head.Render("Members of " + preset.Name), "", lines, highlighted
}

// memberLine is the line of member in the members pane, after its mark,
// width cells, with its cost at the right edge.
func (m *model) memberLine(member equip.Member, width int) string {
	edit, name := " ", member.Name

	switch {
	case member.Added:
		edit = "+"
	case member.Removed:
		edit, name = "-", lipgloss.NewStyle().Strikethrough(true).Render(name)
	}

	if !member.Installed {
		return m.style.dim.Render("  " + edit + " " + name + "  not installed")
	}

	if member.Override {
		name += m.style.warn.Render(" ovr")
	}

	cost := m.costCell(member.State, member.ByName, member.CostUnknown, member.Cost)

	return fit(m.glyph(member.State)+" "+edit+" "+name, width-lipgloss.Width(cost)) + cost
}

// addList is the add list of preset, width cells: its title and search, and
// its candidates, the extensions that are not members, grouped by kind, with
// the index of the highlighted one's line; -1 with none.
func (m *model) addList(preset equip.Preset, candidates []equip.Row, width int) (string, string, []string, int) {
	search := m.searchTag(m.ws.query, m.ws.searching, m.style.dim.Render("/ searches"), width)

	lines := []string{m.style.dim.Render("  nothing matches")}
	if len(candidates) > 0 {
		lines = nil
	}

	highlighted := -1

	for index, row := range candidates {
		if index == 0 || row.Kind != candidates[index-1].Kind {
			lines = append(lines, "", m.style.kinds[row.Kind].Render(row.Kind.String()))
		}

		if index == m.ws.member {
			highlighted = len(lines)
		}

		lines = append(lines, m.entry(row, index == m.ws.member, width))
	}

	return m.style.head.Render("Add to " + preset.Name), search, lines, highlighted
}

// right is the right pane, width by height cells, and the title its border
// shows: the question asked, else the detail of the highlighted extension in
// the members pane or among candidates, the add list, else the projects that
// use preset.
func (m *model) right(preset equip.Preset, presets []equip.Preset, candidates []equip.Row,
	width, height int,
) (string, string) {
	switch m.ws.asking {
	case askWrite, askDelete:
		return "", lipgloss.NewStyle().Width(width).Render(m.confirm(preset))
	case askActivate:
		return "", lipgloss.NewStyle().Width(width).Render(strings.Join([]string{
			"Wrote preset " + preset.Name, "", m.style.warn.Render("Make it active here? y/n"),
			m.style.dim.Render("a pending change, saved with s on the main screen"),
		}, "\n"))
	case askLeave:
		return "", m.style.warn.Render(preset.Name+" has unwritten edits") + "\n\nw write  d discard  esc stay"
	}

	if key, name, ok := m.highlightedExtension(preset, candidates); ok {
		return m.extension(key, name, presets, width, height)
	}

	here := m.s.View().Project.Path
	lines := []string{m.style.head.Render("Used by " + count(len(preset.Projects), "project", "projects"))}

	for _, path := range preset.Projects {
		if path == here {
			path += m.style.dim.Render(" (here)")
		}

		lines = append(lines, "  "+path)
	}

	return "", strings.Join(lines, "\n")
}

// highlightedExtension is the key and name of the extension highlighted in
// candidates, the add list, or the members pane of preset, reporting whether
// there is one.
func (m *model) highlightedExtension(preset equip.Preset, candidates []equip.Row) (string, string, bool) {
	if m.ws.adding {
		if m.ws.member < len(candidates) {
			return candidates[m.ws.member].Key, candidates[m.ws.member].Name, true
		}

		return "", "", false
	}

	if m.ws.inMembers && m.ws.member < len(preset.Members) {
		return preset.Members[m.ws.member].Key, preset.Members[m.ws.member].Name, true
	}

	return "", "", false
}

// extension is the right pane of the extension with key and name, and its
// title: its detail, as the main screen shows it, and the presets that
// contain it, with the active ones marked, width by height cells.
func (m *model) extension(key, name string, presets []equip.Preset, width, height int) (string, string) {
	var containing []string

	for _, preset := range presets {
		if slices.ContainsFunc(preset.Members, func(member equip.Member) bool {
			return member.Key == key && !member.Removed
		}) {
			label := preset.Name
			if preset.Active {
				label += " (active)"
			}

			containing = append(containing, label)
		}
	}

	line := "Presets  " + cmp.Or(strings.Join(containing, ", "), m.style.dim.Render("none"))

	view := m.s.View()
	if i := index(view.Rows, key); i >= 0 {
		return m.detailTitle(view.Rows[i]),
			m.detail(view, view.Rows[i], m.s.Detail(key), -1, width, height-presetsLines) + "\n\n" + line
	}

	return m.style.cur.Render(name), m.style.dim.Render("not installed") + "\n\n" + line
}

// confirm is the right pane that asks to write or delete preset: what that
// turns on and off in each other Project that uses it, or why it skips one,
// the extensions whose saved state it changes here, and each agent's session
// total before and after.
func (m *model) confirm(preset equip.Preset) string {
	verb := "Write"

	switch {
	case m.ws.asking == askDelete:
		verb = "Delete"
	case preset.New:
		verb = "Create"
	}

	before, after := m.ws.preview.Before, m.ws.preview.After
	changes := after.TurnedSince(before)
	lines := []string{m.style.warn.Render(verb + " preset " + preset.Name + "?"), ""}
	lines = append(lines, m.others(m.ws.preview.Others)...)
	lines = append(lines, "", m.style.head.Render("This project"))

	if len(changes) == 0 {
		lines = append(lines, m.style.dim.Render("  no extension changes state here"))
	}

	for _, row := range changes {
		was := before.Rows[index(before.Rows, row.Key)].State
		lines = append(lines, "  "+glyph(was)+" → "+glyph(row.State)+" "+row.Name)
	}

	lines = append(lines, "")

	for _, agent := range equip.Agents() {
		lines = append(lines, agentTotal(agent, before.Totals[agent], m.style)+" → "+totalOf(after.Totals[agent], m.style))
	}

	lines = append(lines, "", m.style.dim.Render("pending changes here stay pending"), "",
		"y "+strings.ToLower(verb)+"  n cancel")

	return strings.Join(lines, "\n")
}

// others are the confirm's lines of the other Projects that use a preset:
// the extensions that turn on (+) and off (-) in each, or why it is skipped.
func (m *model) others(others []equip.Affected) []string {
	if len(others) == 0 {
		return []string{m.style.dim.Render("no other project uses it")}
	}

	lines := []string{m.style.head.Render("Other projects")}

	for _, project := range others {
		if project.Skipped != "" {
			lines = append(lines, "  "+project.Path+m.style.warn.Render("  skipped: "+project.Skipped))

			continue
		}

		lines = append(lines, "  "+project.Path)

		var turnedOn, turnedOff []string

		for _, row := range project.Turned {
			if row.State == equip.On {
				turnedOn = append(turnedOn, row.Name)
			} else {
				turnedOff = append(turnedOff, row.Name)
			}
		}

		if len(turnedOn) > 0 {
			lines = append(lines, "    + "+strings.Join(turnedOn, ", "))
		}

		if len(turnedOff) > 0 {
			lines = append(lines, "    - "+strings.Join(turnedOff, ", "))
		}
	}

	return lines
}
