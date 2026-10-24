// Package driver is the remote storage driver abstraction, shaped after
// OpenList's internal/driver: a storage mount resolves to one Driver that
// owns every filesystem operation under it. The local driver wraps os.*,
// the SMB driver speaks the protocol directly with go-smb2 (no cgo).
// Capability is interface presence — a driver that only implements Reader
// is read-only, and the remote handlers ask for Writer before offering a
// write, rather than branching on the driver name.
package driver

import (
	"context"
	"io"
	"time"
)

// Obj is one filesystem entry as the remote layer sees it: the fields the
// list/get/download routes return, without any driver-specific state. It
// mirrors OpenList's model.Obj enough for the handlers to build responses
// without caring which driver answered.
type Obj struct {
	Name    string
	Size    int64
	IsDir   bool
	ModTime time.Time
	// Created is the birth time when the driver can read it; ModTime is
	// the portable fallback.
	Created time.Time
}

// Config is a driver's static description: the id the storage page picks,
// the human name, and the flags the frontend reads to shape its form
// (NoUpload greys the write buttons, OnlyProxy forces download through the
// server, LocalSort means the driver sorts its own listings).
type Config struct {
	ID        string
	Name      string
	LocalSort bool
	OnlyLocal bool
	OnlyProxy bool
	NoCache   bool
	NoUpload  bool
	// DefaultRoot is the root the create form pre-fills, when it has one.
	DefaultRoot string
}

// Field is one entry of a driver's Addition form: the storage page renders
// it, the driver unmarshals the submitted JSON.
type Field struct {
	Name     string
	Type     string
	Default  string
	Required bool
	Help     string
}

// Reader is the read half every driver implements. Path is the path inside
// the mount (relative to the mount root, "/" is the root itself), so the
// driver never sees another mount's paths.
type Reader interface {
	List(ctx context.Context, dir string) ([]Obj, error)
	Get(ctx context.Context, p string) (Obj, error)
	// Open returns a ReadSeekCloser for a file's bytes. The caller closes it;
	// for network drivers the handle lives until then. Seek is required for
	// HTTP Range requests (http.ServeContent).
	Open(ctx context.Context, p string) (io.ReadSeekCloser, error)
}

// Writer is the write half. A driver that does not implement Writer is
// read-only — the remote handlers type-assert and refuse writes with 403,
// so there is no storageReadOnly(driverName) branch.
type Writer interface {
	Mkdir(ctx context.Context, p string) error
	Rename(ctx context.Context, p, newName string) error
	Move(ctx context.Context, src, dst string) error
	Copy(ctx context.Context, src, dst string) error
	Remove(ctx context.Context, p string) error
	// Put writes one file's bytes to dst (a full path inside the mount).
	// The caller streams; the driver handles the temp-and-rename itself.
	Put(ctx context.Context, dst string, r io.Reader) error
}

// Driver is what a storage mount holds: its config and its reader, plus a
// writer if it has one. Drop releases any connection the driver opened.
type Driver interface {
	Config() Config
	Reader
	Drop(ctx context.Context) error
}

// Writable reports whether d implements Writer. The handlers call this
// once per request to decide if a write endpoint is open.
func Writable(d Driver) bool {
	_, ok := d.(Writer)
	return ok
}
