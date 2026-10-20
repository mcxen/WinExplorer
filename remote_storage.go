package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

// remote_storage.go is the OpenList-compatible storage API: the
// /api/storage/* and /api/driver/* endpoints the browser frontend's 存储
// page drives. A storage is a mount point: its MountPath is a virtual path
// and its Addition says which disk folder that path shows. The storage
// mounted at "/" is the remote root — what the file browser opens on — so
// changing it is how the owner chooses what the homepage lists.
//
// The field names, the response envelopes and the enable/disable/load_all
// semantics follow OpenList's model.Storage and handles/storage.go. The
// driver set is deliberately small: "local" mounts a folder of this
// machine, which is the only thing Sunvale can serve without a second
// filesystem implementation.

// remoteStorage is OpenList's model.Storage with the fields this app can
// honour. The *_details are read-only extras OpenList's page shows.
type remoteStorage struct {
	ID              uint      `json:"id"`
	MountPath       string    `json:"mount_path"`
	Order           int       `json:"order"`
	Driver          string    `json:"driver"`
	CacheExpiration int       `json:"cache_expiration"`
	Status          string    `json:"status"`
	Addition        string    `json:"addition"`
	Remark          string    `json:"remark"`
	Modified        time.Time `json:"modified"`
	Disabled        bool      `json:"disabled"`
	DisableIndex    bool      `json:"disable_index"`
	EnableSign      bool      `json:"enable_sign"`

	OrderBy        string `json:"order_by"`
	OrderDirection string `json:"order_direction"`
	ExtractFolder  string `json:"extract_folder"`

	WebProxy     bool   `json:"web_proxy"`
	WebdavPolicy string `json:"webdav_policy"`
	ProxyRange   bool   `json:"proxy_range"`
	DownProxyURL string `json:"down_proxy_url"`

	DisableProxySign bool `json:"disable_proxy_sign"`

	// MountDetails is what OpenList's StorageResp carries; the local driver
	// reports the folder's free and used space.
	MountDetails *remoteStorageDetails `json:"mount_details,omitempty"`
}

// remoteStorageDetails is OpenList's DiskUsage for a mount.
type remoteStorageDetails struct {
	TotalSpace int64 `json:"total_space"`
	UsedSpace  int64 `json:"used_space"`
	FreeSpace  int64 `json:"free_space"`
}

// localStorageAddition is the local driver's Addition payload: the disk
// folder the mount shows.
type localStorageAddition struct {
	Root string `json:"root"`
}

// localDriverInfo is the /api/driver/* entry for the local driver,
// shaped like OpenList's driver.Info: the config fields the create page
// renders, each with a type, and the common flags.
type localDriverInfo struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	LocalSort    bool               `json:"local_sort"`
	OnlyLocal    bool               `json:"only_local"`
	OnlyProxy    bool               `json:"only_proxy"`
	NoCache      bool               `json:"no_cache"`
	NoUpload     bool               `json:"no_upload"`
	NeedProxy    bool               `json:"need_proxy"`
	DefaultRoot  string             `json:"default_root"`
	ConfigFields []localDriverField `json:"config"`
	CommonFields []localDriverField `json:"common"`
}

// localDriverField is one entry of a driver's config form.
type localDriverField struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Default  string `json:"default"`
	Required bool   `json:"required"`
	Help     string `json:"help"`
}

// remoteStorageMu guards the storage list, which the admin page writes and
// the file routes read.
var remoteStorageMu sync.Mutex

// remoteStorages returns the configured mounts, seeding the home folder as
// the root mount on first use — a fresh install serves the user's home
// directory exactly as before, and the storage page can point it elsewhere.
func (a *App) remoteStorages() []remoteStorage {
	remoteStorageMu.Lock()
	defer remoteStorageMu.Unlock()
	return a.remoteStoragesLocked()
}

