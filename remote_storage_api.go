package main

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"sunvale/internal/remote"
)

// remote_storage_api.go is the HTTP surface for /api/storage/* and
// /api/driver/* used by the management 存储 page. Split from remote_storage.go
// (LAY1 / R7 start).
//
// R7 boundary: handlers are (*App) methods; keep here until a storage Host exists.

func (a *App) remoteHandleFSStorageList(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	var objs []remoteObj
	for _, s := range a.remoteStoragesOrdered() {
		if s.Disabled {
			continue
		}
		name := "主文件夹"
		if s.MountPath != "/" {
			name = strings.Trim(s.MountPath, "/")
		}
		if s.Remark != "" {
			name = s.Remark
		}
		objs = append(objs, remoteObj{
			Name: name, IsDir: true, Modified: s.Modified, Created: s.Modified,
			Type: remote.FSTypeFolder, HashInfo: "null", Path: s.MountPath,
		})
	}
	remote.OK(w, remoteFSListData{Content: objs, Total: int64(len(objs)), Write: false, Provider: "storage"})
}

// remoteHandleStorageList implements GET /api/storage/list: the mounts in
// order, paged as OpenList pages them.
func (a *App) remoteHandleStorageList(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	perPage, _ := strconv.Atoi(r.URL.Query().Get("per_page"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 1000 {
		perPage = 0 // all, the default the storage page asks for
	}
	list := a.remoteStorages()
	total := len(list)
	start, end := 0, total
	if perPage > 0 {
		start = min((page-1)*perPage, total)
		end = min(start+perPage, total)
		if start > total {
			start = total
		}
	}
	out := make([]*remoteStorage, 0, end-start)
	for _, s := range list[start:end] {
		out = append(out, storageRespOf(s))
	}
	remote.OK(w, map[string]any{"content": out, "total": total})
}

// remoteHandleStorageGet implements GET /api/storage/get?id=N.
func (a *App) remoteHandleStorageGet(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	for _, s := range a.remoteStorages() {
		if int(s.ID) == id {
			remote.OK(w, s)
			return
		}
	}
	remote.Err(w, 500, "storage not found")
}

// remoteStorageReadReq decodes a storage body and validates the parts the
// app can honour: a mount path, and a driver the app serves — local for a
// disk folder, smb for an OS-mounted network share (smb_storage.go).
func remoteStorageReadReq(r *http.Request) (remoteStorage, error) {
	var s remoteStorage
	if err := remote.ReadReq(r, &s); err != nil {
		return s, err
	}
	if s.Driver == "" {
		s.Driver = "local"
	}
	if s.Driver != "local" && s.Driver != "smb" {
		return s, errStorageDriver
	}
	return s, nil
}

type storageErr string

func (e storageErr) Error() string { return string(e) }

const (
	errStorageDriver = storageErr("仅支持本机路径（local）与已挂载的 SMB 共享（smb）")
	errStorageMount  = storageErr("挂载路径不能为空")
	errStorageRoot   = storageErr("目录不存在或不是文件夹")
	errStorageTaken  = storageErr("该挂载路径已被占用")
)

// validateStorage normalises and checks a storage before it is stored.
func validateStorage(s *remoteStorage) error {
	if strings.TrimSpace(s.MountPath) == "" {
		return errStorageMount
	}
	s.MountPath = remote.FSCleanVPath(s.MountPath)
	if s.MountPath != "/" {
		s.MountPath = strings.TrimSuffix(s.MountPath, "/")
	}
	if s.Driver == "smb" {
		if err := validateSMBAddition(s); err != nil {
			return err
		}
		s.Modified = time.Now()
		if s.Status == "" {
			s.Status = "work"
		}
		return nil
	}
	var add localStorageAddition
	if err := json.Unmarshal([]byte(s.Addition), &add); err != nil || strings.TrimSpace(add.Root) == "" {
		return errStorageRoot
	}
	add.Root = strings.TrimSpace(add.Root)
	if info, err := statDir(add.Root); err != nil || !info {
		return errStorageRoot
	}
	raw, err := json.Marshal(add)
	if err != nil {
		return errStorageRoot
	}
	s.Addition = string(raw)
	s.Modified = time.Now()
	if s.Status == "" {
		s.Status = "work"
	}
	return nil
}

// remoteHandleStorageCreate implements POST /api/storage/create.
func (a *App) remoteHandleStorageCreate(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	s, err := remoteStorageReadReq(r)
	if err != nil {
		remote.Err(w, 500, err.Error())
		return
	}
	if err := validateStorage(&s); err != nil {
		remote.Err(w, 500, err.Error())
		return
	}
	remoteStorageMu.Lock()
	list := a.remoteStoragesLocked()
	for _, x := range list {
		if x.MountPath == s.MountPath {
			remoteStorageMu.Unlock()
			remote.Err(w, 500, errStorageTaken.Error())
			return
		}
	}
	maxID := uint(0)
	for _, x := range list {
		maxID = max(maxID, x.ID)
	}
	s.ID = maxID + 1
	list = append(list, s)
	a.remoteSaveStorages(list)
	remoteStorageMu.Unlock()
	remote.OK(w, map[string]any{"id": s.ID})
}

// remoteHandleStorageUpdate implements POST /api/storage/update. The body
// carries the whole storage, as OpenList's does.
func (a *App) remoteHandleStorageUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	s, err := remoteStorageReadReq(r)
	if err != nil {
		remote.Err(w, 500, err.Error())
		return
	}
	if s.ID == 0 {
		remote.Err(w, 400, "id is required")
		return
	}
	if err := validateStorage(&s); err != nil {
		remote.Err(w, 500, err.Error())
		return
	}
	remoteStorageMu.Lock()
	list := a.remoteStoragesLocked()
	found := false
	for i := range list {
		if list[i].ID != s.ID {
			continue
		}
		for j := range list {
			if j != i && list[j].MountPath == s.MountPath {
				remoteStorageMu.Unlock()
				remote.Err(w, 500, errStorageTaken.Error())
				return
			}
		}
		s.Disabled = list[i].Disabled // enable/disable own that flag
		list[i] = s
		found = true
		break
	}
	if !found {
		remoteStorageMu.Unlock()
		remote.Err(w, 500, "storage not found")
		return
	}
	a.remoteSaveStorages(list)
	remoteStorageMu.Unlock()
	remote.OK(w, nil)
}

// remoteHandleStorageDelete implements POST /api/storage/delete?id=N.
func (a *App) remoteHandleStorageDelete(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	remoteStorageMu.Lock()
	list := a.remoteStoragesLocked()
	kept := list[:0]
	removed := false
	for _, s := range list {
		if int(s.ID) == id {
			if s.MountPath == "/" {
				remoteStorageMu.Unlock()
				remote.Err(w, 500, "根挂载不能删除，请改为停用")
				return
			}
			removed = true
			continue
		}
		kept = append(kept, s)
	}
	if !removed {
		remoteStorageMu.Unlock()
		remote.Err(w, 500, "storage not found")
		return
	}
	a.remoteSaveStorages(kept)
	remoteStorageMu.Unlock()
	remote.OK(w, nil)
}

// remoteSetStorageDisabled implements enable and disable, which differ only
// in the flag they set.
func (a *App) remoteSetStorageDisabled(w http.ResponseWriter, r *http.Request, disabled bool) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	remoteStorageMu.Lock()
	list := a.remoteStoragesLocked()
	found := false
	for i := range list {
		if int(list[i].ID) == id {
			list[i].Disabled = disabled
			if disabled {
				list[i].Status = "disabled"
			} else {
				list[i].Status = "work"
			}
			found = true
			break
		}
	}
	if !found {
		remoteStorageMu.Unlock()
		remote.Err(w, 500, "storage not found")
		return
	}
	a.remoteSaveStorages(list)
	remoteStorageMu.Unlock()
	remote.OK(w, nil)
}

