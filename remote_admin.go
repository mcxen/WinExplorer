package main

import (
	"net/http"
)

// This file is Sunvale's own management API, the /api/sunvale/* endpoints
// the built-in 管理 page drives. They are not part of OpenList's client
// surface: every one is admin-only, answers HTTP 200 with the
// {code,message,data} envelope, and carries what no OpenList client may
// touch — the account, the master token, the port and the on/off switch.

// remoteAdminVersion is what /api/sunvale/status reports, the same string
// /api/public/settings carries as "version".
const remoteAdminVersion = "0.1.0 (Sunvale)"

// remoteAdminDisk is the home volume's capacity in the status reply.
type remoteAdminDisk struct {
	Total int64 `json:"total"`
	Free  int64 `json:"free"`
}

// remoteAdminStatus is /api/sunvale/status's data.
type remoteAdminStatus struct {
	Enabled  bool            `json:"enabled"`
	Port     int             `json:"port"`
	URL      string          `json:"url"`
	Version  string          `json:"version"`
	Username string          `json:"username"`
	Disk     remoteAdminDisk `json:"disk"`
	Shares   int             `json:"shares"`
}

// remoteAdminAccount is /api/sunvale/account's data.
type remoteAdminAccount struct {
	Username string `json:"username"`
}

// remoteAdminToken is /api/sunvale/reset-token's data.
type remoteAdminToken struct {
	Token string `json:"token"`
}

// remoteAdminPort is /api/sunvale/port's data.
type remoteAdminPort struct {
	Port int `json:"port"`
}

// remoteAdminEnabled is /api/sunvale/enabled's data.
type remoteAdminEnabled struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
}

// remoteHandleStatus implements GET /api/sunvale/status: the whole remote
// setup at a glance — enabled, port, LAN address, account, the home
// volume's capacity and the number of live shares.
func (a *App) remoteHandleStatus(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	conf := a.remoteConf()
	var disk remoteAdminDisk
	if free, total, ok := volumeCap(a.homePath); ok {
		disk.Free, disk.Total = free, total
	}
	shares := 0
	if a.store != nil {
		shares = len(a.store.shareAll())
	}
	remoteOK(w, remoteAdminStatus{
		Enabled:  conf.Enabled,
		Port:     conf.Port,
		URL:      a.remoteLANURL(),
		Version:  remoteAdminVersion,
		Username: conf.Username,
		Disk:     disk,
		Shares:   shares,
	})
}

// remoteHandleAccount implements POST /api/sunvale/account: change the
// admin username, the password or both. A new password rehashes, replaces
// the in-memory plaintext the 复制地址 menu copies, and retires every
// login JWT issued so far — the caller must log in again; the raw master
// token keeps working.
func (a *App) remoteHandleAccount(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := remoteReadJSON(r, &req); err != nil {
		remoteErr(w, 400, "bad request")
		return
	}
	if req.Username == "" && req.Password == "" {
		remoteErr(w, 400, "nothing to change")
		return
	}
	if req.Username != "" && !remoteValidAccountName(req.Username) {
		remoteErr(w, 400, "invalid username")
		return
	}
	conf := a.remoteConf()
	nc := *conf
	if req.Username != "" {
		nc.Username = req.Username
	}
	if req.Password != "" {
		nc.PassHash = remoteHashPassword(req.Password)
	}
	a.remoteSaveConf(&nc)
	if req.Password != "" {
		remoteConfMu.Lock()
		remotePassPlain = req.Password
		remoteConfMu.Unlock()
		remoteInvalidateLogins()
	}
	remoteOK(w, remoteAdminAccount{Username: nc.Username})
}

// remoteHandleResetToken implements POST /api/sunvale/reset-token: replace
// the master token with a fresh random one. Everything signed with the old
// secret dies with it — the live login JWTs, and every /d and /sd sign
// issued under the old key. Only the new token works from here on.
func (a *App) remoteHandleResetToken(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	conf := a.remoteConf()
	nc := *conf
	nc.Token = remoteRandToken()
	a.remoteSaveConf(&nc)
	remoteInvalidateLogins()
	remoteOK(w, remoteAdminToken{Token: nc.Token})
}

// remoteHandlePort implements POST /api/sunvale/port: move the server to a
// new port, restarting it when it is running. remoteStart walks up to nine
// ports past the requested one when they are taken; when the whole range
// is busy the old port is restored, the server comes back up on it, and
// the request answers 500 "port in use".
func (a *App) remoteHandlePort(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	var req struct {
		Port int `json:"port"`
	}
	if err := remoteReadJSON(r, &req); err != nil {
		remoteErr(w, 400, "bad request")
		return
	}
	if req.Port < 1 || req.Port > 65535 {
		remoteErr(w, 400, "invalid port")
		return
	}
	conf := a.remoteConf()
	oldPort := conf.Port
	wasRunning := remoteServing()
	if wasRunning {
		a.remoteStop()
	}
	nc := *conf
	nc.Port = req.Port
	a.remoteSaveConf(&nc)
	if wasRunning && !a.remoteStart() {
		rb := *a.remoteConf()
		rb.Port = oldPort
		a.remoteSaveConf(&rb)
		a.remoteStart()
		remoteErr(w, 500, "port in use")
		return
	}
	remoteOK(w, remoteAdminPort{Port: a.remoteConf().Port})
}

// remoteHandleEnabled implements POST /api/sunvale/enabled: turn remote
// access on or off. Both remoteStart and remoteStop are idempotent, so
// repeating a state is safe; the answer carries the LAN address a phone
// opens after switching on.
func (a *App) remoteHandleEnabled(w http.ResponseWriter, r *http.Request) {
	if !a.remoteIsAdmin(r) {
		remoteErr(w, 401, "Guest user is disabled, login please")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := remoteReadJSON(r, &req); err != nil {
		remoteErr(w, 400, "bad request")
		return
	}
	conf := a.remoteConf()
	nc := *conf
	nc.Enabled = req.Enabled
	a.remoteSaveConf(&nc)
	if req.Enabled {
		a.remoteStart()
	} else {
		a.remoteStop()
	}
	remoteOK(w, remoteAdminEnabled{Enabled: nc.Enabled, URL: a.remoteLANURL()})
}

// remoteInvalidateLogins retires every login JWT issued this run; the raw
// master token keeps working, and so do fresh logins.
func remoteInvalidateLogins() {
	remoteLiveMu.Lock()
	remoteLiveTokens = map[string]struct{}{}
	remoteLiveMu.Unlock()
}

// remoteServing reports whether the remote server is up.
func remoteServing() bool {
	remoteSrvMu.Lock()
	defer remoteSrvMu.Unlock()
	return remoteSrv != nil
}

// remoteValidAccountName accepts 1..32 letters, digits, underscores and
// hyphens for the admin username.
func remoteValidAccountName(name string) bool {
	if name == "" || len(name) > 32 {
		return false
	}
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}
