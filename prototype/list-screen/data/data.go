// Package data is PROTOTYPE fake data for the equip list screen. Throwaway.
//
// Assumptions until research tickets land:
//   - Both agents support manual-only, so every skill has three states.
//   - Cost = tokens an extension puts into every session's context: a skill's
//     name+description, an MCP server's tool schemas, a plugin's contents.
//   - A plugin's skills and MCP servers each have their own state; they load
//     only while the plugin is on.
package data

import (
	"fmt"
	"hash/fnv"
	"maps"
	"slices"
	"strings"
)

type Kind int

const (
	Skill Kind = iota
	Plugin
	MCP
)

func (k Kind) String() string { return [...]string{"skill", "plugin", "mcp"}[k] }

type State int

const (
	Off State = iota
	Manual
	On
)

func (s State) String() string { return [...]string{"off", "manual-only", "on"}[s] }

// Agent is a bitmask of the agents that have an extension.
type Agent int

const (
	Claude Agent = 1 << iota
	Codex
	Both = Claude | Codex
)

func (a Agent) String() string {
	switch a {
	case Claude:
		return "claude"
	case Codex:
		return "codex"
	}
	return "both"
}

type Ext struct {
	Kind     Kind
	Name     string
	Desc     string
	Source   string // where it's installed; for plugins, the marketplace
	Agents   Agent
	Tokens   int    // context cost when on (plugins: sum of Children)
	Children []*Ext // plugin contents
	Parent   *Ext
	State    State
	Origin   string // "default", "preset go", "override"
}

// States lists the states the user can pick for e.
func (e *Ext) States() []State {
	if e.Kind == Skill {
		return []State{On, Manual, Off}
	}
	return []State{On, Off}
}

// Cycle moves e to its next state and marks it an override.
func (e *Ext) Cycle() {
	ss := e.States()
	for i, s := range ss {
		if s == e.State {
			e.Set(ss[(i+1)%len(ss)])
			return
		}
	}
	e.Set(ss[0])
}

func (e *Ext) Set(s State) {
	if s == e.State {
		return
	}
	e.State = s
	e.Origin = "override"
}

// Cost is what e adds to a session right now.
func (e *Ext) Cost() (n int) {
	if e.State != On || (e.Parent != nil && e.Parent.State != On) {
		return 0
	}
	if e.Children == nil {
		return e.Tokens
	}
	for _, c := range e.Children {
		n += c.Cost()
	}
	return n
}

// Match reports whether q (lowercase) matches e's name, description, or source.
func (e *Ext) Match(q string) bool {
	if q == "" {
		return true
	}
	return strings.Contains(strings.ToLower(e.Name+" "+e.Desc+" "+e.Source), q)
}

type Preset struct {
	Name    string
	Members map[string]bool // top-level extension names
}

func (p *Preset) Clone() *Preset { return &Preset{p.Name, maps.Clone(p.Members)} }

// Project is another project on this machine with its own record.
type Project struct {
	Path    string
	Presets []string
}

// OpenPresets asks the host to show the preset screens; ClosePresets asks it
// to return to the main screen.
type (
	OpenPresets  struct{}
	ClosePresets struct{}
)

type Store struct {
	Project  string
	Presets  []string  // active in this project
	Library  []*Preset // every preset the user has
	Projects []Project // every other project with a record
	Exts     []*Ext    // top level, in load order
	saved    map[*Ext]State
}

// PresetsOf lists the presets that have e. Plugin contents follow their plugin.
func (s *Store) PresetsOf(e *Ext) (names []string) {
	for _, p := range s.Library {
		if e.Parent == nil && p.Members[e.Name] {
			names = append(names, p.Name)
		}
	}
	return names
}

func (s *Store) Preset(name string) *Preset {
	if i := slices.IndexFunc(s.Library, func(p *Preset) bool { return p.Name == name }); i >= 0 {
		return s.Library[i]
	}
	return nil
}

// UsersOf lists the projects that use the preset, this one first.
func (s *Store) UsersOf(name string) (paths []string) {
	if slices.Contains(s.Presets, name) {
		paths = append(paths, s.Project)
	}
	for _, p := range s.Projects {
		if slices.Contains(p.Presets, name) {
			paths = append(paths, p.Path)
		}
	}
	return paths
}

// Fallback is the state e gets from the active presets, ignoring any override:
// on if an active preset has it, off if none does, and on (the agents'
// default) when no preset is active. Plugin contents default to on.
func (s *Store) Fallback(e *Ext) (State, string) {
	if e.Parent != nil || len(s.Presets) == 0 {
		return On, "default"
	}
	var in []string
	for _, name := range s.Presets {
		if p := s.Preset(name); p != nil && p.Members[e.Name] {
			in = append(in, name)
		}
	}
	if len(in) == 0 {
		return Off, "default"
	}
	return On, "preset " + strings.Join(in, ", ")
}

