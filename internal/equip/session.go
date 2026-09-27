package equip

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// ErrChangedSinceOpen is the error of a save that found owned entries
// changed after equip read them. The save wrote nothing.
var ErrChangedSinceOpen = errors.New("changed outside equip since open")

// ErrCannotProbe is the error of a probe of an extension equip does not
// start: one that is not an MCP server an agent has on and trusted.
var ErrCannotProbe = errors.New("equip measures only an MCP server an agent has on and trusted")

// ErrNotOrphan is the error of an adoption of a record the Project was not
// offered.
var ErrNotOrphan = errors.New("not a record this project can adopt")

// Project is the git repo equip runs in, taken at the main checkout's root,
// or the directory itself outside git.
type Project struct {
	Path       string // symlinks resolved
	RootCommit string // empty outside git or with no commits
	gitDir     string // the main checkout's .git; empty outside git
	checkout   string // the root of the checkout equip runs in: a worktree's own; Path outside git
}

// State is how an extension takes part in a Project's sessions.
type State int

// The states, in the order the user picks them.
const (
	On State = iota
	ManualOnly
	Off
)

// States returns the states, in the order the user picks them.
func States() []State { return []State{On, ManualOnly, Off} }

func (s State) String() string {
	return [...]string{On: "on", ManualOnly: "manual-only", Off: "off"}[s]
}

// Session is one open Project.
type Session struct {
	pending  choice                     // the active presets and Overrides, pending
	saved    map[string]State           // the overrides at the last save
	disk     map[Agent]map[string]State // each agent's entries for its applied exts, as last read or written
	outside  map[string]Agent           // changed outside equip since the last save, in that agent
	applied  map[Agent][]Extension      // the exts whose states equip writes for each agent
	measured map[string]measurement     // the MCP servers measured, by key; guarded by mu
	codex    codexConfig
	machine  Machine
	project  Project
	orphans  []string // the paths of the records the Project can adopt
	exts     []Extension
	draft    *Preset // the one preset with unwritten edits, as edited; nil with none
	// unfinished are the other Projects that use the draft, opened by a
	// write of it that failed, before its preset file changed; nil with none.
	unfinished []Affected
	recorded   []recordPreset // the active presets at the last save
	mu         sync.Mutex
	approvals  bool // Claude Code takes the .mcp.json approvals in settings.local.json
}

// View is what the user sees of a Session.
type View struct {
	Project Project
	Totals  map[Agent]int // the estimated tokens of a session in each agent
	// Unknown marks each agent whose total leaves out an MCP server that is
	// on but not measured yet.
	Unknown map[Agent]bool
	// OverBudget marks each agent whose skill listing passes its listing
	// budget, so the agent shortens or drops what it lists.
	OverBudget map[Agent]bool
	Rows       []Row
	Facets     []Facet // the ways to narrow Rows, in the order the sidebar shows them
	// Orphans are the paths of the records the Project can adopt on its first
	// open: of a repo with its root commit whose path no longer exists.
	Orphans []string
	Presets []string // the names of the active presets, pending
	Unsaved int      // pending changes a save would write
}

// Facet is a way to narrow the list.
type Facet struct {
	keys     map[string]bool // of the rows it keeps
	Name     string
	NewGroup bool // the first of the agents' facets, of the states' or of the changes'
}

// Has reports whether the facet keeps row.
func (f Facet) Has(row Row) bool { return f.keys[row.Key] }

// Count is the number of rows the facet keeps.
func (f Facet) Count() int { return len(f.keys) }

// facet is the name of a Facet and the test of the rows it keeps.
type facet struct {
	has  func(ext Extension, row Row) bool
	name string
}

// kindFacet is the facet of the rows of kind k.
func kindFacet(name string, k Kind) facet {
	return facet{name: name, has: func(_ Extension, row Row) bool { return row.Kind == k }}
}

