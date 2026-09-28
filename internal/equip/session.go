package equip

import (
	"cmp"
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

// ErrNotOrphan is the error of an adoption of a record the Project was not
// offered.
var ErrNotOrphan = errors.New("not a record this project can adopt")

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
	pending    choice                     // the pending active presets and Overrides, with the installed exts
	saved      map[string]State           // the overrides at the last save
	disk       map[Agent]map[string]State // each agent's entries for its applied exts, as last read or written
	outside    map[string]Agent           // changed outside equip since the last save, in that agent
	measured   map[string]measurement     // the MCP servers measured, by key; guarded by mu
	refused    map[string]string          // why each MCP server that refused equip for good did, by key; guarded by mu
	codex      codexConfig
	notApplied map[Agent]string // why equip does not write each agent's config; empty for one it writes
	discovered map[string]State // the Claude Code defaults of the exts kept in settings.local.json, as discovered
	machine    Machine
	project    Project
	orphans    []string // the paths of the records the Project can adopt
	draft      *Preset  // the one preset with unwritten edits, as edited; nil with none
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
	Totals  map[Agent]Total // the total of a session in each agent
	Rows    []Row
	Facets  []Facet // the ways to narrow Rows, in the order the sidebar shows them
	// Orphans are the paths of the records the Project can adopt on its first
	// open: of a repo with its root commit whose path no longer exists.
	Orphans []string
	Presets []string // the names of the active presets, pending
	Unsaved int      // pending changes a save would write
}

