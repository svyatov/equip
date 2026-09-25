package equip

import "maps"

// Project is the git repo equip runs in, taken at the main checkout's root,
// or the directory itself outside git.
type Project struct {
	Path       string // symlinks resolved
	RootCommit string // empty outside git or with no commits
	gitDir     string // the main checkout's .git; empty outside git
}

// State is how an extension takes part in a Project's sessions.
type State int

// The states, in the order the user picks them.
const (
	On State = iota
	ManualOnly
	Off
)

// Session is one open Project.
type Session struct {
	m         Machine
	project   Project
	exts      []Extension
	overrides map[string]State // pending, by extension key
	saved     map[string]State // the overrides at the last save
}

// View is what the user sees of a Session.
type View struct {
	Project Project
	Rows    []Row
	Unsaved int // pending changes a save would write
}

// Row is one extension in the list.
type Row struct {
	Name     string
	State    State
	Override bool  // State was set by hand in this Project
	Fallback State // the state without the Override
	Unsaved  bool
}

// Open finds the Project of dir and discovers its extensions.
func Open(m Machine, dir string) (*Session, error) {
	p, err := locate(m, dir)
	if err != nil {
		return nil, err
	}
	exts, err := discover(m)
	if err != nil {
		return nil, err
	}
	saved, err := readRecord(m, p)
	if err != nil {
		return nil, err
	}
	return &Session{m: m, project: p, exts: exts, overrides: maps.Clone(saved), saved: saved}, nil
}

// SetState makes st an Override for the extension with key.
func (s *Session) SetState(key string, st State) { s.overrides[key] = st }

// View returns the current view.
func (s *Session) View() View {
	v := View{Project: s.project}
	for _, e := range s.exts {
		// With no presets, a skill falls back to Claude Code's default: on.
		r := Row{Name: e.Key, State: On, Fallback: On, Unsaved: s.unsaved(e.Key)}
		if st, ok := s.overrides[e.Key]; ok {
			r.State, r.Override = st, true
		}
		v.Rows = append(v.Rows, r)
	}
	keys := maps.Clone(s.saved)
	maps.Copy(keys, s.overrides)
	for key := range keys {
		if s.unsaved(key) {
			v.Unsaved++
		}
	}
	return v
}

// unsaved reports whether the Override for key differs from the last save.
func (s *Session) unsaved(key string) bool {
	st, ok := s.overrides[key]
	was, wasOK := s.saved[key]
	return ok != wasOK || st != was
}

// DropOverride removes the Override for key, so the extension falls back.
func (s *Session) DropOverride(key string) { delete(s.overrides, key) }

// Save writes the pending Overrides into Claude Code's settings.
func (s *Session) Save() error {
	if err := writeClaude(s.m, s.project, s.exts, s.overrides); err != nil {
		return err
	}
	if err := writeRecord(s.m, s.project, s.overrides); err != nil {
		return err
	}
	s.saved = maps.Clone(s.overrides)
	return nil
}