func (a *App) remoteHandleStorageEnable(w http.ResponseWriter, r *http.Request) {
	a.remoteSetStorageDisabled(w, r, false)
}

func (a *App) remoteHandleStorageDisable(w http.ResponseWriter, r *http.Request) {
	a.remoteSetStorageDisabled(w, r, true)
}

// remoteHandleStorageLoadAll implements POST /api/storage/load_all. The
// mounts are read from the store on every request, so there is nothing to
// reload; the endpoint exists so the frontend's button does not 404.
func (a *App) remoteHandleStorageLoadAll(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remote.Err(w, 401, "Guest user is disabled, login please")
		return
	}
	remote.OK(w, nil)
}

// remoteHandleDriverList implements GET /api/driver/list: every driver the
// create page may pick, keyed by name, as OpenList answers.
func (a *App) remoteHandleDriverList(w http.ResponseWriter, r *http.Request) {
	remote.OK(w, map[string]localDriverInfo{"local": localDriver(), "smb": smbDriver()})
}

// remoteHandleDriverNames implements GET /api/driver/names.
func (a *App) remoteHandleDriverNames(w http.ResponseWriter, r *http.Request) {
	remote.OK(w, []string{"local", "smb"})
}

// remoteHandleDriverInfo implements GET /api/driver/info?driver=….
func (a *App) remoteHandleDriverInfo(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Query().Get("driver") {
	case "local":
		remote.OK(w, localDriver())
	case "smb":
		remote.OK(w, smbDriver())
	default:
		remote.Err(w, 404, "driver not found")
	}
}

// localDriver is the local driver's description: one field, the folder.
func localDriver() localDriverInfo {
	return localDriverInfo{
		ID:        "local",
		Name:      "本机路径",
		LocalSort: true,
		ConfigFields: []localDriverField{{
			Name:     "root",
			Type:     "string",
			Default:  "",
			Required: true,
			Help:     "要显示的文件夾路径，例如 /Users/me/Pictures",
		}},
	}
}

// statDir reports whether path is an existing folder.
func statDir(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}