// onlyFacet is the facet of the rows only agent has.
func onlyFacet(agent Agent) facet {
	return facet{name: agent.String() + " only", has: func(ext Extension, _ Row) bool {
		return !slices.ContainsFunc(Agents(), func(other Agent) bool { return other != agent && ext.has(other) })
	}}
}

// stateFacet is the facet of the rows in state st.
func stateFacet(name string, st State) facet {
	return facet{name: name, has: func(_ Extension, row Row) bool { return row.State == st }}
}

// Row is one extension in the list.
type Row struct {
	Key         string // what SetState, Detail and DropOverride take
	Name        string
	Kind        Kind
	Cost        int // estimated tokens: the higher of the agents' costs
	State       State
	Fallback    State // the state without the Override
	CostUnknown bool  // an MCP server not measured yet
	Override    bool  // State was set by hand in this Project
	Unsaved     bool
	// ChangedOutside reports that the row changed through an edit outside
	// equip in Claude Code.
	ChangedOutside bool
}

// Open finds the Project of dir and discovers its extensions.
func Open(machine Machine, dir string) (*Session, error) {
	// Resolved, as ~/.claude.json keys projects that way.
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("locate project: %w", err)
	}

	project := locate(machine, dir)

	codex := readCodexConfig(machine, project)

	exts, err := discover(machine, project, dir, codex)
	if err != nil {
		return nil, err
	}

	applied := appliedExts(exts, codex)
	disk := map[Agent]map[string]State{}

	for _, agent := range Agents() {
		// ponytail: broken config reads as no entries here; Save reports it.
		disk[agent], _ = agent.config().read(machine, project, applied[agent])
	}

	saved, presets, err := readRecord(recordPath(machine, project.Path), firstStates(disk))
	if err != nil {
		return nil, err
	}

	library, err := readPresets(machine)
	if err != nil {
		return nil, err
	}

	session := &Session{
		machine:    machine,
		project:    project,
		exts:       exts,
		pending:    choice{library: library, active: nil, overrides: nil, exts: exts, applied: applied},
		draft:      nil,
		unfinished: nil,
		recorded:   nil,
		applied:    applied,
		codex:      codex,
		saved:      nil,
		disk:       nil,
		outside:    nil,
		measured:   map[string]measurement{},
		approvals:  approvalsCount(machine, project),
		orphans:    orphans(machine, project),
		mu:         sync.Mutex{},
	}
	session.start(saved, presets, disk)
	session.readMeasurements()

	return session, nil
}

// firstStates are the states of disk, each agent's entries. Where the agents
// disagree, the first agent's state is the record's, and the other's shows
// as changed outside.
func firstStates(disk map[Agent]map[string]State) map[string]State {
	all := map[string]State{}

	for _, agent := range Agents() {
		for key, st := range disk[agent] {
			if _, ok := all[key]; !ok {
				all[key] = st
			}
		}
	}

	return all
}

// appliedExts are the exts whose states equip writes for each agent, with
// Codex's config codex.
func appliedExts(exts []Extension, codex codexConfig) map[Agent][]Extension {
	applied := map[Agent][]Extension{}

	for _, agent := range Agents() {
		for _, ext := range exts {
			// Codex has no per-project skill setting.
			if ext.has(agent) && (agent == ClaudeCode || ext.Kind != Skill && codex.notApplied == "") {
				applied[agent] = append(applied[agent], ext)
			}
		}
	}

	return applied
}

// adapter is how equip reads and writes an agent's config for a Project.
type adapter struct {
	// read reads the states the agent has for exts.
	read func(machine Machine, project Project, exts []Extension) (map[string]State, error)
	// write writes the states of overrides for exts.
	write func(machine Machine, project Project, exts []Extension, overrides map[string]State) error
}

// config is agent's adapter.
func (a Agent) config() adapter {
	return [...]adapter{
		ClaudeCode: {read: readClaude, write: writeClaude},
		Codex:      {read: readCodex, write: writeCodex},
	}[a]
}

