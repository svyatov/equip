package equip

import (
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
)

// ErrChangedSinceOpen is the error of a save that found owned entries
// changed after equip read them. The save wrote nothing.
var ErrChangedSinceOpen = errors.New("changed outside equip since open")

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
	overrides  map[string]State // pending, by extension key
	saved      map[string]State // the overrides at the last save
	disk       map[string]State // Claude Code's entries for claudeExts, as last read or written
	outside    map[string]bool  // changed outside equip since the last save
	machine    Machine
	project    Project
	exts       []Extension
	claudeExts []Extension // the exts Claude Code has
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
	Name     string
	Cost     int // estimated tokens: the higher of the agents' costs
	State    State
	Fallback State // the state without the Override
	Override bool  // State was set by hand in this Project
	Unsaved  bool
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

	exts, err := discover(machine, project, dir)
	if err != nil {
		return nil, err
	}

	claudeExts := slices.DeleteFunc(slices.Clone(exts), func(e Extension) bool { return !e.has(ClaudeCode) })
	// ponytail: broken settings read as no entries here; Save reports them.
	disk, _ := readClaude(project, claudeExts)

	saved, err := readRecord(machine, project, disk)
	if err != nil {
		return nil, err
	}

	session := &Session{
		machine:    machine,
		project:    project,
		exts:       exts,
		claudeExts: claudeExts,
		overrides:  maps.Clone(saved),
		saved:      saved,
		disk:       map[string]State{},
		outside:    map[string]bool{},
	}
	session.take(disk)

	return session, nil
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

	for _, ext := range s.exts {
		state, override := s.state(ext)
		cost := 0

		for _, agent := range Agents() {
			totals[agent] += ext.costIn(agent, state)
			cost = max(cost, ext.costIn(agent, state))
		}

		rows = append(rows, Row{
			Name:           ext.Key,
			Cost:           cost,
			State:          state,
			Override:       override,
			Fallback:       ext.fallback,
			Unsaved:        s.unsaved(ext.Key),
			ChangedOutside: s.outside[ext.Key],
		})
	}

	for agent, total := range totals {
		if total > 0 {
			totals[agent] += agent.listing().introTokens
		}
	}

	return View{Project: s.project, Rows: rows, Unsaved: s.unsavedCount(), Totals: totals}
}

// costIn estimates the tokens the extension puts into agent's sessions in st.
// Codex cannot apply a skill's state, so there a skill keeps its full cost.
func (e Extension) costIn(agent Agent, st State) int {
	if agent == ClaudeCode && st != On {
		return 0
	}

	return e.cost[agent]
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
}

// Content is one extension inside a plugin. It follows its plugin and is
// never an Override.
type Content struct {
	Name        string
	Description string
	Kind        Kind
	State       State
	Cost        int // estimated tokens in Claude Code
}

// Detail returns the detail of the extension with key.
func (s *Session) Detail(key string) Detail {
	ext, ok := s.ext(key)
	if !ok {
		return Detail{
			Description: "", Marketplace: "", Agents: nil, Locations: nil, NotApplied: nil, Costs: nil, States: nil,
			Contents: nil, Hooks: false,
		}
	}

	detail := Detail{
		Description: ext.Description, Marketplace: marketplaceOf(ext.Key), Agents: nil, Locations: ext.Locations,
		NotApplied: map[Agent]string{}, Costs: map[Agent]int{}, States: ext.Kind.claude().states,
		Contents: nil, Hooks: ext.hooks,
	}
	state, _ := s.state(ext)

	for _, content := range ext.contents {
		content.State = state
		if state != On {
			content.Cost = 0
		}

		detail.Contents = append(detail.Contents, content)
	}

	for _, loc := range ext.Locations {
		if !slices.Contains(detail.Agents, loc.Agent) {
			detail.Agents = append(detail.Agents, loc.Agent)
			detail.Costs[loc.Agent] = ext.costIn(loc.Agent, state)
		}
	}

	if ext.has(Codex) {
		detail.NotApplied[Codex] = "Codex has no per-project skill setting"
	}

	return detail
}

// DropOverride removes the Override for key, so the extension falls back.
func (s *Session) DropOverride(key string) { delete(s.overrides, key) }

// Save writes the pending Overrides into Claude Code's settings. If Claude
// Code's entries changed since they were read, it writes nothing, imports the
// changes and returns ErrChangedSinceOpen.
func (s *Session) Save() error {
	now, err := readClaude(s.project, s.claudeExts)
	if err != nil {
		return err
	}

	if s.take(now) {
		return ErrChangedSinceOpen
	}
	// Nothing to write. With saved Overrides, a save still writes them, so
	// the record of a first open's imports gets created.
	if s.unsavedCount() == 0 && len(s.saved) == 0 {
		return nil
	}

	err = writeClaude(s.machine, s.project, s.claudeExts, s.overrides)
	if err != nil {
		return err
	}
	// Set before the record write, so a failed one does not make equip's own
	// entries look changed outside.
	s.disk = map[string]State{}
	for _, e := range s.claudeExts {
		if st, ok := s.overrides[e.Key]; ok {
			s.disk[e.Key] = st
		}
	}

	err = writeRecord(s.machine, s.project, s.overrides)
	if err != nil {
		return err
	}

	s.saved = maps.Clone(s.overrides)
	clear(s.outside)

	return nil
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
	if !override {
		// With no presets, an extension falls back to the agent's default.
		state = ext.fallback
	}

	return state, override
}

// take takes now, Claude Code's entries on disk, and imports each entry that
// changed since the last read and differs from the record as an unsaved
// Override. It reports whether any entry changed.
func (s *Session) take(now map[string]State) bool {
	changed := false

	for _, e := range s.claudeExts {
		key := e.Key
		if !differ(now, s.disk, key) {
			continue
		}

		changed = true
		state, set := now[key]
		imported := set && differ(now, s.saved, key)
		// A pending toggle the change replaces was changed outside too.
		s.outside[key] = imported || differ(s.overrides, s.saved, key) && !s.outside[key]
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

	s.disk = now

	return changed
}

// unsavedCount counts the pending changes a save would write.
func (s *Session) unsavedCount() int {
	keys := maps.Clone(s.saved)
	maps.Copy(keys, s.overrides)
	maps.Copy(keys, s.disk)

	count := 0

	for key := range keys {
		if s.unsaved(key) {
			count++
		}
	}

	return count
}

// unsaved reports whether a save would change the Override for key or, for
// an extension Claude Code has, its entry on disk.
func (s *Session) unsaved(key string) bool {
	inClaude := slices.ContainsFunc(s.claudeExts, func(e Extension) bool { return e.Key == key })

	return differ(s.overrides, s.saved, key) || inClaude && differ(s.overrides, s.disk, key)
}

// differ reports whether a and b hold different states for key.
func differ(a, b map[string]State, key string) bool {
	st, ok := a[key]
	was, wasOK := b[key]

	return ok != wasOK || st != was
}
