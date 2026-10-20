package fsops

import (
	"encoding/binary"
	"strings"
	"time"
	"unicode/utf16"
)

// recycleinfo.go parses the Recycle Bin's $I metadata files (the format
// Windows 10 and 11 write, version 2), so listing, restoring and
// emptying the bin are plain file operations, testable on any platform:
//
//	 0  uint32   format version (2)
//	 4  uint64   the item's size
//	12  int64    deletion time, a Windows FILETIME
//	20  uint32   the original path's length, in UTF-16 units
//	24  UTF-16LE the original path

// parseRecycleInfo returns the original path recorded in a $I file.
func ParseRecycleInfo(data []byte) (string, bool) {
	if len(data) < 24 {
		return "", false
	}
	if binary.LittleEndian.Uint32(data[0:]) != 2 {
		return "", false // a version this build does not read
	}
	n := binary.LittleEndian.Uint32(data[20:])
	if n == 0 || int(n) > (len(data)-24)/2 {
		return "", false
	}
	u16 := make([]uint16, n)
	for i := range u16 {
		u16[i] = binary.LittleEndian.Uint16(data[24+2*i:])
	}
	return decodeUTF16(u16), true
}

// recycleDeletedAt is the deletion time the $I file records, zero when
// unreadable.
func RecycleDeletedAt(data []byte) time.Time {
	if len(data) < 20 || binary.LittleEndian.Uint32(data[0:]) != 2 {
		return time.Time{}
	}
	ft := binary.LittleEndian.Uint64(data[12:])
	return filetimeToTime(ft)
}

// windowsEpoch is FILETIME's zero, 1601-01-01, in Unix seconds.
const windowsEpoch = -11644473600

// filetimeToTime converts a Windows FILETIME — 100 ns ticks since
// 1601-01-01 — to UTC, where the zero value means "unknown".
func filetimeToTime(ft uint64) time.Time {
	if ft == 0 {
		return time.Time{}
	}
	sec := int64(ft) / 1e7
	nsec := (int64(ft) % 1e7) * 100
	return time.Unix(sec+windowsEpoch, nsec).UTC()
}

// winFiletime converts a time to FILETIME, without time.Duration, which
// overflows past about 292 years.
func WinFiletime(t time.Time) uint64 {
	sec := t.Unix() - windowsEpoch
	return uint64(sec)*1e7 + uint64(t.Nanosecond())/100
}

// decodeUTF16 renders UTF-16LE units as text, trimming trailing NULs.
func decodeUTF16(u []uint16) string {
	for len(u) > 0 && u[len(u)-1] == 0 {
		u = u[:len(u)-1]
	}
	return string(utf16.Decode(u))
}

// — freedesktop trash (Linux) —

// trashinfoEscape percent-escapes a path for a .trashinfo file, which is
// a URI-style Path= line; the freedesktop spec says only % must be
// escaped, as UTF-8 passes through directly.
func TrashinfoEscape(path string) string {
	return strings.ReplaceAll(path, "%", "%25")
}

func TrashinfoUnescape(path string) string {
	return strings.ReplaceAll(path, "%25", "%")
}
