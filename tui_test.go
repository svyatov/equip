package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/svyatov/equip/internal/equip"
	"github.com/svyatov/equip/internal/equiptest"
)

func newModel(t *testing.T, machine *equiptest.Machine) *model {
	t.Helper()

	return newModelIn(t, machine, machine.Root)
}

// resize tells tui the terminal is width by height cells.
func resize(tui *model, width, height int) {
	tui.Update(tea.WindowSizeMsg{Width: width, Height: height})
}

// withSkills is a machine with count Claude Code skills, s00 on.
func withSkills(t *testing.T, count int) *equiptest.Machine {
	t.Helper()

	machine := equiptest.New(t)
	for i := range count {
		machine.Skill(machine.ClaudeSkills(), fmt.Sprintf("s%02d", i))
	}

	return machine
}

// fits reports why the view of tui does not fill width by height cells
// exactly, or "" when it does.
func fits(tui *model, width, height int) string {
	lines := strings.Split(tui.View().Content, "\n")
	if len(lines) != height {
		return fmt.Sprintf("%d lines, want %d", len(lines), height)
	}

	for i, text := range lines {
		if got := lipgloss.Width(text); got > width {
			return fmt.Sprintf("line %d is %d cells, want at most %d", i, got, width)
		}
	}

	return ""
}

func TestViewFitsTheTerminalWithTheKeysOnTheLastLine(t *testing.T) {
	t.Parallel()
	tui := newModel(t, withSkills(t, 40))
	resize(tui, 80, 20)

	if why := fits(tui, 80, 20); why != "" {
		t.Errorf("view does not fit 80x20: %s\n%s", why, tui.View().Content)
	}

	lines := strings.Split(styleCodes.ReplaceAllString(tui.View().Content, ""), "\n")
	if !strings.Contains(lines[len(lines)-1], "q  quit") {
		t.Errorf("last line %q does not show q quit", lines[len(lines)-1])
	}
}

func TestListScrollsToKeepTheHighlightOnScreen(t *testing.T) {
	t.Parallel()
	tui := newModel(t, withSkills(t, 40))
	resize(tui, 80, 20)

	for range 39 {
		press(tui, down())
	}

	if got := line(tui, "▸ ● s39"); got == "" {
		t.Errorf("highlighted s39 is off screen:\n%s", tui.View().Content)
	}

	for range 39 {
		press(tui, tea.KeyPressMsg{Code: tea.KeyUp})
	}

	if got := line(tui, "▸ ● s00"); got == "" {
		t.Errorf("highlighted s00 is off screen:\n%s", tui.View().Content)
	}
}

func TestNarrowTerminalDropsTheSidebarAndNamesTheFacetKeys(t *testing.T) {
	t.Parallel()
	tui := newModel(t, withSkills(t, 3))
	resize(tui, 90, 20)

	if line(tui, "Unsaved changes") != "" {
		t.Errorf("sidebar still shows:\n%s", tui.View().Content)
	}

	if !strings.Contains(line(tui, "All · 3"), "[ ] facet") {
		t.Errorf("list title does not name the facet keys:\n%s", tui.View().Content)
	}
}

func TestDetailPaneScrollsToKeepTheHighlightedMCPServerOnScreen(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")

	servers := make([]string, 0, 20)
	for i := range 20 {
		servers = append(servers, fmt.Sprintf(`"m%02d": {"command": "m"}`, i))
	}

	machine.WriteFile(filepath.Join(dir, ".mcp.json"), `{"mcpServers": {`+strings.Join(servers, ", ")+`}}`)
	tui := newModel(t, machine)
	resize(tui, 120, 20)

	press(tui, tea.KeyPressMsg{Code: tea.KeyTab})

	for range 19 {
		press(tui, down())
	}

	if got := line(tui, "MCP server m19"); !strings.Contains(got, "▸") {
		t.Errorf("highlighted m19 is off screen:\n%s", tui.View().Content)
	}

	if why := fits(tui, 120, 20); why != "" {
		t.Errorf("view does not fit 120x20: %s", why)
	}
}

func ctrl(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }

func TestHAndLMoveTheKeysBetweenThePanes(t *testing.T) {
	t.Parallel()
	tui := newModel(t, withSkills(t, 3))

	for _, step := range []struct {
		key  tea.KeyPressMsg
		want int
	}{
		{key('l'), onDetail},
		{key('l'), onDetail},
		{key('h'), onList},
		{key('h'), onFacets},
		{key('h'), onFacets},
		{tea.KeyPressMsg{Code: tea.KeyRight}, onList},
		{tea.KeyPressMsg{Code: tea.KeyTab}, onDetail},
		{tea.KeyPressMsg{Code: tea.KeyTab}, onFacets},
		{tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, onDetail},
		{esc(), onList},
	} {
		press(tui, step.key)

		if tui.focus != step.want {
			t.Fatalf("after %s the keys are on pane %d, want %d", step.key, tui.focus, step.want)
		}
	}
}

func TestJOnTheSidebarPicksTheNextFacetAndEnterGoesToTheList(t *testing.T) {
	t.Parallel()
	machine := withSkills(t, 2)
	machine.Plugin("github@official", "user", "")
	tui := newModel(t, machine)

	press(tui, key('h'), key('j'), key('j'))

	if rowLine(tui, "s00") != "" || rowLine(tui, "github@official") == "" {
		t.Errorf("Plugins facet does not show the plugin alone:\n%s", plain(tui))
	}

	press(tui, enter())

	if tui.focus != onList {
		t.Errorf("enter left the keys on pane %d, want the list", tui.focus)
	}
}

func TestHDoesNotMoveOntoAHiddenSidebar(t *testing.T) {
	t.Parallel()
	tui := newModel(t, withSkills(t, 3))
	resize(tui, 90, 20)

	press(tui, key('h'))

	if tui.focus != onList {
		t.Errorf("h put the keys on pane %d, want the list", tui.focus)
	}
}

func TestGJumpsToTheEndsAndCtrlDMovesHalfAPage(t *testing.T) {
	t.Parallel()
	tui := newModel(t, withSkills(t, 40))
	resize(tui, 80, 20)

	for _, step := range []struct {
		want string
		key  tea.KeyPressMsg
	}{
		{"s39", key('G')}, {"s00", key('g')}, {"s05", ctrl('d')}, {"s10", ctrl('d')}, {"s05", ctrl('u')},
	} {
		press(tui, step.key)

		if highlighted(tui) != step.want {
			t.Errorf("after %s highlighted %q, want %q", step.key, highlighted(tui), step.want)
		}
	}
}