func (s *Store) ClearOverride(e *Ext) { e.State, e.Origin = s.Fallback(e) }

// Recompute resets every extension without an override from the active presets.
func (s *Store) Recompute() {
	for _, e := range s.All() {
		if e.Origin != "override" {
			s.ClearOverride(e)
		}
	}
}

// SetActive makes names the active presets here. It is an unsaved change like
// any toggle; overrides stay.
func (s *Store) SetActive(names []string) {
	s.Presets = names
	s.Recompute()
}

// Try runs f against a copy of the presets, reports which extensions would
// change state and the session total after, then puts everything back.
func (s *Store) Try(f func()) (changed []*Ext, total int) {
	type was struct {
		st     State
		origin string
	}
	before := map[*Ext]was{}
	for _, e := range s.All() {
		before[e] = was{e.State, e.Origin}
	}
	lib, active, projects := s.Library, s.Presets, s.Projects
	s.Library, s.Presets, s.Projects = cloneLib(lib), slices.Clone(active), cloneProjects(projects)
	f()
	s.Recompute()
	for _, e := range s.All() {
		if before[e].st != e.State {
			changed = append(changed, e)
		}
	}
	total = s.Total()
	s.Library, s.Presets, s.Projects = lib, active, projects
	for e, w := range before {
		e.State, e.Origin = w.st, w.origin
	}
	return changed, total
}

// SavePreset writes p over the preset named old: old "" creates p, nil p
// deletes old. Every project using it is rewritten at once (prototype: nothing
// is written). Here, states that change only because of the preset count as
// saved; pending toggles stay pending.
func (s *Store) SavePreset(old string, p *Preset) {
	before := map[*Ext]State{}
	for _, e := range s.All() {
		before[e] = e.State
	}
	i := slices.IndexFunc(s.Library, func(x *Preset) bool { return x.Name == old })
	switch {
	case p == nil && i >= 0:
		s.Library = slices.Delete(s.Library, i, i+1)
	case p == nil:
	case i < 0:
		s.Library = append(s.Library, p)
	default:
		s.Library[i] = p
	}
	rename := func(names []string) []string {
		out := []string{}
		for _, n := range names {
			switch {
			case n != old:
				out = append(out, n)
			case p != nil:
				out = append(out, p.Name)
			}
		}
		return out
	}
	s.Presets = rename(s.Presets)
	for i := range s.Projects {
		s.Projects[i].Presets = rename(s.Projects[i].Presets)
	}
	s.Recompute()
	for e, st := range before {
		if e.State != st && s.saved[e] == st {
			s.saved[e] = e.State
		}
	}
}

func cloneLib(lib []*Preset) []*Preset {
	out := make([]*Preset, len(lib))
	for i, p := range lib {
		out[i] = p.Clone()
	}
	return out
}

func cloneProjects(ps []Project) []Project {
	out := slices.Clone(ps)
	for i := range out {
		out[i].Presets = slices.Clone(out[i].Presets)
	}
	return out
}

// All returns every extension, plugin contents right after their plugin.
func (s *Store) All() []*Ext {
	var out []*Ext
	for _, e := range s.Exts {
		out = append(out, e)
		out = append(out, e.Children...)
	}
	return out
}

func (s *Store) Total() (total int) {
	for _, e := range s.Exts {
		total += e.Cost()
	}
	return total
}

// Unsaved counts extensions whose state differs from the last save.
func (s *Store) Unsaved() (n int) {
	for _, e := range s.All() {
		if s.saved[e] != e.State {
			n++
		}
	}
	return n
}

// Saved is e's state at the last save.
func (s *Store) Saved(e *Ext) State { return s.saved[e] }

// Save is a stub: it only moves the baseline. Nothing is written.
func (s *Store) Save() int {
	n := s.Unsaved()
	s.snapshot()
	return n
}

func (s *Store) snapshot() {
	s.saved = map[*Ext]State{}
	for _, e := range s.All() {
		s.saved[e] = e.State
	}
}

// Tokens formats a token count as "1.2k" or "340".
func Tokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprint(n)
}

// tok gives a stable fake cost in [lo, hi) from a name.
func tok(name string, lo, hi int) int {
	h := fnv.New32a()
	h.Write([]byte(name))
	return lo + int(h.Sum32())%(hi-lo)
}