// SetState makes st an Override for the extension with key, unless the
// extension does not offer st.
func (s *Session) SetState(key string, st State) {
	if ext, ok := s.ext(key); ok && !slices.Contains(ext.Kind.states(), st) {
		return
	}

	s.pending.overrides[key] = st
}

// View returns the current view.
func (s *Session) View() View {
	rows := make([]Row, 0, len(s.exts))

	for _, ext := range s.exts {
		// A plugin's MCP server shows among the plugin's contents.
		if ext.plugin == "" {
			rows = append(rows, s.row(ext))
		}
	}

	totals, unknown, over := s.totals()

	var presets []string

	for _, preset := range s.pending.library {
		if slices.Contains(s.pending.active, preset.ID) {
			presets = append(presets, preset.Name)
		}
	}

	presets = append(presets, s.missing(s.pending.active)...)

	return View{
		Project: s.project, Rows: rows, Facets: s.facets(rows), Unsaved: s.unsavedCount(), Totals: totals, Unknown: unknown,
		OverBudget: over, Orphans: s.orphans, Presets: presets,
	}
}

// fixedCost is the tokens of the blocks agent puts into a session once: the
// skills intro when it lists skills, and Codex's plugins block when a plugin
// is on.
func fixedCost(agent Agent, listed, plugins bool) int {
	cost := 0
	if listed {
		cost += agent.listing().introTokens
	}

	if plugins && agent == Codex {
		cost += tokens(Codex, codexPluginsBlockBytes)
	}

	return cost
}

// Detail is what the detail pane shows of one extension.
type Detail struct {
	NotApplied  map[Agent]string // why an agent that has it does not get its state
	Costs       map[Agent]int    // estimated tokens in each agent that has it
	Description string
	// Note says why the row differs from the agent config while an active
	// preset changed outside equip or is missing.
	Note        string
	Marketplace string  // a plugin's
	Agents      []Agent // the agents that have it
	Locations   []Location
	States      []State   // the states the user can pick
	Contents    []Content // a plugin's skills
	Hooks       bool      // a plugin has hooks, whose output adds an unknown cost
	BuiltIn     bool      // built into Claude Code, so it has no Locations
	// ChangedIn is the agent whose config changed outside equip, when the
	// row is ChangedOutside.
	ChangedIn Agent
}

// Content is one extension inside a plugin. A skill follows its plugin and is
// never an Override; an MCP server follows it unless it has an Override.
type Content struct {
	Key         string // an MCP server's, what SetState and DropOverride take; a skill has none
	Name        string
	Description string
	Kind        Kind
	State       State
	Cost        int  // estimated tokens in the agent the plugin was read for, Claude Code when both have it
	CostUnknown bool // an MCP server not measured yet
	Override    bool // an MCP server's State was set by hand in this Project
	Unsaved     bool // a save would change an MCP server's Override or entries
	// ChangedOutside reports that an MCP server changed through an edit
	// outside equip, in the agent ChangedIn.
	ChangedOutside bool
	ChangedIn      Agent
}