func TestSpaceCyclesTheStateOfTheHighlightedRow(t *testing.T) {
	t.Parallel()
	tui := newModel(t, withSkills(t, 1))

	for _, want := range []equip.State{equip.ManualOnly, equip.Off, equip.On} {
		press(tui, key(' '))

		if tui.s.View().Rows[0].State != want {
			t.Errorf("space set %v, want %v", tui.s.View().Rows[0].State, want)
		}
	}
}

func TestJScrollsTheDetailPaneOfARowWithNoMCPServer(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")

	for i := range 20 {
		machine.Skill(filepath.Join(dir, "skills"), fmt.Sprintf("s%02d", i))
	}

	tui := newModel(t, machine)
	resize(tui, 120, 20)

	if line(tui, "Locations") != "" {
		t.Fatalf("Locations shows before scrolling:\n%s", plain(tui))
	}

	press(tui, key('l'), key('G'))

	if line(tui, "Locations") == "" {
		t.Errorf("G in the detail pane did not scroll to Locations:\n%s", plain(tui))
	}
}

func TestQuestionMarkListsEveryKeyUntilTheNextKey(t *testing.T) {
	t.Parallel()
	tui := newModel(t, withSkills(t, 1))

	press(tui, key('?'))

	for _, want := range []string{"ctrl+d", "cycle state", "measure", "presets"} {
		if line(tui, want) == "" {
			t.Errorf("key list does not show %q:\n%s", want, plain(tui))
		}
	}

	press(tui, key('j'))

	if line(tui, "ctrl+d") != "" {
		t.Errorf("a key did not close the key list:\n%s", plain(tui))
	}
}

func TestTildeWritesTheHomeDirAsATilde(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct{ path, home, want string }{
		{"/Users/me/code/app", "/Users/me", "~/code/app"},
		{"/Users/me", "/Users/me", "/Users/me"},
		{"/Users/meg/app", "/Users/me", "/Users/meg/app"},
		{"/srv/app", "", "/srv/app"},
	} {
		if got := tilde(testCase.path, testCase.home); got != testCase.want {
			t.Errorf("tilde(%q, %q) = %q, want %q", testCase.path, testCase.home, got, testCase.want)
		}
	}
}

func TestTinyTerminalSaysItIsTooSmall(t *testing.T) {
	t.Parallel()
	tui := newModel(t, withSkills(t, 3))
	resize(tui, 30, 6)

	if got := tui.View().Content; got != "terminal too small" {
		t.Errorf("view = %q, want terminal too small", got)
	}
}

// press feeds keys to tui and returns the last command.
func press(tui *model, keys ...tea.KeyPressMsg) tea.Cmd {
	var cmd tea.Cmd
	for _, k := range keys {
		_, cmd = tui.Update(k)
	}

	return cmd
}

func key(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

func down() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyDown} }

func quits(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}

	_, ok := cmd().(tea.QuitMsg)

	return ok
}

// line returns the first line of the view whose text, styles left out,
// contains s, styles left out. A line holds the sidebar, the list and the
// detail pane side by side, so s can match in any of them; for a list row,
// use rowLine.
func line(tui *model, s string) string {
	at := lineIndex(tui, 0, s)
	if at < 0 {
		return ""
	}

	return strings.Split(plain(tui), "\n")[at]
}

// plain is the view's text, styles left out.
func plain(tui *model) string { return styleCodes.ReplaceAllString(tui.View().Content, "") }

// lineIndex is the index of the first line of the view, from, whose text,
// styles left out, contains s; -1 with none.
func lineIndex(tui *model, from int, s string) int {
	lines := strings.Split(styleCodes.ReplaceAllString(tui.View().Content, ""), "\n")
	for i := max(from, 0); i < len(lines); i++ {
		if strings.Contains(lines[i], s) {
			return i
		}
	}

	return -1
}

// rowLine returns the first line of the view that holds the list row of
// name, its state glyph then name, styles left out.
func rowLine(tui *model, name string) string {
	row := regexp.MustCompile(`[●◉○] ` + regexp.QuoteMeta(name) + `[ *]`)
	for text := range strings.SplitSeq(plain(tui), "\n") {
		if row.MatchString(text) {
			return text
		}
	}

	return ""
}

// styleCodes matches the escape codes that style the view's text.
var styleCodes = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// highlighted returns the name of the highlighted row in the list tui shows.
func highlighted(tui *model) string {
	rows := tui.rows(tui.s.View())
	if tui.cur < len(rows) {
		return rows[tui.cur].Name
	}

	return ""
}

func TestMainScreenShowsProjectPathAndSkills(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")

	top, list, _ := strings.Cut(newModel(t, machine).View().Content, "\n")
	if !strings.Contains(top, machine.Root) {
		t.Errorf("top line %q does not show the Project path %q", top, machine.Root)
	}

	if !strings.Contains(list, "review") {
		t.Errorf("list %q does not show the skill", list)
	}
}

func TestStateKeySetsAnUnsavedOverrideOnTheHighlightedSkill(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "alpha")
	machine.Skill(machine.ClaudeSkills(), "beta")
	tui := newModel(t, machine)

	press(tui, down(), key('3'))

	if r := tui.s.View().Rows[1]; r.State != equip.Off || !r.Override {
		t.Errorf("beta = %+v, want an Override off", r)
	}

	top, _, _ := strings.Cut(tui.View().Content, "\n")
	if !strings.Contains(top, "1 unsaved") {
		t.Errorf("top line %q does not count 1 unsaved", top)
	}

	if strings.Count(tui.View().Content, "*") != 1 || strings.Contains(line(tui, "alpha"), "*") {
		t.Errorf("unsaved marker on the wrong row:\n%s", tui.View().Content)
	}

	press(tui, key('1'))

	if r := tui.s.View().Rows[1]; r.State != equip.On || !r.Override {
		t.Errorf("after 1, beta = %+v, want an Override on", r)
	}
}

func TestDetailPaneShowsStatesOriginAndFallback(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")

	tui := newModel(t, machine)
	if line(tui, "Origin  default") == "" {
		t.Errorf("view does not show the default origin:\n%s", tui.View().Content)
	}

	press(tui, key('3'))

	for _, want := range []string{
		"( ) 1 on", "( ) 2 manual-only", "(○) 3 off", "set by hand here", "without it: on (default)",
	} {
		if line(tui, want) == "" {
			t.Errorf("view does not show %q:\n%s", want, tui.View().Content)
		}
	}
}

func TestThreeTurnsOffAPlugin(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Plugin("github@official", "user", "")
	tui := newModel(t, machine)

	press(tui, key('3'))

	if r := tui.s.View().Rows[0]; r.State != equip.Off || !r.Override {
		t.Errorf("github@official = %+v, want an Override off", r)
	}

	// Each state shows the key that sets it.
	for _, want := range []string{"( ) 1 on", "(○) 3 off"} {
		if line(tui, want) == "" {
			t.Errorf("view does not show %q:\n%s", want, plain(tui))
		}
	}
}

