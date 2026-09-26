package equip

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sync"
)

// ErrChangedSinceOpen is the error of a save that found owned entries
// changed after equip read them. The save wrote nothing.
var ErrChangedSinceOpen = errors.New("changed outside equip since open")

// ErrCannotProbe is the error of a probe of an extension equip does not
// start: one that is not an MCP server an agent has on and trusted.
var ErrCannotProbe = errors.New("equip measures only an MCP server an agent has on and trusted")

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
	overrides map[string]State           // pending, by extension key
	saved     map[string]State           // the overrides at the last save
	disk      map[Agent]map[string]State // each agent's entries for its applied exts, as last read or written
	outside   map[string]Agent           // changed outside equip since the last save, in that agent
	applied   map[Agent][]Extension      // the exts whose states equip writes for each agent
	measured  map[string]measurement     // the MCP servers measured, by key; guarded by mu
	codex     codexConfig
	machine   Machine
	project   Project
	exts      []Extension
	mu        sync.Mutex
}

// View is what the user sees of a Session.
type View struct {
	Project Project
	Totals  map[Agent]int // the estimated tokens of a session in each agent
	Rows    []Row
	Unsaved int // pending changes a save would write
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
	all := map[string]State{}

	for _, agent := range Agents() {
		// ponytail: broken config reads as no entries here; Save reports it.
		disk[agent], _ = agent.config().read(machine, project, applied[agent])
		// Where the agents disagree, the first agent's state is the record's,
		// and the other's shows as changed outside.
		for key, st := range disk[agent] {
			if _, ok := all[key]; !ok {
				all[key] = st
			}
		}
	}

	saved, err := readRecord(machine, project, all)
	if err != nil {
		return nil, err
	}

	session := &Session{
		machine:   machine,
		project:   project,
		exts:      exts,
		applied:   applied,
		codex:     codex,
		overrides: maps.Clone(saved),
		saved:     saved,
		disk:      map[Agent]map[string]State{},
		outside:   map[string]Agent{},
		measured:  map[string]measurement{},
		mu:        sync.Mutex{},
	}

	for _, agent := range Agents() {
		session.take(agent, disk[agent])
	}

	session.readMeasurements()

	return session, nil
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
	if ext, ok := s.ext(key); ok && !slices.Contains(ext.Kind.claude().states, st) {
		return
	}

	s.overrides[key] = st
}

// View returns the current view.
func (s *Session) View() View {
	rows := make([]Row, 0, len(s.exts))
	totals := map[Agent]int{}
	codexPlugins := false

	for _, ext := range s.exts {
		// A plugin's MCP server shows among the plugin's contents.
		if ext.plugin != "" {
			continue
		}

		state, override := s.state(ext)
		cost := 0

		for _, agent := range Agents() {
			totals[agent] += s.costIn(agent, ext)
			cost = max(cost, s.costIn(agent, ext))
		}

		codexPlugins = codexPlugins || ext.Kind == Plugin && ext.has(Codex) && s.stateIn(Codex, ext) == On

		_, changed := s.outside[ext.Key]
		rows = append(rows, Row{
			Key:            ext.Key,
			Name:           ext.name(),
			Kind:           ext.Kind,
			Cost:           cost,
			CostUnknown:    s.unknown(ext),
			State:          state,
			Override:       override,
			Fallback:       ext.fallback[ext.primary()],
			Unsaved:        s.rowUnsaved(ext),
			ChangedOutside: changed,
		})
	}

	for agent, total := range totals {
		if total > 0 {
			totals[agent] += agent.listing().introTokens
		}
	}

	if codexPlugins {
		totals[Codex] += tokens(Codex, codexPluginsBlockBytes)
	}

	return View{Project: s.project, Rows: rows, Unsaved: s.unsavedCount(), Totals: totals}
}

