package equip

import (
	"errors"
	"maps"
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
}

// State is how an extension takes part in a Project's sessions.
type State int

// The states, in the order the user picks them.
const (
	On State = iota
	ManualOnly
	Off
)

var stateNames = [...]string{On: "on", ManualOnly: "manual-only", Off: "off"}

func (s State) String() string { return stateNames[s] }

// Session is one open Project.
type Session struct {
	m         Machine
	project   Project
	exts      []Extension
	overrides map[string]State // pending, by extension key
	saved     map[string]State // the overrides at the last save
	disk      map[string]State // Claude Code's entries for exts, as last read or written
	notes     map[string]string
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
	Note     string // why the row changed, such as an edit outside equip
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
	// ponytail: broken settings read as no entries here; Save reports them.
	disk, _ := readClaude(p, exts)
	if saved == nil {
		// A first open imports the states set by hand, so a save keeps them.
		saved = disk
	}
	s := &Session{m: m, project: p, exts: exts, overrides: maps.Clone(saved), saved: saved, disk: map[string]State{}, notes: map[string]string{}}
	s.take(disk)
	return s, nil
}

// take takes now, Claude Code's entries on disk, and imports each entry that
// changed since the last read and differs from the record as an unsaved
// Override. It reports whether any entry changed.
func (s *Session) take(now map[string]State) bool {
	changed := false
	for _, e := range s.exts {
		key := e.Key
		if !differ(now, s.disk, key) {
			continue
		}
		changed = true
		delete(s.notes, key)
		st, ok := now[key]
		if !ok || !differ(now, s.saved, key) {
			// Missing or as recorded: the row shows the record's state.
			st, ok = s.saved[key]
		} else {
			s.notes[key] = "changed outside equip in Claude Code"
		}
		if ok {
			s.overrides[key] = st
		} else {
			delete(s.overrides, key)
		}
	}
	s.disk = now
	return changed
}

// SetState makes st an Override for the extension with key.
func (s *Session) SetState(key string, st State) { s.overrides[key] = st }

// View returns the current view.
func (s *Session) View() View {
	v := View{Project: s.project}
	for _, e := range s.exts {
		// With no presets, a skill falls back to Claude Code's default: on.
		r := Row{Name: e.Key, State: On, Fallback: On, Unsaved: s.unsaved(e.Key), Note: s.notes[e.Key]}
		if st, ok := s.overrides[e.Key]; ok {
			r.State, r.Override = st, true
		}
		v.Rows = append(v.Rows, r)
	}
	keys := maps.Clone(s.saved)
	maps.Copy(keys, s.overrides)
	maps.Copy(keys, s.disk)
	for key := range keys {
		if s.unsaved(key) {
			v.Unsaved++
		}
	}
	return v
}

// unsaved reports whether a save would change the Override for key or, for
// an installed extension, its entry on disk.
func (s *Session) unsaved(key string) bool {
	installed := slices.ContainsFunc(s.exts, func(e Extension) bool { return e.Key == key })
	return differ(s.overrides, s.saved, key) || installed && differ(s.overrides, s.disk, key)
}

// differ reports whether a and b hold different states for key.
func differ(a, b map[string]State, key string) bool {
	st, ok := a[key]
	was, wasOK := b[key]
	return ok != wasOK || st != was
}

// DropOverride removes the Override for key, so the extension falls back.
func (s *Session) DropOverride(key string) { delete(s.overrides, key) }

// Save writes the pending Overrides into Claude Code's settings. If Claude
// Code's entries changed since they were read, it writes nothing, imports the
// changes and returns ErrChangedSinceOpen.
func (s *Session) Save() error {
	now, err := readClaude(s.project, s.exts)
	if err != nil {
		return err
	}
	if s.take(now) {
		return ErrChangedSinceOpen
	}
	// Nothing to write. With saved Overrides, a save still writes them, so
	// the record of a first open's imports gets created.
	if s.View().Unsaved == 0 && len(s.saved) == 0 {
		return nil
	}
	if err := writeClaude(s.m, s.project, s.exts, s.overrides); err != nil {
		return err
	}
	if err := writeRecord(s.m, s.project, s.overrides); err != nil {
		return err
	}
	s.saved = maps.Clone(s.overrides)
	clear(s.notes)
	s.disk = map[string]State{}
	for _, e := range s.exts {
		if st, ok := s.overrides[e.Key]; ok {
			s.disk[e.Key] = st
		}
	}
	return nil
}