func TestTwoLeavesAPluginAndSaysItHasNoManualOnlyState(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Plugin("github@official", "user", "")
	tui := newModel(t, machine)

	press(tui, key('2'))

	if r := tui.s.View().Rows[0]; r.State != equip.On || r.Override {
		t.Errorf("github@official = %+v, want on with no Override", r)
	}

	if line(tui, "plugins have no manual-only state") == "" {
		t.Errorf("view does not say plugins have no manual-only state:\n%s", plain(tui))
	}
}

func TestTwoLeavesAnMCPServerAndSaysItHasNoManualOnlyState(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.WriteFile(filepath.Join(machine.Home, ".claude.json"), `{"mcpServers": {"github": {"command": "gh"}}}`)
	tui := newModel(t, machine)

	press(tui, key('2'))

	if r := tui.s.View().Rows[0]; r.State != equip.On || r.Override {
		t.Errorf("github = %+v, want on with no Override", r)
	}

	if line(tui, "MCP servers have no manual-only state") == "" {
		t.Errorf("view does not say MCP servers have no manual-only state:\n%s", plain(tui))
	}
}

func TestTwoOnAPluginsMCPServerSaysMCPServersHaveNoManualOnlyState(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	machine.WriteFile(filepath.Join(dir, ".mcp.json"), `{"mcpServers": {"search": {"command": "search"}}}`)
	tui := newModel(t, machine)

	press(tui, tea.KeyPressMsg{Code: tea.KeyTab}, key('2'))

	if line(tui, "MCP servers have no manual-only state") == "" {
		t.Errorf("view does not say MCP servers have no manual-only state:\n%s", plain(tui))
	}
}

func TestThreeTurnsOffAnMCPServer(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.WriteFile(filepath.Join(machine.Home, ".claude.json"), `{"mcpServers": {"github": {"command": "gh"}}}`)
	tui := newModel(t, machine)

	press(tui, key('3'))

	if r := tui.s.View().Rows[0]; r.State != equip.Off || !r.Override {
		t.Errorf("github = %+v, want an Override off", r)
	}
}

func TestDetailPaneShowsThePluginsMarketplace(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Plugin("github@official", "user", "")
	machine.Skill(machine.ClaudeSkills(), "review")
	tui := newModel(t, machine)

	if line(tui, "From    official marketplace") == "" {
		t.Errorf("view does not show the Marketplace:\n%s", tui.View().Content)
	}

	press(tui, down())

	for _, pluginOnly := range []string{"From    ", "Contents  "} {
		if line(tui, pluginOnly) != "" {
			t.Errorf("a skill shows %q:\n%s", pluginOnly, tui.View().Content)
		}
	}
}

func TestDetailPaneMarksAPluginWithHooks(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	machine.WriteFile(filepath.Join(dir, "hooks", "hooks.json"), `{"hooks": {}}`)

	if got := line(newModel(t, machine), "Cost  "); !strings.Contains(got, "Claude Code ~0 + hook output, unknown") {
		t.Errorf("cost line %q does not mark the hooks", got)
	}
}

func TestDetailPaneListsThePluginsContents(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	machine.Skill(filepath.Join(dir, "skills"), "review") // "github:review" and "The review skill.": 10
	machine.WriteFile(filepath.Join(dir, ".mcp.json"), `{"mcpServers": {"search": {"command": "search"}}}`)
	tui := newModel(t, machine)

	press(tui, key('3'))

	// Only its MCP servers can be overridden.
	if line(tui, "Contents  skills follow the plugin, MCP servers too unless overridden") == "" {
		t.Errorf("contents header does not say the skills follow the plugin:\n%s", tui.View().Content)
	}

	if got := line(tui, "○ skill      review ~0"); !strings.Contains(got, "The review skill.") {
		t.Errorf("skill line %q does not show the skill's state, cost and description", got)
	}

	if !strings.HasSuffix(strings.TrimRight(line(tui, "○ MCP server search"), " │"), "?") {
		t.Errorf("view does not show the MCP server:\n%s", tui.View().Content)
	}
}

func TestTabThenStateKeyTurnsOffTheHighlightedMCPServerInsideThePlugin(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	machine.WriteFile(filepath.Join(dir, ".mcp.json"),
		`{"mcpServers": {"issues": {"command": "issues"}, "search": {"command": "search"}}}`)
	machine.Skill(filepath.Join(dir, "skills"), "review") // follows the plugin, so tab skips it
	tui := newModel(t, machine)
	up := tea.KeyPressMsg{Code: tea.KeyUp}

	// Down stops at the last server, so two ups are back on the first.
	press(tui, tea.KeyPressMsg{Code: tea.KeyTab}, down(), down(), down(), up, up, key('3'))

	if r := tui.s.View().Rows[0]; r.State != equip.On || r.Override {
		t.Errorf("plugin = %+v, want on with no Override", r)
	}

	got := line(tui, "MCP server issues")
	if !strings.Contains(got, "▸") || !strings.Contains(got, "○") || !strings.Contains(got, "ovr") {
		t.Errorf("issues line %q, want highlighted and off with the override mark", got)
	}

	if got := line(tui, "MCP server search"); !strings.Contains(got, "●") || strings.Contains(got, "▸") {
		t.Errorf("search line %q, want on and not highlighted", got)
	}
}

func TestDetailPaneMarksAPluginMCPServerChangedOutside(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	machine.WriteFile(filepath.Join(dir, ".mcp.json"), `{"mcpServers": {"search": {"command": "search"}}}`)
	machine.Skill(machine.ClaudeSkills(), "review")
	press(newModel(t, machine), down(), key('3'), key('s'))
	machine.WriteFile(filepath.Join(machine.Home, ".claude.json"),
		`{"projects": {"`+machine.Root+`": {"disabledMcpServers": ["plugin:github:search"]}}}`)

	tui := newModel(t, machine)

	if got := line(tui, "MCP server search"); !strings.Contains(got, "*") {
		t.Errorf("search line %q, want the unsaved marker", got)
	}

	if line(tui, "changed outside equip in Claude Code") == "" {
		t.Errorf("view does not show the note:\n%s", tui.View().Content)
	}
}

func TestStateKeyInTheDetailPaneActsOnARowWithNoMCPServers(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	tui := newModel(t, machine)

	press(tui, tea.KeyPressMsg{Code: tea.KeyTab}, key('3'))

	if r := tui.s.View().Rows[0]; r.State != equip.Off {
		t.Errorf("review = %+v, want off", r)
	}
}

