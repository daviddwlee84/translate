package tts

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/daviddwlee84/translate/internal/xdgpath"
)

// cacheFile returns the on-disk MP3 path for (tl, text). dir overrides the
// default (<XDG cache>/translate/tts).
func cacheFile(dir, tl, text string) string {
	if dir == "" {
		dir = filepath.Join(xdgpath.CacheDir(), "tts")
	}
	sum := sha256.Sum256([]byte(tl + "\x00" + text))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".mp3")
}

// cached reports whether path exists and is non-empty.
func cached(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Size() > 0
}

// writeCache writes b to path atomically (temp + rename), creating the directory.
func writeCache(path string, b []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tts-*.mp3")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
