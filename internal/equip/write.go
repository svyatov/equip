package equip

import (
	"fmt"
	"os"
	"path/filepath"
)

// dirMode is the mode of a directory writeFile creates: no access for others.
const dirMode = 0o750

// writeFile writes data to path through a temp file and a rename, so no
// reader sees half a file. It creates the directory of path.
func writeFile(path string, data []byte) error {
	// Write through a symlink, so a link into dotfiles stays a link.
	target, err := filepath.EvalSymlinks(path)
	if err == nil {
		path = target
	}

	dir := filepath.Dir(path)

	err = os.MkdirAll(dir, dirMode)
	if err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	defer func() { _ = os.Remove(f.Name()) }() // fails once renamed
	// The temp file starts at 0600; an existing file keeps its own mode.
	fi, statErr := os.Stat(path)
	if statErr == nil {
		err = f.Chmod(fi.Mode().Perm())
	}

	if err == nil {
		_, err = f.Write(data)
	}

	if cerr := f.Close(); err == nil {
		err = cerr
	}

	if err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	err = os.Rename(f.Name(), path)
	if err != nil {
		return fmt.Errorf("replace file: %w", err)
	}

	return nil
}