// Detail returns the detail of the extension with key.
func (s *Session) Detail(key string) Detail {
	ext, ok := s.ext(key)
	if !ok {
		return Detail{
			Description: "", Note: "", Marketplace: "", Agents: nil, Locations: nil, NotApplied: nil, Costs: nil,
			States: nil, Contents: nil, Hooks: false, BuiltIn: false, ChangedIn: ClaudeCode,
		}
	}

	detail := Detail{
		Description: ext.Description, Note: "", Marketplace: marketplaceOf(ext.Key), Agents: nil,
		Locations: ext.Locations, NotApplied: map[Agent]string{}, Costs: map[Agent]int{}, States: ext.Kind.states(),
		Contents: s.contents(ext), Hooks: ext.hooks, BuiltIn: ext.builtIn, ChangedIn: s.outside[key],
	}
	// Only where the record's state differs from the agent config.
	if s.inRow(ext, func(key string) bool {
		return slices.ContainsFunc(Agents(), func(agent Agent) bool {
			return differ(s.lastSave().entries(agent), s.disk[agent], key)
		})
	}) {
		detail.Note = s.presetNote()
	}

	for _, agent := range Agents() {
		if ext.has(agent) {
			detail.Agents = append(detail.Agents, agent)
			detail.Costs[agent] = s.costIn(agent, ext)
		}
	}

	switch {
	case !ext.has(Codex):
	case ext.Kind == Skill:
		detail.NotApplied[Codex] = "Codex has no per-project skill setting"
	case s.codex.notApplied != "":
		detail.NotApplied[Codex] = s.codex.notApplied
	}

	return detail
}

// ProbeCost returns a probe that starts the MCP server with key and measures
// its cost. The probe may run on another goroutine: it touches the Session
// only to record the cost.
func (s *Session) ProbeCost(key string) func() error {
	ext, _ := s.ext(key)

	for _, agent := range Agents() {
		cfg, ok := s.probeConfig(agent, ext)
		if !ok || !s.onAndTrusted(agent, ext) {
			continue
		}

		return func() error {
			// An agent starts a server in the checkout the session runs in.
			measured, err := probe(context.Background(), s.machine.Env, s.project.checkout, cfg)
			if err != nil {
				return err
			}

			s.mu.Lock()
			s.measured[key] = measured
			s.mu.Unlock()

			return writeCache(s.machine, cfg, measured)
		}
	}

	return func() error { return ErrCannotProbe }
}

// Adopt moves the record of the moved repo at path, one of View's Orphans, to
// the Project, in place of the states imported at open.
func (s *Session) Adopt(path string) error {
	// Listed again, as another session may have adopted it since open.
	if !slices.Contains(orphans(s.machine, s.project), path) {
		return fmt.Errorf("%w: %s", ErrNotOrphan, path)
	}

	old, disk := recordPath(s.machine, path), s.disk

	saved, presets, err := readRecord(old, firstStates(disk))
	if err != nil {
		return err
	}

	err = writeRecord(s.machine, s.project, saved, presets)
	if err != nil {
		return err
	}

	err = os.Remove(old)
	if err != nil {
		return fmt.Errorf("remove the adopted record: %w", err)
	}

	s.start(saved, presets, disk)
	s.orphans = nil

	return nil
}

// DropOverride removes the Override for key, so the extension falls back.
func (s *Session) DropOverride(key string) { delete(s.pending.overrides, key) }

// Save writes the pending Overrides into each agent's config. If an agent's
// entries changed since they were read, it writes nothing, imports the
// changes and returns ErrChangedSinceOpen.
func (s *Session) Save() error {
	changed, err := s.changedOutside()
	if err != nil {
		return err
	}

	if changed {
		return ErrChangedSinceOpen
	}
	// Nothing to write. With saved Overrides, a save still writes them, so
	// the record of a first open's imports gets created. With records on
	// offer, it creates an empty record, so the next open offers them no more.
	nothing := s.unsavedCount() == 0 && len(s.saved) == 0
	if nothing && len(s.orphans) == 0 {
		return nil
	}
	// Written before the record, so a failed record write does not make
	// equip's own entries look changed outside.
	if !nothing {
		err = s.writeAgents(s.pending)
		if err != nil {
			return err
		}
	}

	presets := s.pending.record()

	err = writeRecord(s.machine, s.project, s.pending.overrides, presets)
	if err != nil {
		return err
	}

	s.saved, s.recorded = maps.Clone(s.pending.overrides), presets
	s.orphans = nil
	clear(s.outside)

	return nil
}

