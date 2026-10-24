package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"sunvale/internal/remote"
	"sunvale/internal/remote/driver"
)

// smb_storage.go is the SMB driver for the storage mounts: it speaks the
// SMB2/3 protocol directly with go-smb2 (pure Go, no cgo), so a mount does
// not need the OS to have mounted the share. The driver itself lives in
// internal/remote/driver/smb.go; this file holds the storage-page metadata
// (driver form fields, validation) and the order endpoint, which are
// remote-storage concerns rather than driver concerns.
//
// The management page renders the driver's fields (address, username,
// password, share name, root) and the server validates them; the driver
// dials lazily so a share that is down at create time still stores, the
// same way OpenList's create-on-init-failure keeps the row for editing.

// smbDriverInfo is the /api/driver/* entry for the SMB driver. It speaks
// the protocol directly (go-smb2), so it is writable like the local driver;
// OnlyProxy marks downloads as going through the server rather than a
// direct share URL, OpenList's own convention for network drivers.
func smbDriver() localDriverInfo {
	return localDriverInfo{
		ID:        "smb",
		Name:      "SMB 网络共享",
		LocalSort: true,
		OnlyProxy: true,
		NoCache:   true,
		ConfigFields: []localDriverField{
			{Name: "address", Type: "string", Required: true, Help: "服务器地址，例如 192.168.1.10 或 nas.local（缺省端口 445）"},
			{Name: "username", Type: "string", Required: true, Help: "登录用户名"},
			{Name: "password", Type: "string", Help: "登录密码"},
			{Name: "share_name", Type: "string", Required: true, Help: "共享名，例如 public"},
			{Name: "root", Type: "string", Default: "/", Help: "共享内的子路径（可选），例如 /photos"},
		},
	}
}

// smbMountPath is retained for the management page's legacy callers; with
// direct-connection SMB the server assembles no OS path, so this returns
// the share's root field (the sub-path inside the share) for display.
func smbMountPath(server, share, sub string) string {
	_ = server
	_ = share
	return sub
}

// validateSMBAddition checks an SMB mount's addition: address, username and
// share_name are required (password may be empty for guest access). The
// driver dials lazily, so a server that is down at create time still
// stores — OpenList's create-on-init-failure semantics.
func validateSMBAddition(s *remoteStorage) error {
	var add driver.SMBAddition
	if err := json.Unmarshal([]byte(s.Addition), &add); err != nil {
		return errStorageRoot
	}
	add.Address = strings.TrimSpace(add.Address)
	add.Username = strings.TrimSpace(add.Username)
	add.Share = strings.TrimSpace(add.Share)
	add.Root = strings.TrimSpace(add.Root)
	if add.Address == "" || add.Username == "" || add.Share == "" {
		return errStorageRoot
	}
	if add.Root == "" {
		add.Root = "/"
	}
	raw, err := json.Marshal(add)
	if err != nil {
		return errStorageRoot
	}
	s.Addition = string(raw)
	return nil
}

// storageReadOnly reports whether a mount refuses the remote API's write
// operations. With the driver abstraction, this is whether the mount's
// driver implements Writer — the SMB driver now does (it speaks the
// protocol directly), so only a future read-only driver would answer true.
func (a *App) storageReadOnly(s remoteStorage) bool {
	d := a.driverFor(s)
	return !driver.Writable(d)
}

// errStorageReadOnly is what the write endpoints answer a read-only mount
// with.
const errStorageReadOnly = storageErr("该挂载是只读的，不支持写操作")

// remoteWriteGuard answers the protocol error when vpath (a write target)
// sits on a read-only mount, and reports whether the operation may go on.
func (a *App) remoteWriteGuard(w http.ResponseWriter, vpaths ...string) bool {
	for _, vp := range vpaths {
		if vp == "" {
			continue
		}
		mount, _, ok := a.remoteMountFor(vp)
		if ok && a.storageReadOnly(mount) {
			remote.Err(w, 403, errStorageReadOnly.Error())
			return false
		}
	}
	return true
}

// remoteHandleStorageOrder implements POST /api/storage/order: reorder the
// mounts by id list, which is what dragging rows in the management page
// produces. Order 0 keeps the mount last; the homepage's storage row lists
// pinned mounts first.
func (a *App) remoteHandleStorageOrder(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	var req struct {
		IDs []uint `json:"ids"`
	}
	if err := remote.ReadReq(r, &req); err != nil || len(req.IDs) == 0 {
		remote.Err(w, 400, "ids is required")
		return
	}
	remoteStorageMu.Lock()
	list := a.remoteStoragesLocked()
	rank := make(map[uint]int, len(req.IDs))
	for i, id := range req.IDs {
		rank[id] = i + 1
	}
	for i := range list {
		if r, ok := rank[list[i].ID]; ok {
			list[i].Order = r
		}
	}
	a.remoteSaveStorages(list)
	remoteStorageMu.Unlock()
	remote.OK(w, nil)
}
