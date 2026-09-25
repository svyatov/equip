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

type Store struct {
	Project string
	Presets []string // active in this project
	Library []Preset // every preset the user has
	Exts    []*Ext   // top level, in load order
	saved   map[*Ext]State
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