// row is the row of ext in the list.
func (s *Session) row(ext Extension) Row {
	state, override := s.pending.state(ext)
	cost := 0

	for _, agent := range Agents() {
		cost = max(cost, s.costIn(agent, ext))
	}

	_, changed := s.outside[ext.Key]

	return Row{
		Key:            ext.Key,
		Name:           ext.name(),
		Kind:           ext.Kind,
		Cost:           cost,
		CostUnknown:    s.unknown(ext),
		State:          state,
		Override:       override,
		Fallback:       s.pending.base(ext.primary(), ext),
		Unsaved:        s.inRow(ext, s.unsaved),
		ChangedOutside: changed,
	}
}

// start takes saved and presets, the Overrides and active presets of the
// record, and disk, each agent's entries, as an open does.
func (s *Session) start(saved map[string]State, presets []recordPreset, disk map[Agent]map[string]State) {
	s.saved, s.pending.overrides = saved, maps.Clone(saved)
	s.recorded, s.pending.active = presets, ids(presets)
	s.disk, s.outside = map[Agent]map[string]State{}, map[string]Agent{}

	for _, agent := range Agents() {
		s.take(agent, disk[agent])
	}
}

// lastSave is the choice of the last save, with the library as it is now.
func (s *Session) lastSave() choice {
	saved := s.pending
	saved.overrides, saved.active = s.saved, ids(s.recorded)

	return saved
}

// facets are the facets of rows, in the order the sidebar shows them.
func (s *Session) facets(rows []Row) []Facet {
	groups := [][]facet{
		{
			{name: "All", has: func(Extension, Row) bool { return true }},
			kindFacet("Skills", Skill), kindFacet("Plugins", Plugin), kindFacet("MCP servers", MCPServer),
		},
		{onlyFacet(ClaudeCode), onlyFacet(Codex)},
		{stateFacet("On", On), stateFacet("Manual-only", ManualOnly), stateFacet("Off", Off)},
		{
			{name: "Overrides", has: func(ext Extension, _ Row) bool { return s.inRow(ext, s.overridden) }},
			{name: "Unsaved changes", has: func(_ Extension, row Row) bool { return row.Unsaved }},
		},
	}

	var out []Facet

	for groupIndex, group := range groups {
		for index, def := range group {
			keys := map[string]bool{}

			for _, row := range rows {
				if ext, _ := s.ext(row.Key); def.has(ext, row) {
					keys[row.Key] = true
				}
			}

			out = append(out, Facet{Name: def.name, keys: keys, NewGroup: groupIndex > 0 && index == 0})
		}
	}

	return out
}

// totals are the estimated tokens of a session in each agent, whether each
// leaves out an MCP server that is on but not measured yet, and whether each
// agent's skill listing passes its listing budget.
func (s *Session) totals() (map[Agent]int, map[Agent]bool, map[Agent]bool) {
	totals, unknown, over := map[Agent]int{}, map[Agent]bool{}, map[Agent]bool{}

	for _, agent := range Agents() {
		totals[agent], unknown[agent], over[agent] = s.total(agent)
	}

	return totals, unknown, over
}

// total is the estimated tokens of a session in agent, whether it leaves out
// an MCP server that is on but not measured yet, and whether its skill listing
// passes agent's listing budget.
func (s *Session) total(agent Agent) (int, bool, bool) {
	total, listingTokens, unknown := 0, 0, false
	listed, plugins := false, false // a skill or plugin is on, so agent lists skills; a plugin is on

	for _, ext := range s.exts {
		// A plugin's MCP server counts in its plugin's cost.
		if ext.plugin != "" {
			continue
		}

		cost := s.costIn(agent, ext)
		total += cost

		if ext.Kind != MCPServer && cost > 0 {
			listingTokens += ext.cost[agent] // of its skills alone, without a plugin's MCP servers
			listed = true
		}

		plugins = plugins || ext.Kind == Plugin && ext.has(agent) && s.pending.stateIn(agent, ext) == On
		unknown = unknown || s.unknownIn(agent, ext)
	}

	budget := agent.listing().budget

	return total + fixedCost(agent, listed, plugins), unknown, budget > 0 && listingTokens > budget
}