func TestDetailPaneCutsAPluginSkillsDescriptionShort(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	long := strings.Repeat("word ", 20) + "end"
	machine.WriteFile(filepath.Join(machine.Plugin("github@official", "user", ""), "skills", "review", "SKILL.md"),
		"---\nname: review\ndescription: |\n  "+long+"\n  second line\n---\n")
	tui := newModel(t, machine)
	resize(tui, 160, 30)

	got := line(tui, "skill review")
	if !strings.Contains(got, "word") || strings.Contains(got, "end") {
		t.Errorf("skill line %q does not cut the description short", got)
	}

	if line(tui, "second line") != "" {
		t.Errorf("view shows the description's second line:\n%s", tui.View().Content)
	}
}

func TestDetailPaneShowsTheNote(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	press(newModel(t, machine), key('3'), key('s'))

	settings := filepath.Join(machine.Root, ".claude", "settings.local.json")

	err := os.WriteFile(settings, []byte(`{"skillOverrides": {"review": "on"}}`), 0o644)
	if err != nil {
		t.Fatal(err)
	}

	if tui := newModel(t, machine); line(tui, "changed outside equip in Claude Code") == "" {
		t.Errorf("view does not show the note:\n%s", tui.View().Content)
	}
}

func TestDropKeyDropsTheOverride(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	tui := newModel(t, machine)

	press(tui, key('3'), key('x'))

	if r := tui.s.View().Rows[0]; r.Override {
		t.Errorf("review = %+v, want no Override", r)
	}
}

func TestSaveKeyShowsTheErrorUntilTheNextKey(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	tui := newModel(t, machine)
	settings := filepath.Join(machine.Root, ".claude", "settings.local.json")
	machine.WriteFile(settings, `{"skillOverrides": {"review": "off"}}`)

	press(tui, key('3'), key('s'))

	const flash = "save failed: changed outside equip since open"
	if line(tui, flash) == "" {
		t.Errorf("s does not show the error:\n%s", tui.View().Content)
	}

	press(tui, down())

	if line(tui, flash) != "" {
		t.Error("the save error stays after the next key")
	}
}

func TestKeysWithNoSkillsDoNothing(t *testing.T) {
	t.Parallel()
	tui := newModel(t, equiptest.New(t))

	press(tui, down(), key('1'), key('x'))

	if n := tui.s.View().Unsaved; n != 0 {
		t.Errorf("Unsaved = %d, want 0", n)
	}
}

func TestQuitKeysQuit(t *testing.T) {
	t.Parallel()

	machine := equiptest.New(t)
	for _, k := range []tea.KeyPressMsg{
		key('q'),
		{Code: 'c', Mod: tea.ModCtrl},
	} {
		if !quits(press(newModel(t, machine), k)) {
			t.Errorf("%s did not quit", k)
		}
	}
}

func TestQuitWithUnsavedChangesAsksFirst(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	tui := newModel(t, machine)

	if quits(press(tui, key('3'), key('q'))) {
		t.Fatal("q quit with unsaved changes")
	}

	if line(tui, "Quit without saving? y/n") == "" {
		t.Errorf("view does not ask:\n%s", tui.View().Content)
	}

	if quits(press(tui, key('n'))) || line(tui, "Quit without saving?") != "" {
		t.Error("n did not cancel the quit")
	}

	if !quits(press(tui, key('q'), key('y'))) {
		t.Error("y did not quit")
	}
}

func TestDetailPaneShowsDescriptionAgentsLocationsAndCodexNote(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	claude := machine.Skill(machine.ClaudeSkills(), "review")
	codex := machine.Skill(filepath.Join(machine.Home, ".agents", "skills"), "review")
	tui := newModel(t, machine)

	for _, want := range []string{"The review skill.", "Claude Code, Codex", "not applied in Codex: "} {
		if line(tui, want) == "" {
			t.Errorf("view does not show %q:\n%s", want, tui.View().Content)
		}
	}

	if got := line(tui, tilde(claude, machine.Home)); !strings.Contains(got, "Claude Code") {
		t.Errorf("location line %q does not name Claude Code", got)
	}

	if got := line(tui, tilde(codex, machine.Home)); !strings.Contains(got, "Codex") {
		t.Errorf("location line %q does not name Codex", got)
	}
}

func TestTopLineShowsTheTotalOfEachAgentAsStatesChange(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review") // 23 bytes: 8
	machine.Skill(machine.CodexSkills(), "review")  // 23 bytes: 6, and the intro's 700
	tui := newModel(t, machine)

	press(tui, key('3'))

	top, _, _ := strings.Cut(plain(tui), "\n")
	for _, want := range []string{"Claude Code ~0", "Codex ~706"} {
		if !strings.Contains(top, want) {
			t.Errorf("top line %q does not show %q", top, want)
		}
	}
}

func TestDetailPaneShowsTheCostInEachAgent(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review") // 23 bytes: 8
	machine.Skill(machine.CodexSkills(), "review")  // 23 bytes: 6

	if got := line(newModel(t, machine), "Cost  "); !strings.Contains(got, "Claude Code ~8, Codex ~6") {
		t.Errorf("cost line %q does not show both costs", got)
	}
}

func TestMCPServerRowAndDetailPaneShowTheCostAsUnknown(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.WriteFile(filepath.Join(machine.Home, ".claude.json"), `{"mcpServers": {"github": {"command": "gh"}}}`)
	tui := newModel(t, machine)

	if got := rowLine(tui, "github"); !strings.Contains(got, " ?   │") {
		t.Errorf("row line %q does not show the cost as unmeasured", got)
	}

	if got := line(tui, "Cost  "); !strings.Contains(got, "Claude Code unmeasured") {
		t.Errorf("cost line %q does not show the cost as unmeasured", got)
	}
}

// serveMCP answers MCP requests as a modern server with the instructions
// "Use fake." and the tool a.
func serveMCP(writer http.ResponseWriter, httpReq *http.Request) {
	var req struct {
		Method string          `json:"method"`
		ID     json.RawMessage `json:"id"`
	}

	_ = json.NewDecoder(httpReq.Body).Decode(&req)

	if httpReq.Header.Get("Mcp-Protocol-Version") != "2026-07-28" {
		writer.WriteHeader(http.StatusBadRequest)

		return
	}

	result := `{"instructions":"Use fake."}`
	if req.Method == "tools/list" {
		result = `{"tools":[{"name":"a"}]}`
	}

	writer.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintf(writer, `{"jsonrpc":"2.0","id":%s,"result":%s}`, req.ID, result)
}

