//go:build darwin

package main

import (
	"syscall"
)

// networkFSTypes are the darwin filesystem types statfs reports for a
// share mounted over the network, as Finder's "Connect to Server" does.
var networkFSTypes = map[string]bool{
	"smbfs":  true,
	"afpfs":  true,
	"nfs":    true,
	"webdav": true,
	"cifs":   true,
	"sshfs":  true,
	"ftp":    true,
}

// platformIsNetworkPath asks statfs for the filesystem type at path. The
// mount type covers every path under the mount point — a folder deep in an
// SMB share reports smbfs like the share root.
func platformIsNetworkPath(path string) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return false
	}
	name := int8sliceString(st.Fstypename[:])
	return networkFSTypes[name]
}

// int8sliceString is a statfs name field as a Go string, up to its NUL.
func int8sliceString(b []int8) string {
	n := 0
	for n < len(b) && b[n] != 0 {
		n++
	}
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = byte(b[i])
	}
	return string(out)
}
