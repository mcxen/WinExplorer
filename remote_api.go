package main

import (
	"net/http"
	"time"

	"sunvale/internal/remote"
)

// remoteLoginReq is the body of both login endpoints: the plaintext
// password with /api/auth/login, its StaticHash with /api/auth/login/hash.
type remoteLoginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// remoteHandleLogin implements POST /api/auth/login: username plus the
// plaintext password, whose StaticHash must match the stored one.
func (a *App) remoteHandleLogin(w http.ResponseWriter, r *http.Request) {
	var req remoteLoginReq
	if err := remote.ReadJSON(r, &req); err != nil {
		remote.Err(w, 400, err.Error())
		return
	}
	if req.Username == "" {
		remote.Err(w, 400, "username is required")
		return
	}
	conf := a.remoteConf()
	if req.Username != conf.Username || remote.HashPassword(req.Password) != conf.PassHash {
		remote.Err(w, 401, "Invalid username or password")
		return
	}
	a.remoteLoginOK(w)
}

// remoteHandleLoginHash implements POST /api/auth/login/hash, which the
// official web UI prefers: the password field already carries the
// StaticHash, compared as it is.
func (a *App) remoteHandleLoginHash(w http.ResponseWriter, r *http.Request) {
	var req remoteLoginReq
	if err := remote.ReadJSON(r, &req); err != nil {
		remote.Err(w, 400, err.Error())
		return
	}
	if req.Username == "" {
		remote.Err(w, 400, "username is required")
		return
	}
	conf := a.remoteConf()
	if req.Username != conf.Username || req.Password != conf.PassHash {
		remote.Err(w, 401, "Invalid username or password")
		return
	}
	a.remoteLoginOK(w)
}

// remoteLoginOK issues the login JWT, marks it live and answers with it.
func (a *App) remoteLoginOK(w http.ResponseWriter) {
	conf := a.remoteConf()
	token := remote.SignJWT(conf.Token, time.Now())
	remoteLiveMu.Lock()
	remoteLiveTokens[token] = struct{}{}
	remoteLiveMu.Unlock()
	remote.OK(w, map[string]string{"token": token})
}

// remoteMeUser is /api/me's payload, in OpenList's field order. A disabled
// guest makes the clients show the login screen instead of a 401.
type remoteMeUser struct {
	ID         int    `json:"id"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	BasePath   string `json:"base_path"`
	Role       int    `json:"role"`
	Disabled   bool   `json:"disabled"`
	Permission int    `json:"permission"`
	SsoID      string `json:"sso_id"`
	AllowLDAP  bool   `json:"allow_ldap"`
	Otp        bool   `json:"otp"`
}

// remoteHandleMe implements GET /api/me: the admin with a valid identity,
// otherwise a disabled guest.
func (a *App) remoteHandleMe(w http.ResponseWriter, r *http.Request) {
	if a.remoteIsAdmin(r) {
		remote.OK(w, remoteMeUser{
			ID:         1,
			Username:   a.remoteConf().Username,
			BasePath:   "/",
			Role:       2,
			Permission: 29183,
		})
		return
	}
	remote.OK(w, remoteMeUser{
		ID:       2,
		Username: "guest",
		BasePath: "/",
		Role:     1,
		Disabled: true,
	})
}

// remoteHandleLogout implements GET /api/auth/logout: drop the request's
// token from the live set; a missing or unknown one still succeeds.
func (a *App) remoteHandleLogout(w http.ResponseWriter, r *http.Request) {
	if token := r.Header.Get("Authorization"); token != "" {
		remoteLiveMu.Lock()
		delete(remoteLiveTokens, token)
		remoteLiveMu.Unlock()
	}
	remote.OK(w, nil)
}

// remoteSettings is the minimum the OpenList web UI needs from
// /api/public/settings; every value is a string. The private keys (token,
// sign_all, the type lists) are deliberately absent.
var remoteSettings = map[string]string{
	"version":                           "0.1.0 (Sunvale)",
	"site_title":                        "Sunvale",
	"announcement":                      "",
	"pagination_type":                   "ALL",
	"default_page_size":                 "30",
	"allow_indexed":                     "false",
	"allow_mounted":                     "false",
	"robots_txt":                        "",
	"logo":                              "",
	"favicon":                           "",
	"main_color":                        "#0067C0",
	"home_icon":                         "",
	"share_icon":                        "",
	"home_container":                    "",
	"settings_layout":                   "",
	"show_disk_usage_in_plain_text":     "false",
	"external_previews":                 "",
	"iframe_previews":                   "",
	"audio_cover":                       "",
	"audio_autoplay":                    "false",
	"video_autoplay":                    "false",
	"preview_download_by_default":       "true",
	"preview_archives_by_default":       "false",
	"share_preview_download_by_default": "true",
	"share_preview_archives_by_default": "false",
	"readme_autorender":                 "true",
	"filter_readme_scripts":             "false",
	"non_efs_zip_encoding":              "",
	"hide_files":                        "",
	"package_download":                  "true",
	"ocr_api":                           "",
	"filename_char_mapping":             "",
	"forward_direct_link_params":        "false",
	"ignore_direct_link_params":         "",
	"webauthn_login_enabled":            "false",
	"share_preview":                     "true",
	"share_archive_preview":             "false",
	"share_summary_content":             "",
	"search_index":                      "none",
	"auto_update_index":                 "false",
	"search_warmup":                     "false",
	"sso_login_enabled":                 "false",
	"sso_login_platform":                "",
	"sso_compatibility_mode":            "false",
	"ldap_login_enabled":                "false",
	"ldap_login_tips":                   "",
	"multipart_enabled":                 "false",
	"multipart_chunk_size":              "0",
}

// remoteHandleSettings implements GET /api/public/settings, on any method.
func (a *App) remoteHandleSettings(w http.ResponseWriter, r *http.Request) {
	remote.OK(w, remoteSettings)
}
