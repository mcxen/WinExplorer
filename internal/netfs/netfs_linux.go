//go:build linux

package netfs

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// networkFSTypes are the linux mounts that live on the network: the SMB
// kernel client (cifs, smb3), NFS, the common FUSE network filesystems and
// WebDAV.
var networkFSTypes = map[string]bool{
	"cifs":        true,
	"smb3":        true,
	"smbfs":       true,
	"nfs":         true,
	"nfs4":        true,
	"fuse.sshfs":  true,
	"sshfs":       true,
	"fuse.rclone": true,
	"davfs":       true,
	"webdav":      true,
}

// networkMagic are statfs' f_type values for the same: CIFS_MAGIC_NUMBER,
// NFS_SUPER_MAGIC and FUSE_SUPER_MAGIC. They catch a mount whose type in
// the mount table could not be read.
var networkMagic = map[int64]bool{
	0xFF534D42: true, // CIFS_MAGIC_NUMBER
	0x6969:     true, // NFS_SUPER_MAGIC
	0x65735546: true, // FUSE_SUPER_MAGIC
}

// platformIsNetworkPath finds the mount point holding path in the mount
// table and checks its type. statfs alone does not name the filesystem on
// linux, only its magic, so the table is the first word and the magic the
// fallback.
func platformIsNetworkPath(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if fstype, ok := mountTypeOf(abs); ok && networkFSTypes[fstype] {
		return true
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(abs, &st); err != nil {
		return false
	}
	return networkMagic[st.Type]
}

// mountTypeOf is the filesystem type of the mount point holding abs: the
// longest mount point in /proc/mounts (then /etc/mtab) that abs is under.
func mountTypeOf(abs string) (string, bool) {
	for _, table := range []string{"/proc/mounts", "/etc/mtab"} {
		f, err := os.Open(table)
		if err != nil {
			continue
		}
		bestMnt, bestType := "", ""
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) < 3 {
				continue
			}
			mnt := unescapeMount(fields[1])
			if mnt == "" || !underPath(abs, mnt) {
				continue
			}
			if len(mnt) > len(bestMnt) {
				bestMnt, bestType = mnt, fields[2]
			}
		}
		f.Close()
		if bestMnt != "" {
			return bestType, true
		}
	}
	return "", false
}

// underPath reports whether p is root itself or sits below it.
func underPath(p, root string) bool {
	return p == root || strings.HasPrefix(p, strings.TrimSuffix(root, "/")+"/")
}

// mountUnescaper turns the kernel's "\040"-style escapes back into the
// characters they stand for — "Macintosh\040HD" is "Macintosh HD". The four
// escapes are the whole set the mount table uses.
var mountUnescaper = strings.NewReplacer(
	`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`,
)

// unescapeMount decodes a mount table path name.
func unescapeMount(s string) string { return mountUnescaper.Replace(s) }
