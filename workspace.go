package main

import (
	"cmp"
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
// writes it, tab moves between the panes, and esc goes back to the main
// screen. A key that moves off the preset with unwritten edits asks first.
func (m *model) onPanes(key string, presets []equip.Preset) {
	if delta, isStep := step(key); isStep {
		m.moveInPanes(key, delta, presets)

		return
	}

	switch {
	case key == "tab":
		m.ws.inMembers = !m.ws.inMembers
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
	case err != nil:
		m.flash, m.ws.leave = m.style.warn.Render("write failed: "+err.Error()), ""
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
		m.flash = m.style.warn.Render("delete failed: " + err.Error())
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

// workspaceView is the presets workspace's top line and panes, height lines
// tall: the library, the highlighted preset's members or the add list, and
// the right pane.
func (m *model) workspaceView(height int) string {
	presets := m.s.Presets()

	title := "Library\n" + m.style.dim.Render("  here name           ext proj")
	library, _ := m.window(m.library(presets), 0, m.ws.preset, height-paneHeight-lipgloss.Height(title))
	panes := []string{
		m.box(title+"\n"+strings.Join(library, "\n"), leftWidth, height, !m.ws.inMembers && m.ws.asking == ""),
	}

	if cur, found := m.current(presets); found {
		var candidates []equip.Row

		title, lines, highlighted := m.members(cur)
		if m.ws.adding {
			candidates = m.candidates(cur)
			title, lines, highlighted = m.addList(cur, candidates)
		}

		lines, m.ws.top = m.window(lines, m.ws.top, max(highlighted, 0), height-paneHeight-lipgloss.Height(title))
		middle := (m.width - leftWidth) / sharing
		right := m.width - leftWidth - middle
		panes = append(panes,
			m.box(title+"\n"+strings.Join(lines, "\n"), middle, height, m.ws.inMembers && m.ws.asking == ""),
			m.box(m.right(cur, presets, candidates, right-paneWidth, height-paneHeight), right, height,
				m.ws.asking != ""))
	}

	return fit(m.workspaceTop(), m.width) + "\n" + lipgloss.JoinHorizontal(lipgloss.Top, panes...)
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
		return m.help("↑↓", "move", "space", "add", "/", "search", "esc", "close")
	case m.ws.inMembers:
		return m.help("↑↓", "member", "space", "remove or add back", "a", "add", "w", "write preset", "tab", "library",
			"esc", "back")
	}

	return m.help("↑↓", "preset", "space", "active here", "n", "new", "r", "rename", "d", "delete", "a", "add members",
		"w", "write preset", "tab", "members", "esc", "back")
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

	turnedOn, turnedOff := turned(now.TurnedSince(m.ws.before))
	changed := turnedOn+turnedOff > 0
	top := []string{m.style.top.Render("equip presets"), "active here: " + active}

	for _, agent := range equip.Agents() {
		total := totalOf(now.Totals[agent])
		if changed {
			total = totalOf(m.ws.before.Totals[agent]) + " → " + total
		}

		top = append(top, agent.String()+" "+total)
	}

	if changed {
		top = append(top, m.style.warn.Render(fmt.Sprintf("%d on, %d off, unsaved, s on main", turnedOn, turnedOff)))
	}

	return strings.Join(top, "  ")
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
		check, name := "[ ]", preset.Name
		if preset.Active {
			check = "[x]"
		}

		if preset.Unwritten {
			name += "*"
		}

		members := len(slices.DeleteFunc(slices.Clone(preset.Members), func(m equip.Member) bool { return m.Removed }))
		line := fmt.Sprintf("%s %-15s %3d %4d", check, name, members, len(preset.Projects))

		// A missing preset's line says so in place of its counts.
		if preset.Missing {
			line = m.style.dim.Render(fmt.Sprintf("%s %-15s %8s", check, name, "missing"))
		}

		lines = append(lines, m.mark(index == m.ws.preset)+line)
	}

	return lines
}

// members is the members pane of preset: its title, and its members grouped
// by kind, each with its state here, cost and Override mark, with the index
// of the highlighted one's line; -1 with none. + marks an unwritten addition
// and - an unwritten removal, struck through. A member not installed here
// shows greyed.
func (m *model) members(preset equip.Preset) (string, []string, int) {
	var lines []string
	if len(preset.Members) == 0 {
		lines = append(lines, m.style.dim.Render("  no members yet, a adds some"))
	}

	highlighted := -1

	for index, member := range preset.Members {
		if index == 0 || member.Kind != preset.Members[index-1].Kind {
			lines = append(lines, "", m.style.dim.Render(member.Kind.String()))
		}

		on := m.ws.inMembers && index == m.ws.member
		if on {
			highlighted = len(lines)
		}

		lines = append(lines, m.mark(on)+m.memberLine(member))
	}

	return "Members of " + preset.Name, lines, highlighted
}

// memberLine is the line of member in the members pane, after its mark.
func (m *model) memberLine(member equip.Member) string {
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

	ovr := ""
	if member.Override {
		ovr = m.style.warn.Render(" ovr")
	}

	return fmt.Sprintf("%s %s %s %s%s", m.glyph(member.State), edit, name,
		m.style.dim.Render(costOf(member.CostUnknown, member.Cost)), ovr)
}

// addList is the add list of preset: its title and search, and its
// candidates, the extensions that are not members, grouped by kind, with the
// index of the highlighted one's line; -1 with none.
func (m *model) addList(preset equip.Preset, candidates []equip.Row) (string, []string, int) {
	search := m.style.dim.Render("/ searches")
	if m.ws.searching || m.ws.query != "" {
		search = m.style.cur.Render("/" + m.ws.query)
	}

	var lines []string

	highlighted := -1

	for index, row := range candidates {
		if index == 0 || row.Kind != candidates[index-1].Kind {
			lines = append(lines, "", m.style.dim.Render(row.Kind.String()))
		}

		if index == m.ws.member {
			highlighted = len(lines)
		}

		lines = append(lines, m.mark(index == m.ws.member)+m.glyph(row.State)+" "+row.Name+" "+
			m.style.dim.Render(costOf(row.CostUnknown, row.Cost)))
	}

	return "Add to " + preset.Name + "\n" + search, lines, highlighted
}

// right is the right pane, width by height cells: the question asked, else
// the detail of the highlighted extension in the members pane or among
// candidates, the add list, else the projects that use preset.
func (m *model) right(preset equip.Preset, presets []equip.Preset, candidates []equip.Row, width, height int) string {
	switch m.ws.asking {
	case askWrite, askDelete:
		return m.confirm(preset)
	case askActivate:
		return strings.Join([]string{
			"Wrote preset " + preset.Name, "", m.style.warn.Render("Make it active here? y/n"),
			m.style.dim.Render("a pending change, saved with s on the main screen"),
		}, "\n")
	case askLeave:
		return m.style.warn.Render(preset.Name+" has unwritten edits") + "\n\nw write  d discard  esc stay"
	}

	if key, name, ok := m.highlightedExtension(preset, candidates); ok {
		return m.extension(key, name, presets, width, height)
	}

	here := m.s.View().Project.Path
	lines := []string{fmt.Sprintf("Used by %d projects", len(preset.Projects))}

	for _, path := range preset.Projects {
		if path == here {
			path += m.style.dim.Render(" (here)")
		}

		lines = append(lines, "  "+path)
	}

	return strings.Join(lines, "\n")
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

// extension is the right pane of the extension with key and name: its
// detail, as the main screen shows it, and the presets that contain it, with
// the active ones marked, width by height cells.
func (m *model) extension(key, name string, presets []equip.Preset, width, height int) string {
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
		return m.detail(view, view.Rows[i], m.s.Detail(key), "", width, height-presetsLines) + "\n\n" + line
	}

	return name + m.style.dim.Render("  not installed") + "\n\n" + line
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
	lines = append(lines, "", "This project")

	if len(changes) == 0 {
		lines = append(lines, m.style.dim.Render("  no extension changes state here"))
	}

	for _, row := range changes {
		was := before.Rows[index(before.Rows, row.Key)].State
		lines = append(lines, "  "+glyph(was)+" → "+glyph(row.State)+" "+row.Name)
	}

	lines = append(lines, "")

	for _, agent := range equip.Agents() {
		lines = append(lines, agent.String()+" "+totalOf(before.Totals[agent])+" → "+totalOf(after.Totals[agent]))
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

	lines := []string{"Other projects"}

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
