package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// storageEnv is an admin server over a home folder, with its own store
// beside the home so the fs listings stay clean.
func storageEnv(t *testing.T) *remoteTestEnv {
	t.Helper()
	e := newRemoteManageEnv(t)
	remoteStorageMu.Lock()
	e.a.remoteSaveStorages(nil)
	remoteStorageMu.Unlock()
	return e
}

// TestRemoteStorageDefaultsToHome: with no mount configured, the server
// serves the user's home folder at "/" — a fresh install behaves as before
// the storage API existed.
func TestRemoteStorageDefaultsToHome(t *testing.T) {
	e := storageEnv(t)
	list := e.a.remoteStorages()
	if len(list) != 1 || list[0].MountPath != "/" || list[0].Driver != "local" {
		t.Fatalf("default storages = %+v, want one local mount at /", list)
	}
	if got := storageDiskRoot(list[0]); got != e.home {
		t.Fatalf("root mount serves %q, want the home %q", got, e.home)
	}
}

// TestRemoteStorageCRUD walks create, list, update, enable/disable and
// delete over HTTP, the way the management page drives them.
func TestRemoteStorageCRUD(t *testing.T) {
	e := storageEnv(t)
	pics := filepath.Join(e.home, "Pictures")
	if err := os.MkdirAll(pics, 0o755); err != nil {
		t.Fatal(err)
	}
	tok := e.login()

	add := func(root string) remoteTestResp {
		return e.api(http.MethodPost, "/api/storage/create", tok, map[string]any{
			"mount_path": "/pics",
			"driver":     "local",
			"addition":   `{"root":"` + root + `"}`,
		})
	}
	rr := add(pics)
	if rr.Code != 200 {
		t.Fatalf("create: %d (%s)", rr.Code, rr.Message)
	}
	var created struct {
		ID uint `json:"id"`
	}
	if err := json.Unmarshal(rr.Data, &created); err != nil || created.ID == 0 {
		t.Fatalf("create returned no id: %s", rr.Data)
	}

	// A second mount at the same path is refused.
	if rr := add(pics); rr.Code == 200 {
		t.Fatal("a duplicate mount path was accepted")
	}
	// A missing folder is refused.
	if rr := add(filepath.Join(e.home, "nope")); rr.Code == 200 {
		t.Fatal("a mount over a missing folder was accepted")
	}
	// A driver the app cannot serve is refused.
	rr = e.api(http.MethodPost, "/api/storage/create", tok, map[string]any{
		"mount_path": "/smb", "driver": "smb", "addition": `{"root":"/x"}`,
	})
	if rr.Code == 200 {
		t.Fatal("an unsupported driver was accepted")
	}

	rr = e.api(http.MethodGet, "/api/storage/list", tok, nil)
	if rr.Code != 200 {
		t.Fatalf("list: %d (%s)", rr.Code, rr.Message)
	}
	var page struct {
		Content []remoteStorage `json:"content"`
		Total   int             `json:"total"`
	}
	if err := json.Unmarshal(rr.Data, &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Content) != 2 {
		t.Fatalf("list = %+v, want the root mount and /pics", page)
	}

	// The mount serves its folder: /pics/x maps to <root>/x.
	if disk, ok := e.a.remoteDiskPath("/pics/one.jpg"); !ok ||
		disk != filepath.Join(pics, "one.jpg") {
		t.Fatalf("remoteDiskPath(/pics/one.jpg) = %q %v", disk, ok)
	}
	// The root mount still serves the home.
	if disk, ok := e.a.remoteDiskPath("/Documents/a.txt"); !ok ||
		disk != filepath.Join(e.home, "Documents", "a.txt") {
		t.Fatalf("remoteDiskPath through root = %q %v", disk, ok)
	}

	// Disabling the /pics mount takes its paths away; the root still works.
	if rr := e.api(http.MethodPost, "/api/storage/disable?id="+strconv.Itoa(int(created.ID)), tok, nil); rr.Code != 200 {
		t.Fatalf("disable: %d (%s)", rr.Code, rr.Message)
	}
	if _, ok := e.a.remoteDiskPath("/pics/one.jpg"); ok {
		t.Fatal("a disabled mount still serves its paths")
	}
	if _, ok := e.a.remoteDiskPath("/Documents/a.txt"); !ok {
		t.Fatal("disabling one mount took the root with it")
	}
	if rr := e.api(http.MethodPost, "/api/storage/enable?id="+strconv.Itoa(int(created.ID)), tok, nil); rr.Code != 200 {
		t.Fatalf("enable: %d (%s)", rr.Code, rr.Message)
	}
	if _, ok := e.a.remoteDiskPath("/pics/one.jpg"); !ok {
		t.Fatal("an enabled mount serves nothing")
	}

	// Update points the mount at another folder.
	other := filepath.Join(e.home, "Music")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	rr = e.api(http.MethodPost, "/api/storage/update", tok, map[string]any{
		"id":         created.ID,
		"mount_path": "/pics",
		"driver":     "local",
		"addition":   `{"root":"` + other + `"}`,
	})
	if rr.Code != 200 {
		t.Fatalf("update: %d (%s)", rr.Code, rr.Message)
	}
	if disk, _ := e.a.remoteDiskPath("/pics/song.mp3"); disk != filepath.Join(other, "song.mp3") {
		t.Fatalf("after update the mount serves %q", disk)
	}

	// The root mount cannot be deleted; another one can.
	rr = e.api(http.MethodPost, "/api/storage/delete?id=1", tok, nil)
	if rr.Code == 200 {
		t.Fatal("the root mount was deleted")
	}
	if rr := e.api(http.MethodPost, "/api/storage/delete?id="+strconv.Itoa(int(created.ID)), tok, nil); rr.Code != 200 {
		t.Fatalf("delete: %d (%s)", rr.Code, rr.Message)
	}
	if len(e.a.remoteStorages()) != 1 {
		t.Fatalf("after delete the mounts are %+v", e.a.remoteStorages())
	}
}