// remoteStoragesLocked needs remoteStorageMu held.
func (a *App) remoteStoragesLocked() []remoteStorage {
	if a.store == nil {
		// A test app without a store still serves its home folder as the
		// root mount.
		raw, _ := json.Marshal(localStorageAddition{Root: remoteFSHome()})
		return []remoteStorage{{
			ID: 1, MountPath: "/", Driver: "local", Status: "work",
			Addition: string(raw), Modified: time.Now(),
		}}
	}
	list := a.store.remoteStorages()
	if len(list) > 0 {
		return list
	}
	root, _ := json.Marshal(localStorageAddition{Root: remoteFSHome()})
	list = []remoteStorage{{
		ID:        1,
		MountPath: "/",
		Driver:    "local",
		Status:    "work",
		Addition:  string(root),
		Modified:  time.Now(),
	}}
	a.store.setRemoteStorages(list)
	return list
}

// remoteSaveStorages persists the whole list.
func (a *App) remoteSaveStorages(list []remoteStorage) {
	if a.store == nil {
		return
	}
	a.store.setRemoteStorages(list)
}

// remoteRootMount returns the storage mounted at "/" — the one the remote
// browser opens on. ok is false when the root mount is disabled, which
// takes the whole remote tree away until it is enabled again.
func (a *App) remoteRootMount() (remoteStorage, bool) {
	for _, s := range a.remoteStorages() {
		if s.MountPath == "/" {
			if s.Disabled {
				return s, false
			}
			return s, true
		}
	}
	return remoteStorage{}, false
}

// storageDiskRoot is the disk folder a storage serves.
func storageDiskRoot(s remoteStorage) string {
	var add localStorageAddition
	if err := json.Unmarshal([]byte(s.Addition), &add); err != nil || add.Root == "" {
		return remoteFSHome()
	}
	return add.Root
}

// remoteMountFor finds the storage whose mount path holds vpath, longest
// match first, as a file system mount table resolves. It returns the mount
// and the path inside it.
//
// A disabled mount still claims its paths: just as unmounting takes a path
// away rather than showing whatever is under it, /pics after /pics was
// disabled is gone rather than answered by a shallower mount.
func (a *App) remoteMountFor(vpath string) (remoteStorage, string, bool) {
	vpath = remoteFSCleanVPath(vpath)
	var (
		best   remoteStorage
		inner  string
		found  bool
		length int
	)
	for _, s := range a.remoteStorages() {
		mp := remoteFSCleanVPath(s.MountPath)
		var (
			match  bool
			inside string
		)
		switch {
		case mp == "/":
			match, inside = true, vpath
		case vpath == mp:
			match, inside = true, "/"
		case strings.HasPrefix(vpath, strings.TrimSuffix(mp, "/")+"/"):
			match, inside = true, strings.TrimPrefix(vpath, mp)
		}
		if !match || len(mp) <= length {
			continue
		}
		best, inner, found, length = s, inside, true, len(mp)
	}
	if !found || best.Disabled {
		return best, inner, false
	}
	return best, inner, true
}

// remoteDiskPathIn maps a path inside a storage to its disk path. ok is
// false when the path would escape the mount.
func remoteDiskPathIn(s remoteStorage, inner string) (string, bool) {
	inner = remoteFSCleanVPath(inner)
	for _, seg := range strings.Split(inner, "/") {
		if seg == ".." {
			return "", false
		}
	}
	root := storageDiskRoot(s)
	rel := strings.TrimPrefix(inner, "/")
	if rel == "" {
		return root, true
	}
	return path.Join(root, rel), true
}

// storageRespOf decorates a storage with its mount details, as OpenList's
// makeStorageResp does.
func storageRespOf(s remoteStorage) *remoteStorage {
	root := storageDiskRoot(s)
	if free, total, ok := volumeCap(root); ok {
		s.MountDetails = &remoteStorageDetails{
			TotalSpace: total,
			UsedSpace:  total - free,
			FreeSpace:  free,
		}
	}
	return &s
}

// ---- handlers -------------------------------------------------------------

// remoteHandleStorageList implements GET /api/storage/list: the mounts in
// order, paged as OpenList pages them.
func (a *App) remoteHandleStorageList(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
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
	remoteOK(w, map[string]any{"content": out, "total": total})
}

// remoteHandleStorageGet implements GET /api/storage/get?id=N.
func (a *App) remoteHandleStorageGet(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	id, _ := strconv.Atoi(r.URL.Query().Get("id"))
	for _, s := range a.remoteStorages() {
		if int(s.ID) == id {
			remoteOK(w, s)
			return
		}
	}
	remoteErr(w, 500, "storage not found")
}

