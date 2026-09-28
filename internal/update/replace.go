package update

import (
	"fmt"
	"os"
	"path/filepath"
)

// Replace swaps the file at exe for bin, atomically.
//
// The new binary is written beside the old one and renamed over it, so there
// is never a moment where exe is missing or half-written — an interrupted
// update leaves the old binary in place, not a broken one. Renaming over a
// running executable is safe on macOS and Linux: the running process keeps
// the old inode until it exits.
//
// Beside, not in a temp directory, because rename cannot cross filesystems.
func Replace(exe string, bin []byte) error {
	dir := filepath.Dir(exe)

	mode := os.FileMode(0o755)
	if info, err := os.Stat(exe); err == nil {
		mode = info.Mode().Perm()
	}

	tmp, err := os.CreateTemp(dir, ".basa-update-*")
	if err != nil {
		return fmt.Errorf("writing to %s: %w", dir, err)
	}
	name := tmp.Name()
	defer os.Remove(name) // a no-op once the rename has succeeded

	if _, err := tmp.Write(bin); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", name, err)
	}

	// #nosec G302 -- this file is the program itself, so it has to be
	// executable, and it takes the mode the binary it replaces already had. The
	// content was checked against checksums.txt before it reached this function.
	if err := os.Chmod(name, mode); err != nil {
		return fmt.Errorf("making %s executable: %w", name, err)
	}
	if err := os.Rename(name, exe); err != nil {
		return fmt.Errorf("replacing %s: %w", exe, err)
	}
	return nil
}