// TestRemoteStorageChangesTheHomepage: the root mount is what the remote
// browser opens on, so pointing it elsewhere changes the homepage listing.
func TestRemoteStorageChangesTheHomepage(t *testing.T) {
	e := storageEnv(t)
	pics := filepath.Join(e.home, "Pictures")
	if err := os.MkdirAll(pics, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pics, "cat.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tok := e.login()

	// Point the root mount at Pictures.
	rr := e.api(http.MethodPost, "/api/storage/update", tok, map[string]any{
		"id":         1,
		"mount_path": "/",
		"driver":     "local",
		"addition":   `{"root":"` + pics + `"}`,
	})
	if rr.Code != 200 {
		t.Fatalf("update root: %d (%s)", rr.Code, rr.Message)
	}

	rr = e.api(http.MethodPost, "/api/fs/list", tok, map[string]any{"path": "/"})
	if rr.Code != 200 {
		t.Fatalf("root list: %d (%s)", rr.Code, rr.Message)
	}
	var page remoteFSListData
	if err := json.Unmarshal(rr.Data, &page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Content[0].Name != "cat.jpg" {
		t.Fatalf("homepage = %+v, want the Pictures folder's single file", page.Content)
	}
}

// TestRemoteDriverEndpoints: the create page's driver list and form.
func TestRemoteDriverEndpoints(t *testing.T) {
	e := storageEnv(t)
	tok := e.login()

	rr := e.api(http.MethodGet, "/api/driver/list", tok, nil)
	if rr.Code != 200 {
		t.Fatalf("driver list: %d (%s)", rr.Code, rr.Message)
	}
	var drivers map[string]localDriverInfo
	if err := json.Unmarshal(rr.Data, &drivers); err != nil {
		t.Fatal(err)
	}
	info, ok := drivers["local"]
	if !ok || len(info.ConfigFields) == 0 || info.ConfigFields[0].Name != "root" {
		t.Fatalf("driver list = %+v, want local with a root field", drivers)
	}

	if rr := e.api(http.MethodGet, "/api/driver/names", tok, nil); rr.Code != 200 {
		t.Fatalf("driver names: %d", rr.Code)
	}
	if rr := e.api(http.MethodGet, "/api/driver/info?driver=local", tok, nil); rr.Code != 200 {
		t.Fatalf("driver info: %d", rr.Code)
	}
	if rr := e.api(http.MethodGet, "/api/driver/info?driver=smb", tok, nil); rr.Code != 404 {
		t.Fatalf("unknown driver: code %d, want 404", rr.Code)
	}
}

// TestShareDialogCreatesAShare: the 分享 dialog's create path produces a
// share through the server's own handler, with the password and expiry the
// dialog asked for.
func TestShareDialogCreatesAShare(t *testing.T) {
	e := storageEnv(t)
	remoteShareAccessMu.Lock()
	remoteShareAccess = map[string]time.Time{}
	remoteShareAccessMu.Unlock()
	if err := os.WriteFile(filepath.Join(e.home, "a.txt"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := e.a
	a.share.src = []string{filepath.Join(e.home, "a.txt")}
	a.share.open = true
	a.share.usePwd = true
	a.share.pwdBuf = "1234"
	a.share.expires = "1 天"
	a.shareCreate()

	if a.share.err != "" {
		t.Fatalf("shareCreate: %s", a.share.err)
	}
	if a.share.result == nil {
		t.Fatal("shareCreate made no share")
	}
	s := a.share.result
	if s.Pwd != "1234" {
		t.Fatalf("share password = %q, want 1234", s.Pwd)
	}
	if s.Expires == nil {
		t.Fatal("the 1 天 expiry was dropped")
	}
	if len(s.Files) != 1 || s.Files[0] != "/a.txt" {
		t.Fatalf("share files = %v, want the virtual /a.txt", s.Files)
	}
	if link := a.shareLink(); !strings.HasSuffix(link, "/sd/"+s.ID) {
		t.Fatalf("share link = %q", link)
	}
	// The share reached the store the server reads.
	found := false
	for _, x := range a.store.shareAll() {
		if x.ID == s.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("the share is not in the store: %+v", a.store.shareAll())
	}
}

// TestShareDialogRefusesOutsideHome: the remote root is the home folder, so
// a file elsewhere cannot be shared rather than being silently remapped.
func TestShareDialogRefusesOutsideHome(t *testing.T) {
	e := storageEnv(t)
	a := e.a
	a.share.src = []string{filepath.Join(t.TempDir(), "outside.txt")}
	a.share.open = true
	a.shareCreate()
	if a.share.err == "" {
		t.Fatal("a path outside the home folder was shared")
	}
	if a.share.result != nil {
		t.Fatal("a refused share still produced one")
	}
}

// TestRemoteDefaultPassword: a fresh account starts as admin/admin, the
// OpenList convention, and the management page can change it.
func TestRemoteDefaultPassword(t *testing.T) {
	remoteResetForTest()
	defer remoteResetForTest()
	a := &App{}
	conf := a.remoteConf()
	if conf.Username != "admin" {
		t.Fatalf("default username = %q, want admin", conf.Username)
	}
	if remoteHashPassword(remoteDefaultPassword) != conf.PassHash {
		t.Fatal("the default password is not admin")
	}

	// The management page's account endpoint changes it.
	e := newRemoteTestEnv(t)
	tok := e.conf.Token
	rr := e.api(http.MethodPost, "/api/sunvale/account", tok, map[string]any{"password": "new-secret"})
	if rr.Code != 200 {
		t.Fatalf("account change: %d (%s)", rr.Code, rr.Message)
	}
	if e.a.remoteConf().PassHash == remoteHashPassword("new-secret") {
		// expected: the hash changed
	} else {
		t.Fatal("the account endpoint did not change the password")
	}
}