// readMeasurements takes the cached measurement of each MCP server, from its
// config in the first agent that has one cached.
func (s *Session) readMeasurements() {
	for _, ext := range s.exts {
		for _, agent := range Agents() {
			cfg, ok := s.probeConfig(agent, ext)
			if !ok {
				continue
			}

			if measured, cached := readCache(s.machine, cfg); cached {
				s.measured[ext.Key] = measured

				break
			}
		}
	}
}

// probeConfig is how to reach the MCP server ext as agent does. It reports
// whether agent has a config equip can probe: a command, or the URL of a
// streamable HTTP server. ponytail: not the deprecated SSE transport.
func (s *Session) probeConfig(agent Agent, ext Extension) (serverConfig, bool) {
	var cfg serverConfig

	err := json.Unmarshal(ext.config[agent], &cfg)
	if agent == ClaudeCode {
		// A plugin's MCP server has its plugin's dir as its one Location.
		pluginRoot := ""
		if ext.plugin != "" {
			pluginRoot = ext.Locations[0].Path
		}

		cfg = expandVars(cfg, s.machine.Env, pluginRoot)
	}

	// Codex keeps a remote server's headers and token apart.
	headers := maps.Clone(cfg.Headers)
	if headers == nil {
		headers = map[string]string{}
	}

	maps.Copy(headers, cfg.HTTPHeaders)

	if cfg.BearerToken != "" {
		headers["Authorization"] = "Bearer " + getenv(s.machine.Env, cfg.BearerToken)
	}

	cfg.Headers, cfg.HTTPHeaders = headers, nil

	return cfg, err == nil && cfg.Type != "sse" && (cfg.Command != "" || cfg.URL != "")
}

// onAndTrusted reports whether agent has ext on and trusts it, as its config
// on disk says, so pending changes do not count.
func (s *Session) onAndTrusted(agent Agent, ext Extension) bool {
	state, onDisk := s.disk[agent][ext.Key]
	// Claude Code trusts a .mcp.json server only once the user approves it,
	// in a folder they trust. ponytail: approval by
	// enableAllProjectMcpServers does not count; read it if users approve
	// that way.
	if agent == ClaudeCode && ext.lists.settings && (!onDisk || !s.approvals) {
		return false
	}

	if !onDisk {
		state = ext.fallback[agent]
	}
	// A plugin's MCP server loads only while its plugin is on.
	plugin, inPlugin := s.ext(ext.plugin)

	return state == On && (!inPlugin || s.onAndTrusted(agent, plugin))
}

// approvalsCount reports whether Claude Code takes the approvals of .mcp.json
// servers in the Project's settings.local.json: only in a folder the user
// trusts, and only when git does not track the file, as a cloned repo cannot
// approve its own servers.
func approvalsCount(machine Machine, project Project) bool {
	var trusted bool

	config, _ := readJSONObject(claudeJSONPath(machine))
	_ = json.Unmarshal(config.object("projects").object(project.Path)["hasTrustDialogAccepted"], &trusted)

	return trusted && !tracked(machine, project, settingsRel)
}

// unknown reports whether the cost of ext is unknown, in part for a plugin:
// an MCP server's, until it is measured.
func (s *Session) unknown(ext Extension) bool {
	_, measured := s.measurement(ext.Key)

	return ext.Kind == MCPServer && !measured ||
		slices.ContainsFunc(Agents(), func(agent Agent) bool { return s.unknownIn(agent, ext) })
}

