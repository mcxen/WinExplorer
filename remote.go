package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

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

// remoteStaticSalt is OpenList's StaticHash salt: the password on disk and
// the one /api/auth/login/hash receives are both
// sha256(password + "-https://github.com/alist-org/alist"), lowercase hex.
const remoteStaticSalt = "-https://github.com/alist-org/alist"

// remoteTokenTTL is how long an issued login JWT stays valid.
const remoteTokenTTL = 48 * time.Hour

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
// account on first use — port 5244, the admin user, a random 10-character
// password and a random 64-hex token — and persisting it.
func (a *App) remoteConf() *remoteConfig {
	remoteConfMu.Lock()
	defer remoteConfMu.Unlock()
	return a.remoteConfLocked()
}

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
	pass := remoteRandString(10)
	rc := &remoteConfig{
		Port:     5244,
		Username: "admin",
		PassHash: remoteHashPassword(pass),
		Token:    remoteRandToken(),
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
// memory only, so after a restart it is unknown; a fresh one is generated
// then, replacing the stored hash, so a copy always carries a password that
// works.
func (a *App) remotePlainPassword() string {
	remoteConfMu.Lock()
	defer remoteConfMu.Unlock()
	if remotePassPlain != "" {
		return remotePassPlain
	}
	pass := remoteRandString(10)
	rc := *a.remoteConfLocked()
	rc.PassHash = remoteHashPassword(pass)
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
// extensions. The /api/fs/*, /d/, /p/, /ping and / handlers live in
// remote_fs.go, remote_ext.go, remote_dl.go and remote_web.go; the
// fs-write, share, /sd/, /@manage and /api/sunvale/* handlers are
// registered below and live in their own files, remote_admin.go among them.
func (a *App) remoteMux() *http.ServeMux {
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
	mux.HandleFunc("/d/", a.remoteHandleDown)
	mux.HandleFunc("/p/", a.remoteHandleProxy)
	mux.HandleFunc("/ping", a.remoteHandlePing)
	mux.HandleFunc("/api/fs/mkdir", a.remoteHandleMkdir)
	mux.HandleFunc("/api/fs/rename", a.remoteHandleRename)
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
	mux.HandleFunc("/api/sunvale/status", a.remoteHandleStatus)
	mux.HandleFunc("/api/sunvale/account", a.remoteHandleAccount)
	mux.HandleFunc("/api/sunvale/reset-token", a.remoteHandleResetToken)
	mux.HandleFunc("/api/sunvale/port", a.remoteHandlePort)
	mux.HandleFunc("/api/sunvale/enabled", a.remoteHandleEnabled)
	mux.HandleFunc("/@manage", a.remoteHandleManage)
	mux.HandleFunc("/@manage/", a.remoteHandleManage)
	mux.HandleFunc("/", a.remoteHandleIndex)
	return mux
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
	if ip := remoteLANIP(); ip != "" {
		return "http://" + net.JoinHostPort(ip, strconv.Itoa(port))
	}
	return "http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
}

// remoteLANIP picks the first non-loopback IPv4 address of the machine.
func remoteLANIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if ip4 := ipnet.IP.To4(); ip4 != nil && !ip4.IsLoopback() {
				return ip4.String()
			}
		}
	}
	return ""
}

// remoteIsAdmin reports whether the request carries the administrator's
// credentials: the raw config token in the Authorization header, or one of
// the JWTs this run issued that is still signed and unexpired.
func (a *App) remoteIsAdmin(r *http.Request) bool {
	token := r.Header.Get("Authorization")
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
	return live && remoteVerifyJWT(conf.Token, token)
}

// remoteResp is the envelope every /api/* reply uses. The HTTP status is
// always 200, errors included, as the protocol prescribes.
type remoteResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

// remoteOK writes a success envelope.
func remoteOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(remoteResp{Code: 200, Message: "success", Data: data})
}

// remoteErr writes an error envelope; code is the protocol code in the
// body, never the HTTP status.
func remoteErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(remoteResp{Code: code, Message: msg})
}

// remoteSignJWT issues the HS256 login token OpenList clients carry in the
// Authorization header; secret is the config token.
func remoteSignJWT(secret string, now time.Time) string {
	header := `{"alg":"HS256","typ":"JWT"}`
	claims := fmt.Sprintf(`{"username":"admin","pwd_ts":%d,"iat":%d,"exp":%d}`,
		now.Unix(), now.Unix(), now.Add(remoteTokenTTL).Unix())
	payload := base64.RawURLEncoding.EncodeToString([]byte(header)) + "." +
		base64.RawURLEncoding.EncodeToString([]byte(claims))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// remoteVerifyJWT checks a token's signature and expiry.
func remoteVerifyJWT(secret, token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return false
	}
	return time.Now().Unix() < claims.Exp
}

// remoteHashPassword is OpenList's StaticHash of a password.
func remoteHashPassword(pass string) string {
	sum := sha256.Sum256([]byte(pass + remoteStaticSalt))
	return hex.EncodeToString(sum[:])
}

// remoteRandString is a random [A-Za-z0-9] string, used for the password.
func remoteRandString(n int) string {
	const chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	limit := 256 - 256%len(chars) // rejection keeps every character as likely
	out := make([]byte, 0, n)
	buf := make([]byte, n+8)
	for len(out) < n {
		rand.Read(buf)
		for _, c := range buf {
			if int(c) < limit {
				out = append(out, chars[int(c)%len(chars)])
				if len(out) == n {
					break
				}
			}
		}
	}
	return string(out)
}

// remoteRandToken is the 64-hex master token.
func remoteRandToken() string {
	var b [32]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
