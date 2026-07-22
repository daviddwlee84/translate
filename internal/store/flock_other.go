//go:build !unix

package store

import "os"

// lockFile is a no-op on platforms without flock (e.g. Windows); the in-process
// mutex still serializes writes within a single process.
func lockFile(*os.File) (func(), error) { return func() {}, nil }