// remoteStorageReadReq decodes a storage body and validates the parts the
// app can honour: a mount path, the local driver, and an addition that
// names a folder.
func remoteStorageReadReq(r *http.Request) (remoteStorage, error) {
	var s remoteStorage
	if err := remoteReadReq(r, &s); err != nil {
		return s, err
	}
	if s.Driver == "" {
		s.Driver = "local"
	}
	if s.Driver != "local" {
		return s, errStorageDriver
	}
	return s, nil
}

type storageErr string

func (e storageErr) Error() string { return string(e) }

const (
	errStorageDriver = storageErr("仅支持本机路径（local 驱动）")
	errStorageMount  = storageErr("挂载路径不能为空")
	errStorageRoot   = storageErr("目录不存在或不是文件夹")
	errStorageTaken  = storageErr("该挂载路径已被占用")
)

// validateStorage normalises and checks a storage before it is stored.
func validateStorage(s *remoteStorage) error {
	if strings.TrimSpace(s.MountPath) == "" {
		return errStorageMount
	}
	s.MountPath = remoteFSCleanVPath(s.MountPath)
	if s.MountPath != "/" {
		s.MountPath = strings.TrimSuffix(s.MountPath, "/")
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
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	s, err := remoteStorageReadReq(r)
	if err != nil {
		remoteErr(w, 500, err.Error())
		return
	}
	if err := validateStorage(&s); err != nil {
		remoteErr(w, 500, err.Error())
		return
	}
	remoteStorageMu.Lock()
	list := a.remoteStoragesLocked()
	for _, x := range list {
		if x.MountPath == s.MountPath {
			remoteStorageMu.Unlock()
			remoteErr(w, 500, errStorageTaken.Error())
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
	remoteOK(w, map[string]any{"id": s.ID})
}

// remoteHandleStorageUpdate implements POST /api/storage/update. The body
// carries the whole storage, as OpenList's does.
func (a *App) remoteHandleStorageUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	s, err := remoteStorageReadReq(r)
	if err != nil {
		remoteErr(w, 500, err.Error())
		return
	}
	if s.ID == 0 {
		remoteErr(w, 400, "id is required")
		return
	}
	if err := validateStorage(&s); err != nil {
		remoteErr(w, 500, err.Error())
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
				remoteErr(w, 500, errStorageTaken.Error())
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
		remoteErr(w, 500, "storage not found")
		return
	}
	a.remoteSaveStorages(list)
	remoteStorageMu.Unlock()
	remoteOK(w, nil)
}

// remoteHandleStorageDelete implements POST /api/storage/delete?id=N.
func (a *App) remoteHandleStorageDelete(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
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
				remoteErr(w, 500, "根挂载不能删除，请改为停用")
				return
			}
			removed = true
			continue
		}
		kept = append(kept, s)
	}
	if !removed {
		remoteStorageMu.Unlock()
		remoteErr(w, 500, "storage not found")
		return
	}
	a.remoteSaveStorages(kept)
	remoteStorageMu.Unlock()
	remoteOK(w, nil)
}

// remoteSetStorageDisabled implements enable and disable, which differ only
// in the flag they set.
func (a *App) remoteSetStorageDisabled(w http.ResponseWriter, r *http.Request, disabled bool) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
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
		remoteErr(w, 500, "storage not found")
		return
	}
	a.remoteSaveStorages(list)
	remoteStorageMu.Unlock()
	remoteOK(w, nil)
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
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	remoteOK(w, nil)
}

// remoteHandleDriverList implements GET /api/driver/list: every driver the
// create page may pick, keyed by name, as OpenList answers.
func (a *App) remoteHandleDriverList(w http.ResponseWriter, r *http.Request) {
	remoteOK(w, map[string]localDriverInfo{"local": localDriver()})
}

// remoteHandleDriverNames implements GET /api/driver/names.
func (a *App) remoteHandleDriverNames(w http.ResponseWriter, r *http.Request) {
	remoteOK(w, []string{"local"})
}

// remoteHandleDriverInfo implements GET /api/driver/info?driver=local.
func (a *App) remoteHandleDriverInfo(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("driver") != "local" {
		remoteErr(w, 404, "driver not found")
		return
	}
	remoteOK(w, localDriver())
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