// Detail is what the detail pane shows of one extension.
type Detail struct {
	NotApplied  map[Agent]string // why an agent that has it does not get its state
	Costs       map[Agent]int    // estimated tokens in each agent that has it
	Description string
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
			Description: "", Marketplace: "", Agents: nil, Locations: nil, NotApplied: nil, Costs: nil, States: nil,
			Contents: nil, Hooks: false, BuiltIn: false, ChangedIn: ClaudeCode,
		}
	}

	detail := Detail{
		Description: ext.Description, Marketplace: marketplaceOf(ext.Key), Agents: nil, Locations: ext.Locations,
		NotApplied: map[Agent]string{}, Costs: map[Agent]int{}, States: ext.Kind.claude().states,
		Contents: s.contents(ext), Hooks: ext.hooks, BuiltIn: ext.builtIn, ChangedIn: s.outside[key],
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
		if !ok || !s.enabled(agent, ext) {
			continue
		}

		return func() error {
			measured, err := probe(context.Background(), s.machine.Env, cfg)
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

// DropOverride removes the Override for key, so the extension falls back.
func (s *Session) DropOverride(key string) { delete(s.overrides, key) }

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
	// the record of a first open's imports gets created.
	if s.unsavedCount() == 0 && len(s.saved) == 0 {
		return nil
	}
	// Written before the record, so a failed record write does not make
	// equip's own entries look changed outside.
	err = s.writeAgents()
	if err != nil {
		return err
	}

	err = writeRecord(s.machine, s.project, s.overrides)
	if err != nil {
		return err
	}

	s.saved = maps.Clone(s.overrides)
	clear(s.outside)

	return nil
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
	if cfg.Command != "" {
		cfg.Dir = cmp.Or(cfg.Dir, s.project.checkout)
	}

	return cfg, err == nil && cfg.Type != "sse" && (cfg.Command != "" || cfg.URL != "")
}

// enabled reports whether agent has ext on and trusts it, as its config on
// disk says, so pending changes do not count.
func (s *Session) enabled(agent Agent, ext Extension) bool {
	state, onDisk := s.disk[agent][ext.Key]
	// Claude Code trusts a .mcp.json server only once the user approves it.
	// ponytail: approval by enableAllProjectMcpServers does not count; read
	// it if users approve that way.
	if !onDisk && agent == ClaudeCode && ext.lists.settings {
		return false
	}

	if !onDisk {
		state = ext.fallback[agent]
	}
	// A plugin's MCP server loads only while its plugin is on.
	plugin, inPlugin := s.ext(ext.plugin)

	return state == On && (!inPlugin || s.enabled(agent, plugin))
}

// unknown reports whether the cost of ext is unknown: an MCP server's, until
// it is measured.
func (s *Session) unknown(ext Extension) bool {
	_, measured := s.measurement(ext.Key)

	return ext.Kind == MCPServer && !measured
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

	state, _ := s.state(plugin)

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

		state, override := s.state(server)
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

// writeAgents writes the pending Overrides into each agent's config.
func (s *Session) writeAgents() error {
	for _, agent := range Agents() {
		err := agent.config().write(s.machine, s.project, s.applied[agent], s.overrides)
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
		s.disk[agent] = s.entries(agent)
	}
	// The write removed the dead Codex entries.
	s.codex = readCodexConfig(s.machine, s.project)

	return nil
}

// costIn estimates the tokens ext puts into agent's sessions.
func (s *Session) costIn(agent Agent, ext Extension) int {
	if !ext.has(agent) || s.stateIn(agent, ext) != On {
		return 0
	}

	if ext.Kind == MCPServer {
		var cfg serverConfig

		measured, _ := s.measurement(ext.Key)
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

// stateIn is the state ext has in agent's sessions: its Override where equip
// writes it for agent, else agent's own default. So Codex keeps a skill on.
func (s *Session) stateIn(agent Agent, ext Extension) State {
	if st, ok := s.overrides[ext.Key]; ok && s.applies(agent, ext) {
		return st
	}

	return ext.fallback[agent]
}

// applies reports whether equip writes the state of ext for agent.
func (s *Session) applies(agent Agent, ext Extension) bool {
	return slices.ContainsFunc(s.applied[agent], func(e Extension) bool { return e.Key == ext.Key })
}

// ext returns the extension with key, reporting whether it is installed.
func (s *Session) ext(key string) (Extension, bool) {
	var ext Extension

	i := slices.IndexFunc(s.exts, func(e Extension) bool { return e.Key == key })
	if i >= 0 {
		ext = s.exts[i]
	}

	return ext, i >= 0
}

// state returns the state of ext and whether an Override set it.
func (s *Session) state(ext Extension) (State, bool) {
	state, override := s.overrides[ext.Key]
	// A plugin's MCP server loads only while its plugin is on.
	if plugin, ok := s.ext(ext.plugin); ok {
		if pluginState, _ := s.state(plugin); pluginState != On {
			return pluginState, override
		}
	}

	if !override {
		// With no presets, an extension falls back to the agent's default.
		state = ext.fallback[ext.primary()]
	}

	return state, override
}

// take takes now, agent's entries on disk, and imports each entry that
// changed since the last read and differs from the record as an unsaved
// Override. It reports whether any entry changed.
func (s *Session) take(agent Agent, now map[string]State) bool {
	changed := false

	for _, e := range s.applied[agent] {
		key := e.Key
		if !differ(now, s.disk[agent], key) {
			continue
		}

		changed = true
		state, set := now[key]
		imported := set && differ(now, s.saved, key)
		// A pending toggle the change replaces was changed outside too.
		if _, was := s.outside[key]; imported || differ(s.overrides, s.saved, key) && !was {
			s.outside[key] = agent
		} else {
			delete(s.outside, key)
		}

		if !imported {
			// Missing or as recorded: the row shows the record's state.
			state, set = s.saved[key]
		}

		if set {
			s.overrides[key] = state
		} else {
			delete(s.overrides, key)
		}
	}

	s.disk[agent] = now

	return changed
}

// unsavedCount counts the pending changes a save would write.
func (s *Session) unsavedCount() int {
	keys := maps.Clone(s.saved)
	maps.Copy(keys, s.overrides)

	for _, disk := range s.disk {
		maps.Copy(keys, disk)
	}
	// A save removes each dead Codex entry.
	count := len(s.codex.dead())

	for key := range keys {
		if s.unsaved(key) {
			count++
		}
	}

	return count
}

// rowUnsaved reports whether the row of ext has an unsaved change: its own or,
// for a plugin, one of its MCP servers'.
func (s *Session) rowUnsaved(ext Extension) bool {
	return s.unsaved(ext.Key) || slices.ContainsFunc(s.exts, func(e Extension) bool {
		return e.plugin == ext.Key && s.unsaved(e.Key)
	})
}

// unsaved reports whether a save would change the Override for key or its
// entry on disk in an agent.
func (s *Session) unsaved(key string) bool {
	return differ(s.overrides, s.saved, key) || slices.ContainsFunc(Agents(), func(agent Agent) bool {
		return differ(s.entries(agent), s.disk[agent], key)
	})
}

// entries are the entries a save leaves in agent's config for the pending
// Overrides.
func (s *Session) entries(agent Agent) map[string]State {
	entries := map[string]State{}

	for _, e := range s.applied[agent] {
		if st, ok := s.overrides[e.Key]; ok && e.entry(agent, st) {
			entries[e.Key] = st
		}
	}

	return entries
}

// differ reports whether a and b hold different states for key.
func differ(a, b map[string]State, key string) bool {
	st, ok := a[key]
	was, wasOK := b[key]

	return ok != wasOK || st != was
}
