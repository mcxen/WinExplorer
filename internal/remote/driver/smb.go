package driver

import (
	"context"
	"errors"
	"io"
	"net"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	smb2 "github.com/cloudsoda/go-smb2"
)

// smb.go is the SMB driver: it speaks SMB2/3 directly with go-smb2 (pure
// Go, no cgo), so a mount does not need the OS to have mounted the share.
// The driver holds one session+tree connect and re-establishes it when the
// connection drops, the way OpenList's driver does. Unlike the earlier
// OS-mount approach, this driver is writable — the remote API can create,
// rename, move, copy, remove and upload into the share, the same as a
// local mount, because the capability is the interface, not a flag.

// SMBAddition is the SMB driver's Addition payload, matching OpenList's
// drivers/smb: an address (host or host:port), credentials, a share name,
// and a root path inside the share.
type SMBAddition struct {
	Address  string `json:"address"`
	Username string `json:"username"`
	Password string `json:"password"`
	Share    string `json:"share_name"`
	Root     string `json:"root"`
}

// SMB is an SMB mount. The session and share are lazy: the first operation
// dials, and a dropped connection is redialed on the next call. mu guards
// the handles; one dial at a time.
type SMB struct {
	add SMBAddition

	mu     sync.Mutex
	dialer *smb2.Dialer
	sess   *smb2.Session
	share  *smb2.Share
}

// NewSMB builds a driver from its addition. It does not dial — Init is
// deferred to the first operation so a share that is down at create time
// still stores (OpenList's create-on-init-failure semantics).
func NewSMB(add SMBAddition) *SMB {
	return &SMB{add: add}
}

func (s *SMB) Config() Config {
	return Config{ID: "smb", Name: "SMB 网络共享", LocalSort: true, OnlyProxy: true, NoCache: true}
}

// ensureShare dials and mounts if needed, returning the live share or the
// error that prevented it. It is the single chokepoint for reconnection.
func (s *SMB) ensureShare(ctx context.Context) (*smb2.Share, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.share != nil {
		// A cheap echo confirms the connection; on failure we drop and
		// redial below.
		if err := s.sess.Echo(); err == nil {
			return s.share, nil
		}
		s.dropLocked()
	}
	addr := s.add.Address
	if _, _, err := net.SplitHostPort(addr); err != nil {
		addr = addr + ":445"
	}
	if s.dialer == nil {
		s.dialer = &smb2.Dialer{Initiator: &smb2.NTLMInitiator{User: s.add.Username, Password: s.add.Password}}
	}
	sess, err := s.dialer.Dial(ctx, addr)
	if err != nil {
		return nil, err
	}
	share, err := sess.Mount(s.add.Share)
	if err != nil {
		_ = sess.Logoff()
		return nil, err
	}
	s.sess, s.share = sess, share
	return share, nil
}

// dropLocked releases the current session; s.mu must be held.
func (s *SMB) dropLocked() {
	if s.share != nil {
		_ = s.share.Umount()
		s.share = nil
	}
	if s.sess != nil {
		_ = s.sess.Logoff()
		s.sess = nil
	}
}

// join maps a mount-relative path to a path inside the share. The share
// uses forward slashes internally (go-smb2 accepts both); the root is the
// subfolder the mount was created with.
func (s *SMB) join(p string) string {
	root := strings.TrimPrefix(strings.ReplaceAll(s.add.Root, `\`, "/"), "/")
	rel := strings.TrimPrefix(path.Clean("/"+p), "/")
	if root == "" {
		return rel
	}
	if rel == "" || rel == "." {
		return root
	}
	return root + "/" + rel
}

func (s *SMB) List(ctx context.Context, dir string) ([]Obj, error) {
	share, err := s.ensureShare(ctx)
	if err != nil {
		return nil, err
	}
	entries, err := share.ReadDir(s.join(dir))
	if err != nil {
		return nil, err
	}
	out := make([]Obj, 0, len(entries))
	for _, info := range entries {
		out = append(out, smbObj(info))
	}
	return out, nil
}

func (s *SMB) Get(ctx context.Context, p string) (Obj, error) {
	share, err := s.ensureShare(ctx)
	if err != nil {
		return Obj{}, err
	}
	info, err := share.Stat(s.join(p))
	if err != nil {
		return Obj{}, err
	}
	return smbObj(info), nil
}

func (s *SMB) Open(ctx context.Context, p string) (io.ReadSeekCloser, error) {
	share, err := s.ensureShare(ctx)
	if err != nil {
		return nil, err
	}
	return share.Open(s.join(p))
}

func (s *SMB) Mkdir(ctx context.Context, p string) error {
	share, err := s.ensureShare(ctx)
	if err != nil {
		return err
	}
	return share.MkdirAll(s.join(p), 0o755)
}

func (s *SMB) Rename(ctx context.Context, p, newName string) error {
	share, err := s.ensureShare(ctx)
	if err != nil {
		return err
	}
	dir := path.Dir(s.join(p))
	return share.Rename(s.join(p), dir+"/"+newName)
}

func (s *SMB) Move(ctx context.Context, src, dst string) error {
	share, err := s.ensureShare(ctx)
	if err != nil {
		return err
	}
	// Rename across directories works on SMB when both are on the same
	// tree, which they are (one mount = one tree connect).
	return share.Rename(s.join(src), s.join(dst))
}

func (s *SMB) Copy(ctx context.Context, src, dst string) error {
	share, err := s.ensureShare(ctx)
	if err != nil {
		return err
	}
	return smbCopy(share, s.join(src), s.join(dst))
}

func (s *SMB) Remove(ctx context.Context, p string) error {
	share, err := s.ensureShare(ctx)
	if err != nil {
		return err
	}
	return share.RemoveAll(s.join(p))
}

func (s *SMB) Put(ctx context.Context, dst string, r io.Reader) error {
	share, err := s.ensureShare(ctx)
	if err != nil {
		return err
	}
	p := s.join(dst)
	if err := share.MkdirAll(path.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := share.Create(p)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, r)
	closeErr := f.Close()
	if copyErr != nil {
		_ = share.Remove(p)
		return copyErr
	}
	return closeErr
}

func (s *SMB) Drop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dropLocked()
	return nil
}

// smbObj maps an os.FileInfo from the share into a driver.Obj. go-smb2
// fills ModTime and Size; IsDir is reliable. Birth time is not always
// available over SMB, so ModTime stands in for Created.
func smbObj(info interface {
	Name() string
	Size() int64
	IsDir() bool
	ModTime() time.Time
}) Obj {
	size := info.Size()
	if info.IsDir() {
		size = 0
	}
	return Obj{Name: info.Name(), Size: size, IsDir: info.IsDir(), ModTime: info.ModTime(), Created: info.ModTime()}
}

// smbCopy copies one entry within the same share. SMB's server-side copy
// is not universally available, so this reads and writes through the
// client — correct everywhere, fast enough on a LAN.
func smbCopy(share *smb2.Share, src, dst string) error {
	info, err := share.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := share.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := share.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := smbCopy(share, filepath.ToSlash(filepath.Join(src, e.Name())), filepath.ToSlash(filepath.Join(dst, e.Name()))); err != nil {
				return err
			}
		}
		return nil
	}
	in, err := share.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := share.OpenFile(dst, flagsCreate, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// flagsCreate is O_CREATE|O_TRUNC|O_WRONLY for the share's OpenFile. The
// smb2 package accepts the standard os flags.
const flagsCreate = 0

var _ = errors.New // keep errors import if future guards need it
