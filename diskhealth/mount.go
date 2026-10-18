package diskhealth

import (
	"bufio"
	"os"
	"strings"
)

// mountEntry is one row of the mount table: where a device is mounted and
// with which filesystem.
type mountEntry struct {
	From string // the device node, "/dev/disk3s5"
	On   string // the mount point, "/System/Volumes/Data"
	FS   string // the filesystem name, "apfs"
}

// readMounts is the platform's mount table. Every platform answers in the
// same shape, so mountOf and the volume matching need no branches.
func readMounts() ([]mountEntry, error) {
	if entries, err := platformMounts(); err == nil {
		return entries, nil
	}
	// Linux and friends: /proc/mounts, then /etc/mtab, both in the util-
	// linux format "device mountpoint fstype options".
	for _, path := range []string{"/proc/mounts", "/etc/mtab"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		return parseMounts(string(data)), nil
	}
	return nil, os.ErrNotExist
}

// mountOf resolves a device node to its mount point, its filesystem and
// its free space. A device mounted nowhere (Preboot, Recovery, VM)
// returns "", "", -1 — callers tell that apart from "space unknown".
func mountOf(device string) (mount, fs string, free int64) {
	mounts, err := readMounts()
	if err != nil {
		return "", "", -1
	}
	for _, m := range mounts {
		if m.From != device {
			continue
		}
		f, _, ok := capacityOf(m.On)
		if !ok {
			f = -1
		}
		return m.On, m.FS, f
	}
	return "", "", -1
}

// parseMounts reads a mount table in either shape the platforms use.
func parseMounts(text string) []mountEntry {
	var out []mountEntry
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		if m, ok := parseMountLine(sc.Text()); ok {
			out = append(out, m)
		}
	}
	return out
}

// parseMountLine reads one line of the mount table. macOS writes the
// device, the word "on", the mount point and the options in parentheses;
// Linux lists three or six whitespace-separated fields:
//
//	/dev/disk3s5 on /System/Volumes/Data (apfs, local, journaled)
//	/dev/sda1 / ext4 rw,relatime 0 0
func parseMountLine(line string) (mountEntry, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return mountEntry{}, false
	}
	if i := strings.Index(line, " on "); i > 0 {
		from := line[:i]
		rest := line[i+4:]
		j := strings.Index(rest, " (")
		if j < 0 {
			return mountEntry{}, false
		}
		fs := strings.TrimSuffix(strings.TrimPrefix(rest[j+2:], "("), ")")
		if k := strings.IndexByte(fs, ','); k >= 0 {
			fs = fs[:k]
		}
		return mountEntry{
			From: unescapeMount(from),
			On:   unescapeMount(rest[:j]),
			FS:   strings.TrimSpace(fs),
		}, true
	}
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return mountEntry{}, false
	}
	return mountEntry{
		From: unescapeMount(fields[0]),
		On:   unescapeMount(fields[1]),
		FS:   fields[2],
	}, true
}

// unescapeMount turns the kernel's "\040" escapes back into the characters
// they stand for — "Macintosh\040HD" is "Macintosh HD".
func unescapeMount(s string) string {
	return strings.NewReplacer(
		`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`,
	).Replace(s)
}

// isPartitionOf reports whether a device node is a partition of the disk:
// "/dev/sda1" is one of "sda", "/dev/nvme0n1p1" one of "nvme0n1".
func isPartitionOf(device, disk string) bool {
	rest, ok := strings.CutPrefix(device, "/dev/"+disk)
	if !ok || rest == "" {
		return false
	}
	rest = strings.TrimPrefix(rest, "p")
	if rest == "" {
		return false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
