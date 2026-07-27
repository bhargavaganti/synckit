//go:build windows

package app

import "os"

// lockHeld reports whether the lock file is currently held. On Windows a
// running Eclipse/DBeaver holds .metadata/.lock with a deny-write share mode,
// so opening it for writing fails with a sharing violation. A stale file that
// nobody holds opens fine.
func lockHeld(path string) bool {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return !os.IsNotExist(err) // sharing violation → held; missing → not running
	}
	_ = f.Close()
	return false
}
