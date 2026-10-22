package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"sunvale/internal/remote"

	"github.com/egoist/mygo"
)

// remoteConfig is the OpenList-compatible remote access account, persisted
// in state.json. Token is the server's master secret: the administrator's
// API token, the HMAC key of the login JWTs and the download sign key at
// once.
type remoteConfig struct {
	Enabled  bool   `json:"enabled"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	PassHash string `json:"pass_hash"`
	Token    string `json:"token"`
}

// remotePortCount is how many ports remoteStart walks through when the
// configured one is taken: 5244, then 5245..5254.
const remotePortCount = 10

// remotePluginID is the plugin's id in the registry (plugins.go).
const remotePluginID = "remote"

// registerRemotePlugin registers the 远程访问 plugin: the 查看更多 menu's
// remote items, and the LAN server the plugin switch owns — turning the
// plugin off stops it at once, turning it back on brings it up when the
// account still has remote access on.
func registerRemotePlugin() {
	RegisterPlugin(Plugin{
		ID:   remotePluginID,
		Name: "远程访问",
		Desc: "把主文件夹通过局域网共享给手机",
		OnEnable: func(a *App) {
			if a.remoteConf().Enabled {
				a.remoteStart()
			}
		},
		OnDisable: func(a *App) { a.remoteStop() },
		Settings: func(a *App) []PluginSetting {
			conf := a.remoteConf()
			return []PluginSetting{
				{Kind: PluginSettingInfo, Name: "账号", Value: conf.Username},
				{Kind: PluginSettingInfo, Name: "端口", Value: strconv.Itoa(conf.Port)},
				{
					Kind:   PluginSettingAction,
					Name:   "复制远程地址",
					Desc:   "把地址、账号和密码复制到剪贴板",
					Action: "复制",
					Run: func() {
						mygo.Clipboard.WriteText(a.remoteLANURL() + "\n账号 " + a.remoteConf().Username + "\n密码 " + a.remotePlainPassword())
						a.statusMsg = "已复制远程地址"
					},
				},
			}
		},
		MoreItems: func(a *App) []PluginMenuItem {
			return []PluginMenuItem{
				{
					Label:   "远程访问",
					Checked: a.remoteConf().Enabled,
					Run:     a.remoteToggle,
				},
				{
					Label: "复制远程地址",
					Run: func() {
						mygo.Clipboard.WriteText(a.remoteLANURL() + "\n账号 " + a.remoteConf().Username + "\n密码 " + a.remotePlainPassword())
						a.statusMsg = "已复制远程地址"
					},
				},
			}
		},
	})
}

var (
	// remotePassPlain is the plaintext behind PassHash, in memory only, so
	// 复制远程地址 can show it. It is lost on restart, where the hash keeps
	// working but a copy has to reset the password to a new one.
	remotePassPlain string

	// remoteConfMu guards remoteConfCache and remotePassPlain. A cached
	// *remoteConfig is immutable once published; changes copy it, swap the
	// pointer and persist through remoteSaveConf.
	remoteConfMu    sync.Mutex
	remoteConfCache *remoteConfig

	// remoteSrv is the running HTTP server, nil while stopped.
	remoteSrvMu sync.Mutex
	remoteSrv   *http.Server

	// remoteLiveTokens are the login JWTs this run issued. remoteStop
	// clears them, so every login is invalid after a restart, as OpenList
	// behaves.
	remoteLiveMu     sync.Mutex
	remoteLiveTokens = map[string]struct{}{}
)

// remoteConf returns the remote access configuration, generating the default
// account on first use — port 5244, the admin user, the admin password (the
// user changes it in 管理界面) and a random 64-hex token — and persisting it.
func (a *App) remoteConf() *remoteConfig {
	remoteConfMu.Lock()
	defer remoteConfMu.Unlock()
	return a.remoteConfLocked()
}

// remoteDefaultPassword is the account a fresh install starts with. It is
// the OpenList convention, and the management page at /@manage makes
// changing it the first thing an owner can do.
const remoteDefaultPassword = "admin"

// remoteConfLocked needs remoteConfMu held.
func (a *App) remoteConfLocked() *remoteConfig {
	if remoteConfCache != nil {
		return remoteConfCache
	}
	if a.store != nil && a.store.Remote != nil {
		rc := *a.store.Remote
		remoteConfCache = &rc
		return remoteConfCache
	}
	pass := remoteDefaultPassword
	rc := &remoteConfig{
		Port:     5244,
		Username: "admin",
		PassHash: remote.HashPassword(pass),
		Token:    remote.RandToken(),
	}
	if a.store != nil {
		a.store.setRemote(rc)
	}
	remotePassPlain = pass
	remoteConfCache = rc
	return remoteConfCache
}

// remoteSaveConf persists a modified configuration and swaps the cached
// copy; rc must not be mutated afterwards.
func (a *App) remoteSaveConf(rc *remoteConfig) {
	remoteConfMu.Lock()
	defer remoteConfMu.Unlock()
	if a.store != nil {
		a.store.setRemote(rc)
	}
	remoteConfCache = rc
}

// remotePlainPassword returns the account's plaintext password. It lives in
// memory only, so after a restart it is unknown. The account then starts
// over with the default password rather than a random one the owner could
// not know: the management page changes it, and every restart without a
// change keeps the last one working.
func (a *App) remotePlainPassword() string {
	remoteConfMu.Lock()
	defer remoteConfMu.Unlock()
	if remotePassPlain != "" {
		return remotePassPlain
	}
	pass := remoteDefaultPassword
	rc := *a.remoteConfLocked()
	rc.PassHash = remote.HashPassword(pass)
	if a.store != nil {
		a.store.setRemote(&rc)
	}
	remoteConfCache = &rc
	remotePassPlain = pass
	return pass
}

// remoteStart serves the OpenList-compatible HTTP API on 0.0.0.0, taking
// conf.Port or the first free port among the ten after it, and persists the
// port it got when it differs. It reports whether the server came up;
// failures land in the status bar.
func (a *App) remoteStart() bool {
	remoteSrvMu.Lock()
	if remoteSrv != nil {
		remoteSrvMu.Unlock()
		return true
	}
	remoteSrvMu.Unlock()

	conf := a.remoteConf()
	base := conf.Port
	if base <= 0 || base > 65535-remotePortCount {
		base = 5244
	}
	var (
		ln   net.Listener
		port int
	)
	for p := base; p <= base+remotePortCount; p++ {
		if l, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", strconv.Itoa(p))); err == nil {
			ln, port = l, p
			break
		}
	}
	if ln == nil {
		mygo.RunOnMain(func() {
			a.statusMsg = fmt.Sprintf("远程访问启动失败：端口 %d-%d 均被占用", base, base+remotePortCount)
			repaint()
		})
		return false
	}
	srv := &http.Server{Handler: a.remoteMux()}
	remoteSrvMu.Lock()
	remoteSrv = srv
	remoteSrvMu.Unlock()
	if port != conf.Port {
		nc := *conf
		nc.Port = port
		a.remoteSaveConf(&nc)
	}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			remoteSrvMu.Lock()
			if remoteSrv == srv {
				remoteSrv = nil
			}
			remoteSrvMu.Unlock()
		}
	}()
	return true
}

// remoteStop shuts the server down gracefully, waiting at most three
// seconds for the requests in flight, and invalidates every issued login.
func (a *App) remoteStop() {
	remoteSrvMu.Lock()
	srv := remoteSrv
	remoteSrv = nil
	remoteSrvMu.Unlock()
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	remoteLiveMu.Lock()
	remoteLiveTokens = map[string]struct{}{}
	remoteLiveMu.Unlock()
}

// remoteMux routes the OpenList v3 API surface plus Sunvale's own
// extensions, behind remoteCORS. The /api/fs/*, /d/, /p/, /ping and /
// handlers live in remote_fs.go, remote_ext.go, remote_dl.go and
// remote_web.go; the fs-write, share, /sd/, /@manage and /api/sunvale/*
// handlers are registered below and live in their own files,
// remote_admin.go among them.
func (a *App) remoteMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/login", a.remoteHandleLogin)
	mux.HandleFunc("/api/auth/login/hash", a.remoteHandleLoginHash)
	mux.HandleFunc("/api/me", a.remoteHandleMe)
	mux.HandleFunc("/api/auth/logout", a.remoteHandleLogout)
	mux.HandleFunc("/api/public/settings", a.remoteHandleSettings)
	mux.HandleFunc("/api/public/archive_extensions", a.remoteHandleArchiveExt)
	mux.HandleFunc("/api/fs/list", a.remoteHandleFSList)
	mux.HandleFunc("/api/fs/get", a.remoteHandleFSGet)
	mux.HandleFunc("/api/fs/search", a.remoteHandleFSSearch)
	mux.HandleFunc("/api/fs/preview", a.remoteHandleFSPreview)
	mux.HandleFunc("/d/", a.remoteHandleDown)
	mux.HandleFunc("/p/", a.remoteHandleProxy)
	mux.HandleFunc("/ping", a.remoteHandlePing)
	mux.HandleFunc("/api/fs/mkdir", a.remoteHandleMkdir)
	mux.HandleFunc("/api/fs/rename", a.remoteHandleRename)
	mux.HandleFunc("/api/fs/batch_rename", a.remoteHandleBatchRename)
	mux.HandleFunc("/api/fs/regex_rename", a.remoteHandleRegexRename)
	mux.HandleFunc("/api/fs/move", a.remoteHandleMove)
	mux.HandleFunc("/api/fs/copy", a.remoteHandleCopy)
	mux.HandleFunc("/api/fs/remove", a.remoteHandleRemove)
	mux.HandleFunc("/api/fs/dirs", a.remoteHandleDirs)
	mux.HandleFunc("/api/fs/put", a.remoteHandlePut)
	mux.HandleFunc("/api/fs/archive/unzip", a.remoteHandleArchiveUnzip)
	mux.HandleFunc("/api/fs/archive/zip", a.remoteHandleArchiveZip)
	mux.HandleFunc("/api/fs/ext/list", a.remoteHandleExtList)
	mux.HandleFunc("/api/fs/ext/run", a.remoteHandleExtRun)
	mux.HandleFunc("/api/fs/terminal", a.remoteHandleTerminal)
	mux.HandleFunc("/api/share/create", a.remoteHandleShareCreate)
	mux.HandleFunc("/api/share/list", a.remoteHandleShareList)
	mux.HandleFunc("/api/share/get", a.remoteHandleShareGet)
	mux.HandleFunc("/api/share/update", a.remoteHandleShareUpdate)
	mux.HandleFunc("/api/share/delete", a.remoteHandleShareDelete)
	mux.HandleFunc("/api/share/enable", a.remoteHandleShareEnable)
	mux.HandleFunc("/api/share/disable", a.remoteHandleShareDisable)
	mux.HandleFunc("/sd/", a.remoteHandleShareDown)

	mux.HandleFunc("/api/fs/storage_list", a.remoteHandleFSStorageList)
	mux.HandleFunc("/api/fs/hidden", a.remoteHandleHidden)
	// OpenList's storage and driver APIs: the mount table the remote tree
	// is served from, and the driver form the create page renders.
	mux.HandleFunc("/api/storage/list", a.remoteHandleStorageList)
	mux.HandleFunc("/api/storage/get", a.remoteHandleStorageGet)
	mux.HandleFunc("/api/storage/create", a.remoteHandleStorageCreate)
	mux.HandleFunc("/api/storage/update", a.remoteHandleStorageUpdate)
	mux.HandleFunc("/api/storage/delete", a.remoteHandleStorageDelete)
	mux.HandleFunc("/api/storage/enable", a.remoteHandleStorageEnable)
	mux.HandleFunc("/api/storage/disable", a.remoteHandleStorageDisable)
	mux.HandleFunc("/api/storage/load_all", a.remoteHandleStorageLoadAll)
	mux.HandleFunc("/api/driver/list", a.remoteHandleDriverList)
	mux.HandleFunc("/api/driver/names", a.remoteHandleDriverNames)
	mux.HandleFunc("/api/driver/info", a.remoteHandleDriverInfo)

	mux.HandleFunc("/api/sunvale/status", a.remoteHandleStatus)
	mux.HandleFunc("/api/sunvale/account", a.remoteHandleAccount)
	mux.HandleFunc("/api/sunvale/reset-token", a.remoteHandleResetToken)
	mux.HandleFunc("/api/sunvale/port", a.remoteHandlePort)
	mux.HandleFunc("/api/sunvale/enabled", a.remoteHandleEnabled)
	// Agent Card discovery is public; execution and task access require the
	// administrator token. Both protocol aliases share the same handlers.
	mux.HandleFunc("/.well-known/agent-card.json", a.remoteHandleA2ACard)
	mux.HandleFunc("/.well-known/agent.json", a.remoteHandleA2ACard)
	mux.HandleFunc("/a2a", a.remoteHandleA2A)
	mux.HandleFunc("/a2a/v1", a.remoteHandleA2A)
	mux.Handle("/mcp", a.assistantHTTPServer())
	mux.Handle("/mcp/", a.assistantHTTPServer())
	mux.HandleFunc("/@manage", a.remoteHandleManage)
	mux.HandleFunc("/@manage/", a.remoteHandleManage)
	mux.HandleFunc("/", a.remoteHandleIndex)
	return remote.CORS(mux)
}

// remoteToggle flips remote access on or off, from the 查看更多 menu.
func (a *App) remoteToggle() {
	conf := a.remoteConf()
	nc := *conf
	nc.Enabled = !conf.Enabled
	if nc.Enabled {
		a.remoteSaveConf(&nc)
		if a.remoteStart() {
			a.statusMsg = "远程访问已开启：" + a.remoteLANURL()
		}
		return
	}
	a.remoteSaveConf(&nc)
	a.remoteStop()
	a.statusMsg = "远程访问已关闭"
}

// remoteLANURL is the address a phone on the LAN opens to reach the service.
func (a *App) remoteLANURL() string {
	port := a.remoteConf().Port
	if ip := remote.LANIP(); ip != "" {
		return "http://" + net.JoinHostPort(ip, strconv.Itoa(port))
	}
	return "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

// remoteIsAdmin reports whether the request carries the administrator's
// credentials: the raw config token in the Authorization header, or one of
// the JWTs this run issued that is still signed and unexpired.
func (a *App) remoteIsAdmin(r *http.Request) bool {
	token := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(token) > 7 && strings.EqualFold(token[:7], "Bearer ") {
		token = strings.TrimSpace(token[7:])
	}
	if token == "" {
		return false
	}
	conf := a.remoteConf()
	if token == conf.Token {
		return true
	}
	remoteLiveMu.Lock()
	_, live := remoteLiveTokens[token]
	remoteLiveMu.Unlock()
	return live && remote.VerifyJWT(conf.Token, token)
}
