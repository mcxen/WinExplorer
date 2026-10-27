package main

import (
	"context"
	"encoding/json"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"sunvale/internal/remote"
	"sunvale/internal/remote/driver"
)

// remote_storage.go holds OpenList-compatible storage mount types, driver
// resolution and path mapping. HTTP CRUD and /api/driver/* live in
// remote_storage_api.go (LAY1 split).
//
// R7 boundary: storageDiskRoot / buildDriver / validateStorage are mostly pure;
// (*App) cache and handlers need Host + store.

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

// storageDiskRoot is the disk folder a local storage serves. SMB storages
// have no disk root (they speak the protocol), so this returns "" for them;
// callers that only care about local mounts check Driver first.
func storageDiskRoot(s remoteStorage) string {
	if s.Driver == "smb" {
		return ""
	}
	var add localStorageAddition
	if err := json.Unmarshal([]byte(s.Addition), &add); err != nil || add.Root == "" {
		return remoteFSHome()
	}
	return add.Root
}

// driverFor returns the driver for a storage, building and caching it on
// first use. A driver that fails to init is still returned (its operations
// will error), so the storage stays in the list with a failed status —
// OpenList's create-on-init-failure semantics, so a share that is down at
// create time can be fixed and enabled without losing its config.
func (a *App) driverFor(s remoteStorage) driver.Driver {
	if a.driverCache == nil {
		a.driverCache = map[uint]driver.Driver{}
	}
	if d, ok := a.driverCache[s.ID]; ok {
		return d
	}
	d := buildDriver(s)
	a.driverCache[s.ID] = d
	return d
}

// buildDriver constructs a driver from its stored config without caching.
func buildDriver(s remoteStorage) driver.Driver {
	switch s.Driver {
	case "smb":
		var add driver.SMBAddition
		_ = json.Unmarshal([]byte(s.Addition), &add)
		return driver.NewSMB(add)
	default:
		return &driver.Local{Root: storageDiskRoot(s)}
	}
}

// dropDriver removes a storage's cached driver, called on update/delete so
// a reconfigured mount does not keep its old connection.
func (a *App) dropDriver(id uint) {
	if a.driverCache == nil {
		return
	}
	if d, ok := a.driverCache[id]; ok {
		_ = d.Drop(context.Background())
		delete(a.driverCache, id)
	}
}

// remoteMountFor finds the storage whose mount path holds vpath, longest
// match first, as a file system mount table resolves. It returns the mount
// and the path inside it.
//
// A disabled mount still claims its paths: just as unmounting takes a path
// away rather than showing whatever is under it, /pics after /pics was
// disabled is gone rather than answered by a shallower mount.
func (a *App) remoteMountFor(vpath string) (remoteStorage, string, bool) {
	vpath = remote.FSCleanVPath(vpath)
	var (
		best   remoteStorage
		inner  string
		found  bool
		length int
	)
	for _, s := range a.remoteStorages() {
		mp := remote.FSCleanVPath(s.MountPath)
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

// remoteDriverFor resolves vpath to its mount's driver and the path inside
// the mount. ok is false when no enabled mount serves vpath. The returned
// driver is cached per storage; the inner path is what the driver operates
// on (relative to the mount root).
func (a *App) remoteDriverFor(vpath string) (driver.Driver, string, bool) {
	mount, inner, ok := a.remoteMountFor(remote.FSCleanVPath(vpath))
	if !ok {
		return nil, "", false
	}
	return a.driverFor(mount), inner, true
}

// remoteDiskPathIn maps a path inside a storage to its disk path. ok is
// false when the path would escape the mount.
func remoteDiskPathIn(s remoteStorage, inner string) (string, bool) {
	inner = remote.FSCleanVPath(inner)
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

// remoteStoragesOrdered returns the configured mounts, lowest Order first —
// the homepage's storage row and the management page's list both read this
// way, so a pinned mount (the page's 置顶) leads the row.
func (a *App) remoteStoragesOrdered() []remoteStorage {
	list := a.remoteStorages()
	out := make([]remoteStorage, len(list))
	copy(out, list)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			// Order 0 is "never ranked": it keeps its store order, after the
			// pinned ones rather than before them.
			if out[i].Order == 0 || out[j].Order == 0 {
				return out[j].Order == 0
			}
			return out[i].Order < out[j].Order
		}
		return false
	})
	return out
}

// remoteHandleFSStorageList implements GET /api/fs/storage_list: the
// enabled mounts as folders, which is the list the homepage shows above
// the root mount's own files. Disabled mounts stay off the homepage — a
// stopped mount is not a place to browse — while the management page's
// own storage list still shows them, through /api/storage/list.
