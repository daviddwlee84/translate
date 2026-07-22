//go:build unix

package store

import (
	"os"
	"syscall"
)

// lockFile takes an exclusive advisory lock (flock) on f and returns an unlock
// func. It serializes history appends across processes (e.g. a running `translate
// serve` and a concurrent CLI invocation) so their writes cannot interleave —
// POSIX only guarantees atomic O_APPEND up to PIPE_BUF (4 KiB), and translations
// can exceed that. Best-effort: on a filesystem without flock support the error is
// surfaced to the caller, which still holds the in-process mutex.
func lockFile(f *os.File) (func(), error) {
	fd := int(f.Fd())
	if err := syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		return func() {}, err
	}
	return func() { _ = syscall.Flock(fd, syscall.LOCK_UN) }, nil
}