func TestMeasureKeyMeasuresTheHighlightedMCPServerInTheBackground(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(serveMCP))
	t.Cleanup(server.Close)
	machine := equiptest.New(t)
	machine.WriteFile(filepath.Join(machine.Home, ".claude.json"),
		`{"mcpServers": {"github": {"type": "http", "url": "`+server.URL+`"}}}`)
	tui := newModel(t, machine)

	cmd := press(tui, key('m'))
	if cmd == nil || !strings.Contains(line(tui, "Cost  "), "unmeasured") || line(tui, "measuring") == "" {
		t.Fatalf("view after m:\n%s\nwant the cost unmeasured while measuring, with a command", tui.View().Content)
	}

	tui.Update(cmd())

	// "Use fake." and mcp__github__a: 23 bytes.
	if got := rowLine(tui, "github"); !strings.Contains(got, "~8") || line(tui, "measuring") != "" {
		t.Errorf("view:\n%s\nwant the measured cost ~8, done measuring", tui.View().Content)
	}
}

// drain runs cmd and each command of a batch it returns, and feeds their
// messages to tui.
func drain(tui *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}

	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, each := range batch {
			drain(tui, each)
		}

		return
	}

	_, next := tui.Update(msg)
	drain(tui, next)
}

func TestOpeningMeasuresEveryServerInTheBackground(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(serveMCP))
	t.Cleanup(server.Close)

	locked := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(locked.Close)
	machine := equiptest.New(t)
	// The locked server refuses equip for good, which is no failure to count.
	machine.WriteFile(filepath.Join(machine.Home, ".claude.json"), `{"mcpServers": {"github": {"type": "http", "url": "`+
		server.URL+`"}, "locked": {"type": "http", "url": "`+locked.URL+`"}, "broken": {"command": "no-such-command"}}}`)
	tui := newModel(t, machine)

	cmd := tui.Init()
	if line(tui, "measuring 3") == "" {
		t.Errorf("top line does not say it measures 3 servers:\n%s", plain(tui))
	}

	drain(tui, cmd)

	if got := rowLine(tui, "github"); !strings.Contains(got, "~8") || line(tui, "measuring") != "" {
		t.Errorf("view:\n%s\nwant github measured, done measuring", plain(tui))
	}

	if line(tui, "1 MCP server could not be measured") == "" {
		t.Errorf("view does not count the server it could not measure:\n%s", plain(tui))
	}
}

func TestMeasureKeyShowsWhyAProbeFailed(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	tui := newModel(t, machine)

	tui.Update(press(tui, key('m'))())

	if line(tui, equip.ErrCannotProbe.Error()) == "" {
		t.Errorf("view does not say why:\n%s", tui.View().Content)
	}
}

func TestTopLineAndPluginRowMarkAServerNotMeasuredYet(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	machine.Skill(filepath.Join(dir, "skills"), "review") // "github:review" and "The review skill.": 10
	machine.WriteFile(filepath.Join(dir, ".mcp.json"), `{"mcpServers": {"search": {"command": "search"}}}`)
	tui := newModel(t, machine)

	if got := line(tui, "equip"); !strings.Contains(got, "Claude Code ~10 + 1 MCP unmeasured") {
		t.Errorf("top line %q does not count the unmeasured server", got)
	}

	if got := rowLine(tui, "github@official"); !strings.Contains(got, "~10+?") {
		t.Errorf("row line %q does not mark the plugin's cost partial", got)
	}
}

func TestTotalOfCountsTheUnmeasuredAndMarksOverBudget(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		want  string
		total equip.Total
	}{
		{"~0 + 1 MCP unmeasured", equip.Total{Tokens: 0, Unmeasured: 1, OverBudget: false}},
		{"~10 + 2 MCP unmeasured", equip.Total{Tokens: 10, Unmeasured: 2, OverBudget: false}},
		{"~6146, skills over budget", equip.Total{Tokens: 6146, Unmeasured: 0, OverBudget: true}},
		{"~6146 + 1 MCP unmeasured, skills over budget", equip.Total{Tokens: 6146, Unmeasured: 1, OverBudget: true}},
	} {
		t.Run(testCase.want, func(t *testing.T) {
			t.Parallel()

			if got := styleCodes.ReplaceAllString(totalOf(testCase.total, newStyles()), ""); got != testCase.want {
				t.Errorf("totalOf(%+v) = %q, want %q", testCase.total, got, testCase.want)
			}
		})
	}
}

func TestDetailPaneNamesTheKind(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.WriteFile(filepath.Join(machine.Home, ".claude.json"), `{"mcpServers": {"github": {"command": "gh"}}}`)

	// The list and the detail pane's title share the first line.
	if got := line(newModel(t, machine), "github"); !strings.Contains(got, "MCP server") {
		t.Errorf("line %q does not name the kind", got)
	}
}

func TestDetailPaneSaysWhyEquipDoesNotMeasureAServer(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.ClaudeBuiltins = map[string]equip.State{"computer-use": equip.On}

	if line(newModel(t, machine), "not measured: built into Claude Code") == "" {
		t.Error("detail pane does not say why the server is not measured")
	}
}

func TestSidebarCountsTheRowsOfEachFacet(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	machine.Skill(machine.ClaudeSkills(), "lint")
	machine.Plugin("github@official", "user", "")
	tui := newModel(t, machine)

	for _, want := range []string{`All +3\b`, `Skills +2\b`, `Plugins +1\b`, `MCP servers +0\b`, `Unsaved changes +0\b`} {
		if !regexp.MustCompile(want).MatchString(plain(tui)) {
			t.Errorf("sidebar does not show %q:\n%s", want, tui.View().Content)
		}
	}
}

func TestSidebarPutsABlankLineBeforeEachGroupOfFacets(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")

	view := newModel(t, machine).View().Content
	lines := strings.Split(view, "\n")
	near := func(s string, offset int) string {
		return lines[slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, s) })+offset]
	}

	// The header names the agents too, so find the kinds' last facet.
	if strings.Contains(near("By name", 1), "Claude Code") {
		t.Errorf("no blank line before the agents' facets:\n%s", view)
	}

	if !strings.Contains(near("Plugins", -1), "Skills") {
		t.Errorf("a blank line inside the kinds' facets:\n%s", view)
	}
}

func TestPickingAFacetNarrowsTheList(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	machine.Plugin("github@official", "user", "")
	tui := newModel(t, machine)

	press(tui, key(']'), key(']'))

	if view := tui.View().Content; strings.Contains(view, "review") || !strings.Contains(view, "github@official") {
		t.Errorf("Plugins facet does not show the plugin alone:\n%s", view)
	}

	if line(tui, "▸ Plugins") == "" {
		t.Errorf("sidebar does not mark the picked facet:\n%s", tui.View().Content)
	}
}

func TestPreviousFacetKeyWrapsToTheLastFacet(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	machine.Skill(machine.ClaudeSkills(), "lint")
	tui := newModel(t, machine)

	press(tui, down(), key('3'), key('['))

	if view := tui.View().Content; strings.Contains(view, "lint") || !strings.Contains(view, "review") {
		t.Errorf("Unsaved changes facet does not show review alone:\n%s", view)
	}
}

