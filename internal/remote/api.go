package remote

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// StaticSalt is OpenList's StaticHash salt: the password on disk and
// the one /api/auth/login/hash receives are both
// sha256(password + "-https://github.com/alist-org/alist"), lowercase hex.
const remoteStaticSalt = "-https://github.com/alist-org/alist"

// TokenTTL is how long an issued login JWT stays valid.
const remoteTokenTTL = 48 * time.Hour

// CORS answers the preflight and stamps every reply with the wildcard
// headers OpenList's default Cors config sends, so a script or a page
// hosted elsewhere can call the API. No credentials travel in headers the
// browser would restrict: the token rides in Authorization, which the
// preflight below allows.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "*")
		h.Set("Access-Control-Allow-Headers", "*")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// LANIP picks the first non-loopback IPv4 address of the machine.
func LANIP() string {
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

// Resp is the envelope every /api/* reply uses. The HTTP status is
// always 200, errors included, as the protocol prescribes.
type remoteResp struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

// OK writes a success envelope.
func OK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(remoteResp{Code: 200, Message: "success", Data: data})
}

// Err writes an error envelope; code is the protocol code in the
// body, never the HTTP status.
func Err(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(remoteResp{Code: code, Message: msg})
}

// SignJWT issues the HS256 login token OpenList clients carry in the
// Authorization header; secret is the config token.
func SignJWT(secret string, now time.Time) string {
	header := `{"alg":"HS256","typ":"JWT"}`
	claims := fmt.Sprintf(`{"username":"admin","pwd_ts":%d,"iat":%d,"exp":%d}`,
		now.Unix(), now.Unix(), now.Add(remoteTokenTTL).Unix())
	payload := base64.RawURLEncoding.EncodeToString([]byte(header)) + "." +
		base64.RawURLEncoding.EncodeToString([]byte(claims))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyJWT checks a token's signature and expiry.
func VerifyJWT(secret, token string) bool {
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

// HashPassword is OpenList's StaticHash of a password.
func HashPassword(pass string) string {
	sum := sha256.Sum256([]byte(pass + remoteStaticSalt))
	return hex.EncodeToString(sum[:])
}

// RandString is a random [A-Za-z0-9] string, used for the password.
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

// RandToken is the 64-hex master token.
func RandToken() string {
	var b [32]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