// Total is the estimated tokens of a session in an agent.
type Total struct {
	Tokens int
	// Unmeasured counts the MCP servers that are on but not measured yet,
	// which the total leaves out.
	Unmeasured int
	// OverBudget marks a skill listing that passes the agent's listing
	// budget, so the agent shortens or drops what it lists.
	OverBudget bool
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
	ByName      bool  // a By-name skill, or a plugin of only such skills
	Override    bool  // State was set by hand in this Project
	Unsaved     bool
	// NotApplied reports that no agent that has the extension gets its
	// state, each for the reason Detail's NotApplied gives.
	NotApplied bool
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

	library, err := readPresets(machine)
	if err != nil {
		return nil, err
	}

	session := &Session{
		machine:    machine,
		project:    project,
		pending:    choice{library: library, active: nil, overrides: nil, exts: exts, applied: nil},
		draft:      nil,
		unfinished: nil,
		recorded:   nil,
		codex:      codex,
		notApplied: nil,
		discovered: claudeDefaults(exts),
		saved:      nil,
		disk:       nil,
		outside:    nil,
		measured:   map[string]measurement{},
		refused:    map[string]string{},
		approvals:  approvalsCount(machine, project),
		orphans:    orphans(machine, project),
		mu:         sync.Mutex{},
	}
	session.takeConfigs(codex)

	disk := map[Agent]map[string]State{}

	for _, agent := range Agents() {
		// ponytail: broken config reads as no entries here; Save reports it.
		disk[agent], _ = agent.config().read(machine, project, session.pending.applied[agent])
	}

	saved, presets, err := readRecord(recordPath(machine, project.Path), firstStates(disk))
	if err != nil {
		return nil, err
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

// SetState makes state an Override for the extension with key, unless the
// extension does not offer state.
func (s *Session) SetState(key string, state State) {
	ext, ok := s.pending.ext(key)
	if ok && !slices.Contains(ext.Kind.states(), state) {
		return
	}

	s.pending.overrides[key] = state
}

// View returns the current view.
func (s *Session) View() View {
	rows := make([]Row, 0, len(s.pending.exts))

	for _, ext := range s.pending.exts {
		// A plugin's contents show in its detail.
		if ext.plugin == "" {
			rows = append(rows, s.row(ext))
		}
	}

	slices.SortStableFunc(rows, func(a, b Row) int { return cmp.Compare(a.Name, b.Name) })

	totals := map[Agent]Total{}

	for _, agent := range Agents() {
		totals[agent] = s.total(agent)
	}

	var presets []string

	for _, preset := range s.pending.library {
		if slices.Contains(s.pending.active, preset.ID) {
			presets = append(presets, preset.Name)
		}
	}

	for _, id := range s.missing(s.pending.active) {
		presets = append(presets, s.missingName(id))
	}

	return View{
		Project: s.project, Rows: rows, Facets: s.facets(rows), Unsaved: s.unsavedCount(), Totals: totals,
		Orphans: s.orphans, Presets: presets,
	}
}

// Detail is what the detail pane shows of one extension.
type Detail struct {
	NotApplied  map[Agent]string // why an agent that has it does not get its state
	Costs       map[Agent]int    // estimated tokens in each agent that has it
	Description string
	// Note says why the row differs from the agent config while an active
	// preset changed outside equip or is missing.
	Note        string
	Marketplace string // a plugin's
	// Unmeasurable says why equip does not measure an MCP server; empty for
	// one it does, and for every other extension.
	Unmeasurable string
	Agents       []Agent // the agents that have it
	Locations    []Location
	States       []State   // the states the user can pick
	Contents     []Content // a plugin's skills
	Hooks        bool      // a plugin has hooks, whose output adds an unknown cost
	BuiltIn      bool      // built into Claude Code, so it has no Locations
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
	// Unmeasurable says why equip does not measure an MCP server; empty for
	// one it does, and for a skill.
	Unmeasurable string
	Kind         Kind
	State        State
	Cost         int  // estimated tokens: the higher of the agents' costs
	CostUnknown  bool // an MCP server not measured yet
	ByName       bool // a By-name skill
	Override     bool // an MCP server's State was set by hand in this Project
	Unsaved      bool // a save would change an MCP server's Override or entries
	// ChangedOutside reports that an MCP server changed through an edit
	// outside equip, in the agent ChangedIn.
	ChangedOutside bool
	ChangedIn      Agent
}

// Detail returns the detail of the extension with key.
func (s *Session) Detail(key string) Detail {
	ext, ok := s.pending.ext(key)
	if !ok {
		return Detail{
			Description: "", Note: "", Marketplace: "", Agents: nil, Locations: nil, NotApplied: nil, Costs: nil,
			States: nil, Contents: nil, Hooks: false, BuiltIn: false, ChangedIn: ClaudeCode, Unmeasurable: "",
		}
	}

	detail := Detail{
		Description: ext.Description, Note: "", Marketplace: marketplaceOf(ext.Key), Agents: nil,
		Locations: ext.Locations, NotApplied: map[Agent]string{}, Costs: map[Agent]int{}, States: ext.Kind.states(),
		Contents: s.contents(ext), Hooks: ext.hooks, BuiltIn: ext.builtIn, ChangedIn: s.outside[key],
		Unmeasurable: s.unmeasurable(ext),
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

	detail.NotApplied = s.notAppliedIn(ext)

	return detail
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

// Saved is what a save wrote or, when it returns ErrChangedSinceOpen, what
// its import of the outside changes did to the pending Overrides.
type Saved struct {
	// Reason is why no agent applies the NotApplied changes, when they share
	// one reason; empty when they do not.
	Reason     string
	Changes    int  // the pending changes it wrote
	NotApplied int  // of those, the changes of extensions no agent applies
	Replaced   int  // the unsaved changes the import replaced
	Updated    bool // whether the import changed any pending Override
}

// Save writes the pending Overrides into each agent's config. If an agent's
// entries changed since they were read, it writes nothing, imports the
// changes and returns ErrChangedSinceOpen. It counts what it writes after it
// reads the configs again, so the count sees a config that changed since.
func (s *Session) Save() (Saved, error) {
	saved := Saved{Reason: "", Changes: 0, NotApplied: 0, Replaced: 0, Updated: false}
	before := maps.Clone(s.pending.overrides)

	changed, err := s.changedOutside()
	if err != nil {
		return saved, err
	}

	if changed {
		saved.Replaced, saved.Updated = s.replaced(before), !maps.Equal(before, s.pending.overrides)

		return saved, ErrChangedSinceOpen
	}

	saved.Changes = s.unsavedCount()
	saved.NotApplied, saved.Reason = s.unsavedNotApplied()
	// Nothing to write. With saved Overrides, a save still writes them, so
	// the record of a first open's imports gets created. With records on
	// offer, it creates an empty record, so the next open offers them no more.
	nothing := saved.Changes == 0 && len(s.saved) == 0
	if nothing {
		// As a write would, every save keeps the settings file out of git.
		err = excludeSettings(s.machine, s.project)
	} else {
		// Written before the record, so a failed record write does not make
		// equip's own entries look changed outside.
		err = s.writeAgents(s.pending)
	}

	if err != nil || nothing && len(s.orphans) == 0 {
		return saved, err
	}

	presets := s.pending.record(s.recorded)

	err = writeRecord(s.machine, s.project, s.pending.overrides, presets)
	if err != nil {
		return saved, err
	}

	s.saved, s.recorded = maps.Clone(s.pending.overrides), presets
	s.orphans = nil
	clear(s.outside)

	return saved, nil
}

// unsavedNotApplied counts the pending changes of extensions no agent gets
// the state of, which a save records but applies nowhere, and gives the
// reason they share, empty when they have more than one.
func (s *Session) unsavedNotApplied() (int, string) {
	count, reasons := 0, map[string]bool{}

	for _, key := range s.unsavedKeys() {
		// An extension not installed has no agent to apply it yet.
		ext, ok := s.pending.ext(key)
		if !ok || !s.isNotApplied(ext) {
			continue
		}

		count++

		for _, why := range s.notAppliedIn(ext) {
			reasons[why] = true
		}
	}

	if len(reasons) != 1 {
		return count, ""
	}

	return count, slices.Collect(maps.Keys(reasons))[0]
}

// row is the row of ext in the list.
func (s *Session) row(ext Extension) Row {
	state, override := s.pending.state(ext)
	_, changed := s.outside[ext.Key]

	return Row{
		Key:            ext.Key,
		Name:           ext.name(),
		Kind:           ext.Kind,
		Cost:           s.cost(ext),
		CostUnknown:    s.unknown(ext),
		ByName:         s.isByName(ext),
		State:          state,
		Override:       override,
		Fallback:       s.pending.base(ext.primary(), ext),
		Unsaved:        s.inRow(ext, s.unsaved),
		NotApplied:     s.isNotApplied(ext),
		ChangedOutside: changed,
	}
}

// isNotApplied reports whether no agent that has ext gets its state.
func (s *Session) isNotApplied(ext Extension) bool {
	return !slices.ContainsFunc(Agents(), func(agent Agent) bool { return s.applies(agent, ext) })
}

// isByName reports whether ext is a By-name skill, or a plugin of skills that
// are all By-name skills and nothing else that costs tokens.
func (s *Session) isByName(ext Extension) bool {
	skills := ext.Kind == Skill || slices.ContainsFunc(ext.contents, func(c Content) bool { return c.Kind == Skill })

	costs := slices.ContainsFunc(Agents(), func(agent Agent) bool { return ext.has(agent) && ext.cost[agent] > 0 })

	return skills && !costs && !slices.ContainsFunc(s.pending.exts, func(e Extension) bool { return e.plugin == ext.Key })
}

// start takes saved and presets, the Overrides and active presets of the
// record, and disk, each agent's entries, as an open does.
func (s *Session) start(saved map[string]State, presets []recordPreset, disk map[Agent]map[string]State) {
	s.saved, s.pending.overrides = saved, maps.Clone(saved)
	s.recorded, s.pending.active = presets, ids(presets)
	s.disk, s.outside = map[Agent]map[string]State{}, map[string]Agent{}

	for _, agent := range Agents() {
		s.importEntries(agent, disk[agent])
	}
}

// lastSave is the choice of the last save, with the library as it is now.
func (s *Session) lastSave() choice {
	saved := s.pending
	saved.overrides, saved.active = s.saved, ids(s.recorded)

	return saved
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

	for _, server := range s.pending.exts {
		if server.plugin != plugin.Key {
			continue
		}

		state, override := s.pending.state(server)
		changedIn, changed := s.outside[server.Key]

		cost := 0
		if state == On {
			cost = s.cost(server)
		}

		contents = append(contents, Content{
			Key: server.Key, Name: server.name(), Description: "",
			Kind: MCPServer, State: state, Cost: cost, CostUnknown: s.unknown(server), ByName: false, Override: override,
			Unsaved: s.unsaved(server.Key), ChangedOutside: changed, ChangedIn: changedIn,
			Unmeasurable: s.unmeasurable(server),
		})
	}

	return contents
}

// takeConfigs takes codex, Codex's config, and reads again whether git tracks
// Claude Code's settings.local.json, then which exts equip writes for each
// agent. Either config may have become tracked or untracked since the last
// read.
func (s *Session) takeConfigs(codex codexConfig) {
	s.codex = codex
	s.notApplied = map[Agent]string{
		ClaudeCode: trackedReason(s.machine, s.project, s.project.Path, settingsRel),
		Codex:      codex.notApplied,
	}
	takeClaudeDefaults(s.machine, s.project, s.pending.exts, s.discovered, s.notApplied[ClaudeCode] != "")

	s.pending.applied = map[Agent][]Extension{}

	for _, agent := range Agents() {
		for _, ext := range s.pending.exts {
			if s.applies(agent, ext) {
				s.pending.applied[agent] = append(s.pending.applied[agent], ext)
			}
		}
	}
}

// applies reports whether agent has ext and equip writes its state there.
func (s *Session) applies(agent Agent, ext Extension) bool {
	return ext.has(agent) && s.whyNotApplied(agent, ext) == ""
}

// notAppliedIn is why each agent that has ext does not get its state, by
// agent; it leaves out each agent that does.
func (s *Session) notAppliedIn(ext Extension) map[Agent]string {
	reasons := map[Agent]string{}

	for _, agent := range Agents() {
		if why := s.whyNotApplied(agent, ext); ext.has(agent) && why != "" {
			reasons[agent] = why
		}
	}

	return reasons
}

// whyNotApplied is why equip does not write the state of ext for agent. It is
// empty when equip does.
func (s *Session) whyNotApplied(agent Agent, ext Extension) string {
	switch {
	case agent == ClaudeCode && !ext.inSettings():
		return "" // kept in ~/.claude.json, which equip always writes
	case agent == Codex && ext.Kind == Skill:
		return "Codex has no per-project skill setting"
	}

	return s.notApplied[agent]
}

// changedOutside reads each agent's config and entries again and imports the
// entries that changed since the last read, reporting whether any did.
func (s *Session) changedOutside() (bool, error) {
	s.takeConfigs(readCodexConfig(s.machine, s.project))

	now := map[Agent]map[string]State{}

	for _, agent := range Agents() {
		states, err := agent.config().read(s.machine, s.project, s.pending.applied[agent])
		if err != nil {
			return false, err
		}

		now[agent] = states
	}

	changed := false
	for _, agent := range Agents() {
		changed = s.importEntries(agent, now[agent]) || changed
	}

	return changed, nil
}

// replaced counts the unsaved changes in before, the pending Overrides of an
// earlier time, that the pending Overrides no longer hold.
func (s *Session) replaced(before map[string]State) int {
	// A dropped Override is an unsaved change too, of a key only saved.
	keys := maps.Clone(before)
	maps.Copy(keys, s.saved)

	count := 0

	for key := range keys {
		if differ(before, s.saved, key) && differ(before, s.pending.overrides, key) {
			count++
		}
	}

	return count
}

// writeAgents writes the entries of chosen into each agent's config.
func (s *Session) writeAgents(chosen choice) error {
	for _, agent := range Agents() {
		err := agent.config().write(s.machine, s.project, s.pending.applied[agent], chosen.entries(agent))
		if err != nil {
			// A file written before the failure holds equip's own entries,
			// which the next save must not read as changed outside.
			for _, agent := range Agents() {
				landed, readErr := agent.config().read(s.machine, s.project, s.pending.applied[agent])
				if readErr == nil {
					s.disk[agent] = landed
				}
			}

			return err
		}
	}

	// The write removed the dead Codex entries.
	s.codex = readCodexConfig(s.machine, s.project)

	for _, agent := range Agents() {
		s.disk[agent] = chosen.entries(agent)
	}

	return nil
}

// importEntries takes now, agent's entries on disk, and imports each entry
// that changed since the last read and differs from the record as an unsaved
// Override. While an active preset changed since the last save, it imports
// none, so the preset change shows as unsaved states. It reports whether any
// entry changed.
func (s *Session) importEntries(agent Agent, now map[string]State) bool {
	changed, presetChanged := false, len(s.presetsChanged()) > 0
	recorded := s.lastSave().entries(agent)

	for _, e := range s.pending.applied[agent] {
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

	// A rename changes only the name, and changes no member.
	for i, now := range s.lastSave().record(s.recorded) {
		if s.presetIndex(now.ID) < 0 || now.Hash != s.recorded[i].Hash {
			changed = append(changed, now.ID)
		}
	}

	return changed
}

// presetNote notes each active preset that changed since the last save, and
// each one missing. It is empty with none.
func (s *Session) presetNote() string {
	changed := s.presetsChanged()
	notes := make([]string, 0, len(changed))

	for _, id := range changed {
		if at := s.presetIndex(id); at >= 0 {
			notes = append(notes, "preset "+s.pending.library[at].Name+" changed outside equip")
		} else {
			notes = append(notes, "preset "+s.missingName(id)+" missing")
		}
	}

	return strings.Join(notes, ", ")
}

// missingName is the name of the missing preset with id: the record's, or
// its id when the record keeps none.
func (s *Session) missingName(id string) string {
	return cmp.Or(presetName(s.recorded, id), id)
}

// missing are the presets with ids the library does not have, sorted.
func (s *Session) missing(ids []string) []string {
	return slices.DeleteFunc(slices.Compact(slices.Sorted(slices.Values(ids))), func(id string) bool {
		return s.presetIndex(id) >= 0
	})
}

// unsavedCount counts the pending changes a save would write.
func (s *Session) unsavedCount() int {
	// A save removes each dead Codex entry, and records a change of presets.
	count := len(s.codex.dead()) + len(s.unsavedKeys())
	if !slices.Equal(s.pending.active, ids(s.recorded)) {
		count++
	}

	return count
}

// unsavedKeys are the keys whose Override or entries a save would change.
func (s *Session) unsavedKeys() []string {
	keys := maps.Clone(s.saved)
	maps.Copy(keys, s.pending.overrides)

	for _, agent := range Agents() {
		maps.Copy(keys, s.disk[agent])
		maps.Copy(keys, s.pending.entries(agent))
	}

	return slices.DeleteFunc(slices.Collect(maps.Keys(keys)), func(key string) bool { return !s.unsaved(key) })
}

// inRow reports whether has holds for a key in the row of ext: its own or,
// for a plugin, one of its MCP servers'.
func (s *Session) inRow(ext Extension, has func(key string) bool) bool {
	return has(ext.Key) || slices.ContainsFunc(s.pending.exts, func(e Extension) bool {
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