func TestPickingAFacetHighlightsTheFirstRow(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "alpha")
	machine.Skill(machine.ClaudeSkills(), "beta")
	tui := newModel(t, machine)

	press(tui, down(), key(']'))

	if got := highlighted(tui); got != "alpha" {
		t.Errorf("highlighted %q, want alpha", got)
	}
}

func TestPickingAFacetLeavesThePluginsContents(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	machine.WriteFile(filepath.Join(dir, ".mcp.json"), `{"mcpServers": {"search": {"command": "search"}}}`)
	tui := newModel(t, machine)
	tab := tea.KeyPressMsg{Code: tea.KeyTab}

	// search off, then the Unsaved changes facet, which keeps the plugin.
	press(tui, tab, key('3'), key('['))

	if tui.focus == onDetail {
		t.Error("keys still act on the MCP server after picking a facet")
	}
}

func TestKeysStayOnTheListAfterTheHighlightedPluginLeavesTheFacet(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	machine.WriteFile(filepath.Join(dir, ".mcp.json"), `{"mcpServers": {"search": {"command": "search"}}}`)
	tui := newModel(t, machine)

	// Off by hand, then saved from the Unsaved changes facet.
	tab := tea.KeyPressMsg{Code: tea.KeyTab}
	press(tui, tab, key('3'), key('['), tab, key('s'))

	if tui.focus == onDetail {
		t.Fatalf("keys still act on the MCP server of a plugin that left the facet:\n%s", tui.View().Content)
	}

	press(tui, key('3'))
}

func TestSlashSearchesTheListByName(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "Review")
	machine.Skill(machine.ClaudeSkills(), "lint")
	tui := newModel(t, machine)

	press(tui, key('/'), key('r'), key('E'), key('v'))

	if view := tui.View().Content; strings.Contains(view, "lint") || !strings.Contains(view, "Review") {
		t.Errorf("search /rEv does not show Review alone:\n%s", view)
	}
}

func TestSearchFindsAPluginByItsMCPServersName(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	dir := machine.Plugin("github@official", "user", "")
	machine.WriteFile(filepath.Join(dir, ".mcp.json"), `{"mcpServers": {"Issues": {"command": "issues"}}}`)
	machine.Skill(machine.ClaudeSkills(), "lint")
	tui := newModel(t, machine)

	press(tui, key('/'), key('i'), key('S'), key('s'))

	if view := tui.View().Content; strings.Contains(view, "lint") || !strings.Contains(view, "github@official") {
		t.Errorf("search /iSs does not show the plugin alone:\n%s", view)
	}
}

func TestEnterEndsTheSearchAndKeepsItsRows(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "lint")
	machine.Skill(machine.ClaudeSkills(), "review")
	tui := newModel(t, machine)

	press(tui, key('/'), key('i'), key('e'), key('w'), tea.KeyPressMsg{Code: tea.KeyEnter}, key('3'))

	if r := tui.s.View().Rows[1]; r.State != equip.Off {
		t.Errorf("review = %+v, want off", r)
	}

	if line(tui, "/iew") == "" {
		t.Errorf("list does not show the search:\n%s", tui.View().Content)
	}
}

func TestEscClearsTheSearch(t *testing.T) {
	t.Parallel()

	esc, enter := tea.KeyPressMsg{Code: tea.KeyEscape}, tea.KeyPressMsg{Code: tea.KeyEnter}

	for name, keys := range map[string][]tea.KeyPressMsg{
		"while typing": {key('/'), key('i'), esc},
		"after enter":  {key('/'), key('i'), enter, esc},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			machine := equiptest.New(t)
			machine.Skill(machine.ClaudeSkills(), "lint")
			machine.Skill(machine.ClaudeSkills(), "review")
			tui := newModel(t, machine)

			press(tui, append(keys, key('3'))...)

			if view := tui.View().Content; !strings.Contains(view, "lint") || strings.Contains(view, "/i") {
				t.Errorf("search stays after esc:\n%s", view)
			}

			if r := tui.s.View().Rows[0]; r.State != equip.Off {
				t.Errorf("lint = %+v, want off: the keys act on the list again", r)
			}
		})
	}
}

func TestBackspaceDeletesTheLastLetterOfTheSearch(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "lint")
	machine.Skill(machine.ClaudeSkills(), "review")
	tui := newModel(t, machine)

	press(tui, key('/'), key('é'), backspace())

	if tui.query != "" {
		t.Errorf("search is %q after é and backspace, want none", tui.query)
	}

	press(tui, backspace(), key('i'), key('e'), backspace())

	if line(tui, "/i") == "" || !strings.Contains(tui.View().Content, "lint") {
		t.Errorf("search is not /i:\n%s", tui.View().Content)
	}
}

func backspace() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyBackspace} }

func TestCtrlCQuitsWhileSearching(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "review")
	tui := newModel(t, machine)

	if cmd := press(tui, key('/'), tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); !quits(cmd) {
		t.Error("ctrl+c does not quit while the search is open")
	}
}

func TestSpecialKeysLeaveTheSearchAndHighlightAlone(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "lint")
	machine.Skill(machine.ClaudeSkills(), "review")
	tui := newModel(t, machine)

	press(tui, down(), key('/'), down())

	if got := highlighted(tui); got != "review" {
		t.Errorf("highlighted %q, want review", got)
	}
}

func TestTypingHighlightsTheFirstMatch(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)

	for _, name := range []string{"lint", "rename", "review"} {
		machine.Skill(machine.ClaudeSkills(), name)
	}

	tui := newModel(t, machine)

	press(tui, down(), down(), key('/'), key('e'))

	if got := highlighted(tui); got != "rename" {
		t.Errorf("highlighted %q, want rename", got)
	}
}

func TestHighlightLeavesTheContentsWhenAnotherRowTakesItsPlace(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	alpha := machine.Plugin("alpha@official", "user", "")
	machine.WriteFile(filepath.Join(alpha, ".mcp.json"),
		`{"mcpServers": {"one": {"command": "one"}, "two": {"command": "two"}}}`)
	beta := machine.Plugin("beta@official", "user", "")
	machine.WriteFile(filepath.Join(beta, ".mcp.json"), `{"mcpServers": {"three": {"command": "three"}}}`)
	tui := newModel(t, machine)

	// alpha's two highlighted, then a search that keeps beta alone.
	press(tui, tea.KeyPressMsg{Code: tea.KeyTab}, down(), key('/'), key('b'))

	if tui.focus == onDetail {
		t.Errorf("keys still act on MCP server %d, of beta now:\n%s", tui.server, tui.View().Content)
	}
}

