//go:build !windows

package app

import (
	"os"
	"syscall"
)

// lockHeld reports whether another process currently holds a POSIX (fcntl)
// write lock on the file — the mechanism Eclipse/DBeaver uses for .metadata/.lock.
// The lock FILE persists after exit, so mere existence is not "running"; only a
// held lock is. Uses F_GETLK, which reports any conflicting lock.
func lockHeld(path string) bool {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return false // can't open → not being held by us to test → treat as free
	}
	defer f.Close()
	lk := syscall.Flock_t{Type: syscall.F_WRLCK, Whence: 0, Start: 0, Len: 0}
	if err := syscall.FcntlFlock(f.Fd(), syscall.F_GETLK, &lk); err != nil {
		return false
	}
	return lk.Type != syscall.F_UNLCK // a conflicting lock exists → app is running
}
