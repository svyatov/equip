package equip

import (
	"os"
	"path/filepath"
)

// writeFile writes data to path through a temp file and a rename, so no
// reader sees half a file. It creates the directory of path.
func writeFile(path string, data []byte) error {
	// Write through a symlink, so a link into dotfiles stays a link.
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }() // fails once renamed
	// The temp file starts at 0600; an existing file keeps its own mode.
	if fi, serr := os.Stat(path); serr == nil {
		err = f.Chmod(fi.Mode().Perm())
	}
	if err == nil {
		_, err = f.Write(data)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