func TestARowThatLeavesTheFacetStaysUntilTheHighlightMoves(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)

	for _, name := range []string{"alpha", "beta", "gamma"} {
		machine.Skill(machine.ClaudeSkills(), name)
	}

	tui := newModel(t, machine)

	// The On facet, then alpha off twice over.
	press(tui, key(']'), key(']'), key(']'), key(']'), key(']'), key(']'), key(']'), key('3'), key('3'))

	if r := tui.s.View().Rows[1]; r.State != equip.On {
		t.Errorf("beta = %+v, want on: the second key acts on alpha again", r)
	}

	press(tui, down())

	if got := highlighted(tui); got != "beta" || strings.Contains(tui.View().Content, "alpha") {
		t.Errorf("highlighted %q, want beta with alpha gone:\n%s", got, tui.View().Content)
	}
}

func TestHighlightStaysOnItsRowWhenTheSearchWidens(t *testing.T) {
	t.Parallel()

	esc, enter := tea.KeyPressMsg{Code: tea.KeyEscape}, tea.KeyPressMsg{Code: tea.KeyEnter}

	for name, keys := range map[string][]tea.KeyPressMsg{
		"backspace":    {key('/'), key('b'), key('e'), backspace(), backspace()},
		"esc typing":   {key('/'), key('b'), key('e'), esc},
		"esc after it": {key('/'), key('b'), key('e'), enter, esc},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			machine := equiptest.New(t)
			machine.Skill(machine.ClaudeSkills(), "alpha")
			machine.Skill(machine.ClaudeSkills(), "beta")
			tui := newModel(t, machine)

			press(tui, keys...)

			if got := highlighted(tui); got != "beta" || !strings.Contains(tui.View().Content, "alpha") {
				t.Errorf("highlighted %q, want beta in the whole list:\n%s", got, tui.View().Content)
			}
		})
	}
}

// movedAway saves github in each of states in its own clone of one repo, then
// removes the clones, as if moved. It returns their old paths and the repo
// they were cloned from, which has no record yet.
func movedAway(t *testing.T, machine *equiptest.Machine, states ...equip.State) ([]string, string) {
	t.Helper()

	repo := machine.Repo("app")
	machine.Commit(repo)
	machine.WriteFile(filepath.Join(machine.Home, ".claude.json"), `{"mcpServers": {"github": {"command": "gh"}}}`)

	olds := make([]string, 0, len(states))

	for i, state := range states {
		old := filepath.Join(machine.Root, fmt.Sprintf("old%d", i+1))
		machine.RunGit(repo, "clone", "-q", repo, old)

		session, err := equip.Open(machine.Machine, old)
		if err != nil {
			t.Fatal(err)
		}

		session.SetState("mcp:github", state)

		err = session.Save()
		if err == nil {
			err = os.RemoveAll(old)
		}

		if err != nil {
			t.Fatal(err)
		}

		olds = append(olds, old)
	}

	return olds, repo
}

// newModelIn is the model of a TUI opened in dir.
func newModelIn(t *testing.T, machine *equiptest.Machine, dir string) *model {
	t.Helper()

	session, err := equip.Open(machine.Machine, dir)
	if err != nil {
		t.Fatal(err)
	}

	// Wide enough for the long paths of the test's temp dirs.
	tui := newTUI(session, machine.Home)
	resize(tui, 400, 60)

	return tui
}

func TestAdoptPromptAdoptsTheRecordOfAMovedRepo(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	olds, repo := movedAway(t, machine, equip.Off)
	tui := newModelIn(t, machine, repo)

	if line(tui, "1 "+olds[0]) == "" {
		t.Fatalf("view does not offer %s:\n%s", olds[0], tui.View().Content)
	}

	press(tui, key('1'))

	if row := tui.s.View().Rows[0]; row.State != equip.Off || !row.Override || line(tui, olds[0]) != "" {
		t.Errorf("github = %+v, want an override off and no prompt:\n%s", row, tui.View().Content)
	}
}

func TestAdoptPromptPicksAmongSeveralMovedRepos(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	olds, repo := movedAway(t, machine, equip.Off, equip.On)
	tui := newModelIn(t, machine, repo)

	press(tui, key('2'))

	if row := tui.s.View().Rows[0]; row.State != equip.On || !row.Override || line(tui, olds[1]) != "" {
		t.Errorf("github = %+v, want the second record's override on and no prompt:\n%s", row, tui.View().Content)
	}
}

func TestAdoptPromptDeclinedKeepsTheImportAndIgnoresOtherKeys(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	olds, repo := movedAway(t, machine, equip.Off)
	tui := newModelIn(t, machine, repo)

	if quits(press(tui, key('2'), key('q'), key('n'))) {
		t.Fatal("q quit while asking to adopt")
	}

	if row := tui.s.View().Rows[0]; row.State != equip.On || row.Override || line(tui, olds[0]) != "" {
		t.Errorf("github = %+v, want on with no override and no prompt:\n%s", row, tui.View().Content)
	}
}

func TestAdoptPromptShowsAFailedAdoption(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	_, repo := movedAway(t, machine, equip.Off)
	records, _ := filepath.Glob(filepath.Join(machine.StateHome, "equip", "*.toml"))
	data, _ := os.ReadFile(records[0])
	machine.WriteFile(records[0], strings.Replace(string(data), "'off'", "'bogus'", 1))
	tui := newModelIn(t, machine, repo)

	press(tui, key('1'))

	if row := tui.s.View().Rows[0]; row.State != equip.On || row.Override || line(tui, "adopt failed") == "" {
		t.Errorf("github = %+v, want on with no override and the error:\n%s", row, tui.View().Content)
	}
}

func TestAdoptPromptListsNoMoreRecordsThanDigitKeys(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	_, repo := movedAway(t, machine, equip.Off)
	root := machine.RunGit(repo, "rev-list", "--max-parents=0", "HEAD")

	for i := range 9 {
		machine.WriteFile(filepath.Join(machine.StateHome, "equip", fmt.Sprintf("gone%d.toml", i)),
			fmt.Sprintf("path = '%s/gone%d'\nroot_commit = '%s'\n", machine.Root, i, root))
	}

	tui := newModelIn(t, machine, repo)

	if line(tui, "  9 ") == "" || line(tui, "  10 ") != "" {
		t.Errorf("prompt does not list exactly 9 records:\n%s", tui.View().Content)
	}
}

func TestCtrlCQuitsWhileAskingToAdopt(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	_, repo := movedAway(t, machine, equip.Off)

	if !quits(press(newModelIn(t, machine, repo), tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})) {
		t.Error("ctrl+c did not quit")
	}
}

// status is the text above the panes, styles left out.
func status(tui *model) string {
	head, _, _ := strings.Cut(plain(tui), "╭")

	return head
}

