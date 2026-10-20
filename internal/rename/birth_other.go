//go:build !darwin

package rename

import (
	"os"
	"time"
)

// BirthTime returns the file's modification time on non-macOS platforms,
// where a portable creation time is not reliably available.
func BirthTime(path string) (time.Time, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}