// unknownIn reports whether agent's cost of ext leaves out an MCP server
// that is on in agent but not measured yet: ext, or one in plugin ext.
func (s *Session) unknownIn(agent Agent, ext Extension) bool {
	if !ext.has(agent) || s.pending.stateIn(agent, ext) != On {
		return false
	}

	if ext.Kind == MCPServer {
		_, measured := s.measurement(ext.Key)

		return !measured
	}

	return slices.ContainsFunc(s.exts, func(server Extension) bool {
		return server.plugin == ext.Key && s.unknownIn(agent, server)
	})
}

// measurement returns the measurement of the MCP server with key, reporting
// whether it has one.
func (s *Session) measurement(key string) (measurement, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	measured, ok := s.measured[key]

	return measured, ok
}

// contents are the plugin's skills, which follow it, then its MCP servers,
// which follow it unless they have an Override.
func (s *Session) contents(plugin Extension) []Content {
	var contents []Content

	state, _ := s.pending.state(plugin)

	for _, content := range plugin.contents {
		content.State = state
		if state != On {
			content.Cost = 0
		}

		contents = append(contents, content)
	}

	for _, server := range s.exts {
		if server.plugin != plugin.Key {
			continue
		}

		state, override := s.pending.state(server)
		changedIn, changed := s.outside[server.Key]
		cost := 0

		for _, agent := range Agents() {
			if state == On {
				cost = max(cost, s.costIn(agent, server))
			}
		}

		contents = append(contents, Content{
			Key: server.Key, Name: server.name(), Description: "",
			Kind: MCPServer, State: state, Cost: cost, CostUnknown: s.unknown(server), Override: override,
			Unsaved: s.unsaved(server.Key), ChangedOutside: changed, ChangedIn: changedIn,
		})
	}

	return contents
}

// changedOutside reads each agent's entries again and imports the ones that
// changed since the last read, reporting whether any did.
func (s *Session) changedOutside() (bool, error) {
	now := map[Agent]map[string]State{}

	for _, agent := range Agents() {
		states, err := agent.config().read(s.machine, s.project, s.applied[agent])
		if err != nil {
			return false, err
		}

		now[agent] = states
	}

	changed := false
	for _, agent := range Agents() {
		changed = s.take(agent, now[agent]) || changed
	}

	return changed, nil
}

// writeAgents writes the entries of chosen into each agent's config.
func (s *Session) writeAgents(chosen choice) error {
	for _, agent := range Agents() {
		err := agent.config().write(s.machine, s.project, s.applied[agent], chosen.entries(agent))
		if err != nil {
			// A file written before the failure holds equip's own entries,
			// which the next save must not read as changed outside.
			for _, agent := range Agents() {
				landed, readErr := agent.config().read(s.machine, s.project, s.applied[agent])
				if readErr == nil {
					s.disk[agent] = landed
				}
			}

			return err
		}
	}

	for _, agent := range Agents() {
		s.disk[agent] = chosen.entries(agent)
	}
	// The write removed the dead Codex entries.
	s.codex = readCodexConfig(s.machine, s.project)

	return nil
}

// costIn estimates the tokens ext puts into agent's sessions.
func (s *Session) costIn(agent Agent, ext Extension) int {
	if !ext.has(agent) || s.pending.stateIn(agent, ext) != On {
		return 0
	}

	if ext.Kind == MCPServer {
		var cfg serverConfig

		measured, ok := s.measurement(ext.Key)
		if !ok {
			return 0
		}

		_ = json.Unmarshal(ext.config[ClaudeCode], &cfg)

		return measured.cost(agent, ext.name(), cfg.AlwaysLoad || !claudeToolSearch(s.machine))
	}

	cost := ext.cost[agent]
	// A plugin costs its MCP servers too.
	for _, server := range s.exts {
		if server.plugin == ext.Key {
			cost += s.costIn(agent, server)
		}
	}

	return cost
}

// ext returns the extension with key, reporting whether it is installed.
func (s *Session) ext(key string) (Extension, bool) { return s.pending.ext(key) }

