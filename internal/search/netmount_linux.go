//go:build linux

package search

import (
	"os"
	"strings"
)

// networkMounts lists mount points of network and FUSE remote filesystems,
// which a background index must not crawl.
func networkMounts() []string {
	data, err := os.ReadFile("/proc/self/mounts")
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		switch t := f[2]; {
		case t == "nfs", t == "nfs4", t == "cifs", t == "smb3", t == "smbfs",
			t == "9p", t == "afs", t == "davfs", strings.HasPrefix(t, "fuse.sshfs"),
			t == "fuse.rclone", t == "fuse.gvfsd-fuse":
			out = append(out, strings.ReplaceAll(f[1], `\040`, " "))
		}
	}
	return out
}