func TestStatusKeepsTheTotalsAndUnsavedCountInANarrowTerminal(t *testing.T) {
	t.Parallel()
	tui := newModel(t, withSkills(t, 3))
	resize(tui, 80, 24)
	press(tui, key('3'))

	for _, want := range []string{"Claude Code ~", "Codex ~", "1 unsaved"} {
		if !strings.Contains(status(tui), want) {
			t.Errorf("status %q does not show %q", status(tui), want)
		}
	}

	if why := fits(tui, 80, 24); why != "" {
		t.Errorf("view does not fit 80x24: %s\n%s", why, plain(tui))
	}
}

func TestListSaysHowManyRowsAreAboveOnceScrolled(t *testing.T) {
	t.Parallel()
	tui := newModel(t, withSkills(t, 40))
	resize(tui, 80, 20)

	if line(tui, "↑") != "" {
		t.Errorf("list at its top shows rows above:\n%s", plain(tui))
	}

	press(tui, key('G'))

	if !strings.Contains(line(tui, "↑"), "more") {
		t.Errorf("scrolled list does not say how many rows are above:\n%s", plain(tui))
	}
}

func TestByNameSkillShowsByNameInPlaceOfItsCost(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.WriteFile(filepath.Join(machine.ClaudeSkills(), "ship", "SKILL.md"),
		"---\nname: ship\ndescription: x\ndisable-model-invocation: true\n---\n")
	tui := newModel(t, machine)

	if got := rowLine(tui, "ship"); !strings.Contains(got, "by name") {
		t.Errorf("row %q does not say by name", got)
	}

	if got := line(tui, "Cost  "); !strings.Contains(got, "called by name only") {
		t.Errorf("detail cost %q does not say called by name only", got)
	}
}

func TestManualOnlySkillReadsManualWhereNoAgentListsIt(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "both")
	machine.Skill(machine.CodexSkills(), "both")
	machine.Skill(machine.ClaudeSkills(), "solo")
	tui := newModel(t, machine)

	press(tui, key('2'), down(), key('2'))

	if got := rowLine(tui, "solo"); !strings.Contains(got, "manual-only") {
		t.Errorf("row %q does not say manual-only", got)
	}
	// Codex has no per-project skill setting, so it still lists this one.
	if got := rowLine(tui, "both"); strings.Contains(got, "manual") || !strings.Contains(got, "~") {
		t.Errorf("row %q hides its Codex cost", got)
	}
}

// withPluginSkill is a machine with the plugin github@official, whose skill
// review has the second row, under its plugin's.
func withPluginSkill(t *testing.T) *equiptest.Machine {
	t.Helper()

	machine := equiptest.New(t)
	machine.Skill(filepath.Join(machine.Plugin("github@official", "user", ""), "skills"), "review")

	return machine
}

func TestPluginSkillRowNamesItsPluginAndItsPluginsNameFindsIt(t *testing.T) {
	t.Parallel()
	tui := newModel(t, withPluginSkill(t))

	if got := rowLine(tui, "review"); !strings.Contains(got, "review  github") {
		t.Errorf("row %q does not name its plugin", got)
	}

	press(tui, key('/'), key('g'), key('i'), key('t'))

	if rowLine(tui, "review") == "" {
		t.Errorf("a search for its plugin leaves out the plugin's skill:\n%s", plain(tui))
	}
}

func TestStateKeysOnAPluginSkillRowChangeNothingAndSayWhatItFollows(t *testing.T) {
	t.Parallel()
	tui := newModel(t, withPluginSkill(t))

	press(tui, down(), key(' '), key('1'), key('3'), key('x'), key('m'))

	if n := tui.s.View().Unsaved; n != 0 {
		t.Errorf("Unsaved = %d, want 0", n)
	}

	if line(tui, "follows github@official, enter goes to it") == "" {
		t.Errorf("state keys do not say what the skill follows:\n%s", plain(tui))
	}
}

func TestEnterOnAPluginSkillRowMovesToItsPlugin(t *testing.T) {
	t.Parallel()
	tui := newModel(t, withPluginSkill(t))

	press(tui, key(']'), enter())

	if tui.key != "github@official" || tui.focus != onList {
		t.Errorf("enter left the highlight on %q in pane %d, want the plugin in the list", tui.key, tui.focus)
	}

	if got := rowLine(tui, "github@official"); !strings.Contains(got, "▸") {
		t.Errorf("plugin row %q is not highlighted:\n%s", got, plain(tui))
	}
}

func TestEnterOnAPluginSkillRowShowsItsPluginFromTheTop(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	skills := filepath.Join(machine.Plugin("github@official", "user", ""), "skills")
	machine.WriteFile(filepath.Join(skills, "review", "SKILL.md"),
		"---\nname: review\ndescription: "+strings.Repeat("word ", 400)+"\n---\n")
	tui := newModel(t, machine)
	resize(tui, 120, 20)

	press(tui, down(), key('l'), key('j'), key('j'), key('j'), key('h'))

	if tui.detailTop == 0 {
		t.Fatal("the skill's detail pane did not scroll")
	}

	press(tui, enter())

	if tui.detailTop != 0 {
		t.Errorf("detailTop = %d on the plugin, want 0", tui.detailTop)
	}
}

func TestDetailPaneOfAPluginSkillSaysItFollowsItsPlugin(t *testing.T) {
	t.Parallel()
	tui := newModel(t, withPluginSkill(t))

	press(tui, down())

	if got := line(tui, "Origin"); !strings.Contains(got, "follows github@official") {
		t.Errorf("origin %q does not name the plugin", got)
	}

	if line(tui, "1 on") != "" {
		t.Errorf("detail pane offers states:\n%s", plain(tui))
	}
}

func TestSaveSaysHowManyChangesItWrote(t *testing.T) {
	t.Parallel()
	tui := newModel(t, withSkills(t, 2))

	press(tui, key('3'), key('s'))

	if line(tui, "saved 1 change") == "" {
		t.Errorf("save does not say what it wrote:\n%s", plain(tui))
	}
}

func TestSidebarCountsTheRowsTheSearchKeeps(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Skill(machine.ClaudeSkills(), "alpha")
	machine.Skill(machine.ClaudeSkills(), "beta")
	tui := newModel(t, machine)

	press(tui, key('/'), key('a'), key('l'))

	if got := strings.Fields(line(tui, "Skills")); len(got) < 3 || got[2] != "1" {
		t.Errorf("Skills facet line %q does not count 1 match", line(tui, "Skills"))
	}
}

func TestPluginRowCutsItsMarketplaceBeforeItsName(t *testing.T) {
	t.Parallel()
	machine := equiptest.New(t)
	machine.Plugin("chrome-devtools-mcp@claude-plugins-official", "user", "")
	tui := newModel(t, machine)
	resize(tui, 80, 20)

	if line(tui, "chrome-devtools-mcp@") == "" {
		t.Errorf("plugin row lost its name:\n%s", plain(tui))
	}
}