// take takes now, agent's entries on disk, and imports each entry that
// changed since the last read and differs from the record as an unsaved
// Override. While an active preset changed since the last save, it imports
// none, so the preset change shows as unsaved states. It reports whether any
// entry changed.
func (s *Session) take(agent Agent, now map[string]State) bool {
	changed, presetChanged := false, len(s.presetsChanged()) > 0
	recorded := s.lastSave().entries(agent)

	for _, e := range s.applied[agent] {
		key := e.Key
		if !differ(now, s.disk[agent], key) {
			continue
		}

		changed = true
		state, set := now[key]
		imported := set && differ(now, recorded, key) && !presetChanged
		// A pending toggle the change replaces was changed outside too.
		if _, was := s.outside[key]; imported || differ(s.pending.overrides, s.saved, key) && !was {
			s.outside[key] = agent
		} else {
			delete(s.outside, key)
		}

		if !imported {
			// Missing or as recorded: the row shows the record's state.
			state, set = s.saved[key]
		}

		if set {
			s.pending.overrides[key] = state
		} else {
			delete(s.pending.overrides, key)
		}
	}

	s.disk[agent] = now

	return changed
}

// presetsChanged are the ids of the active presets that changed since the
// last save, whose members hash differs from the record's, or are missing.
func (s *Session) presetsChanged() []string {
	var changed []string

	for i, now := range s.lastSave().record() {
		if s.presetIndex(now.ID) < 0 || now != s.recorded[i] {
			changed = append(changed, now.ID)
		}
	}

	return changed
}

// presetNote notes each active preset that changed since the last save, and
// each one missing, noted by its id, as the record keeps no name. It is empty
// with none.
func (s *Session) presetNote() string {
	changed := s.presetsChanged()
	notes := make([]string, 0, len(changed))

	for _, id := range changed {
		if at := s.presetIndex(id); at >= 0 {
			notes = append(notes, "preset "+s.pending.library[at].Name+" changed outside equip")
		} else {
			notes = append(notes, "preset "+id+" missing")
		}
	}

	return strings.Join(notes, ", ")
}

// missing are the presets with ids the library does not have, sorted.
func (s *Session) missing(ids []string) []string {
	return slices.DeleteFunc(slices.Compact(slices.Sorted(slices.Values(ids))), func(id string) bool {
		return s.presetIndex(id) >= 0
	})
}

// unsavedCount counts the pending changes a save would write.
func (s *Session) unsavedCount() int {
	keys := maps.Clone(s.saved)
	maps.Copy(keys, s.pending.overrides)

	for _, agent := range Agents() {
		maps.Copy(keys, s.disk[agent])
		maps.Copy(keys, s.pending.entries(agent))
	}
	// A save removes each dead Codex entry, and records a change of presets.
	count := len(s.codex.dead())
	if !slices.Equal(s.pending.active, ids(s.recorded)) {
		count++
	}

	for key := range keys {
		if s.unsaved(key) {
			count++
		}
	}

	return count
}

// inRow reports whether has holds for a key in the row of ext: its own or,
// for a plugin, one of its MCP servers'.
func (s *Session) inRow(ext Extension, has func(key string) bool) bool {
	return has(ext.Key) || slices.ContainsFunc(s.exts, func(e Extension) bool {
		return e.plugin == ext.Key && has(e.Key)
	})
}

// overridden reports whether key has an Override.
func (s *Session) overridden(key string) bool {
	_, ok := s.pending.overrides[key]

	return ok
}

// unsaved reports whether a save would change the Override for key or its
// entry on disk in an agent.
func (s *Session) unsaved(key string) bool {
	return differ(s.pending.overrides, s.saved, key) || slices.ContainsFunc(Agents(), func(agent Agent) bool {
		return differ(s.pending.entries(agent), s.disk[agent], key)
	})
}

// differ reports whether a and b hold different states for key.
func differ(a, b map[string]State, key string) bool {
	st, ok := a[key]
	was, wasOK := b[key]

	return ok != wasOK || st != was
}
