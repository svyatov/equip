package equip

// Project is the git repo equip runs in, taken at the main checkout's root,
// or the directory itself outside git.
type Project struct {
	Path       string // symlinks resolved
	RootCommit string // empty outside git or with no commits
}

// Session is one open Project.
type Session struct {
	project Project
	exts    []Extension
}

// View is what the user sees of a Session.
type View struct {
	Project Project
	Rows    []Row
}

// Row is one extension in the list.
type Row struct {
	Name string
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
	return &Session{project: p, exts: exts}, nil
}

// View returns the current view.
func (s *Session) View() View {
	v := View{Project: s.project}
	for _, e := range s.exts {
		v.Rows = append(v.Rows, Row{Name: e.Key})
	}
	return v
}
