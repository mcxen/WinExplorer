// Package applog is Sunvale's diagnostic log: structured log/slog records
// written to a size-rotated file in the per-user log folder, plus stderr
// when a console exists. Secrets (API keys, bearer tokens, Authorization
// headers, key=… query values) are redacted before anything is written.
package applog

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// FileName is the active log; rotated files are FileName.1 … .N.
	FileName = "sunvale.log"
	// CrashName receives Go runtime fatal errors (debug.SetCrashOutput).
	CrashName = "crash.log"
	// DefaultMaxSize and DefaultBackups: 5 MB × 5 files.
	DefaultMaxSize = 5 << 20
	DefaultBackups = 5
)

// Dir is the per-user log folder: %LOCALAPPDATA%\Sunvale\logs on Windows,
// ~/Library/Logs/Sunvale on macOS, $XDG_STATE_HOME/sunvale/logs (default
// ~/.local/state/sunvale/logs) elsewhere. SUNVALE_LOG_DIR overrides it.
func Dir() string {
	if d := os.Getenv("SUNVALE_LOG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "Sunvale", "logs")
		}
		if d, err := os.UserCacheDir(); err == nil {
			return filepath.Join(d, "Sunvale", "logs")
		}
	case "darwin":
		if home != "" {
			return filepath.Join(home, "Library", "Logs", "Sunvale")
		}
	default:
		if d := os.Getenv("XDG_STATE_HOME"); d != "" {
			return filepath.Join(d, "sunvale", "logs")
		}
		if home != "" {
			return filepath.Join(home, ".local", "state", "sunvale", "logs")
		}
	}
	return filepath.Join(os.TempDir(), "sunvale-logs")
}

// Rotator is an io.Writer over a file that rolls at MaxSize, keeping
// Backups older files (name.1 newest).
type Rotator struct {
	mu      sync.Mutex
	path    string
	maxSize int64
	backups int
	f       *os.File
	size    int64
}

// NewRotator opens (appending) path.
func NewRotator(path string, maxSize int64, backups int) (*Rotator, error) {
	r := &Rotator{path: path, maxSize: maxSize, backups: backups}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Rotator) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	st, _ := f.Stat()
	r.f, r.size = f, 0
	if st != nil {
		r.size = st.Size()
	}
	return nil
}

func (r *Rotator) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return 0, os.ErrClosed
	}
	if r.size > 0 && r.size+int64(len(p)) > r.maxSize {
		r.rotate()
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *Rotator) rotate() {
	r.f.Close()
	os.Remove(fmt.Sprintf("%s.%d", r.path, r.backups))
	for i := r.backups - 1; i >= 1; i-- {
		os.Rename(fmt.Sprintf("%s.%d", r.path, i), fmt.Sprintf("%s.%d", r.path, i+1))
	}
	if r.backups > 0 {
		os.Rename(r.path, r.path+".1")
	} else {
		os.Remove(r.path)
	}
	if err := r.open(); err != nil {
		r.f = nil
	}
}

// Close closes the file.
func (r *Rotator) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}

// Path is the active file.
func (r *Rotator) Path() string { return r.path }

// quiet swallows write errors: a GUI-subsystem Windows process has an
// invalid stderr, which must not break the file side.
type quiet struct{ w io.Writer }

func (q quiet) Write(p []byte) (int, error) { q.w.Write(p); return len(p), nil }

var (
	level   = new(slog.LevelVar)
	mu      sync.Mutex
	rot     *Rotator
	logPath string
)

// Init sets up the default slog logger and the std log package to write to
// the rotating file in dir (and stderr when withStderr). Safe to call once
// early in main; failure to open the file still leaves stderr logging.
func Init(dir string, withStderr bool) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	if s := os.Getenv("SUNVALE_LOG"); s != "" {
		level.Set(ParseLevel(s))
	}
	var ws []io.Writer
	path := filepath.Join(dir, FileName)
	r, err := NewRotator(path, DefaultMaxSize, DefaultBackups)
	if err == nil {
		rot, logPath = r, path
		ws = append(ws, r)
		if cf, cerr := os.OpenFile(filepath.Join(dir, CrashName), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); cerr == nil {
			_ = debug.SetCrashOutput(cf, debug.CrashOptions{})
			cf.Close()
		}
	}
	if withStderr || err != nil {
		ws = append(ws, quiet{os.Stderr})
	}
	out := quiet{io.MultiWriter(ws...)}
	h := &redactHandler{slog.NewTextHandler(out, &slog.HandlerOptions{Level: level, AddSource: false})}
	slog.SetDefault(slog.New(h))
	// slog.SetDefault routes log.Printf through the handler at Info.
	log.SetFlags(0)
	return path, err
}

// Close flushes and closes the file.
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if rot != nil {
		rot.Close()
	}
}

// Path is the active log file ("" before Init succeeded).
func Path() string { mu.Lock(); defer mu.Unlock(); return logPath }

// SetLevel changes the minimum level at run time.
func SetLevel(l slog.Level) { level.Set(l) }

// Level is the current minimum level.
func Level() slog.Level { return level.Level() }

// Levels are the settings-dropdown names, in order.
var Levels = []string{"error", "warn", "info", "debug"}

// ParseLevel reads error/warn/info/debug (default info).
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "error":
		return slog.LevelError
	case "warn", "warning":
		return slog.LevelWarn
	case "debug":
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

// LevelName is ParseLevel's inverse.
func LevelName(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return "error"
	case l >= slog.LevelWarn:
		return "warn"
	case l >= slog.LevelInfo:
		return "info"
	}
	return "debug"
}

// Recover logs a panic with its stack; use as `defer applog.Recover("where")`.
// It re-panics when repanic is true (top level), otherwise swallows.
func Recover(where string, repanic bool) {
	if p := recover(); p != nil {
		LogPanic(where, p)
		if repanic {
			panic(p)
		}
	}
}

// LogPanic writes one panic record with the goroutine stack.
func LogPanic(where string, p any) {
	slog.Error("panic", "where", where, "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
}

// Go runs fn in a goroutine that logs (and swallows) a panic.
func Go(where string, fn func()) {
	go func() {
		defer Recover(where, false)
		fn()
	}()
}

// Files lists the log folder's files (active, rotated, crash), sorted.
func Files(dir string) []string {
	ents, _ := os.ReadDir(dir)
	var out []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "sunvale.log") || e.Name() == CrashName {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

// ---- redaction ----

var redactREs = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?i)(authorization|proxy-authorization)(["']?\s*[:=]\s*["']?)(bearer\s+|basic\s+)?[^\s"',;]+`), "${1}${2}${3}[REDACTED]"},
	{regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{8,}`), "Bearer [REDACTED]"},
	{regexp.MustCompile(`(?i)((?:api[_-]?key|apikey|access[_-]?token|refresh[_-]?token|token|secret|password|passwd|x-api-key|x-goog-api-key)["']?\s*[:=]\s*["']?)[^\s"'&,;}]+`), "${1}[REDACTED]"},
	{regexp.MustCompile(`([?&](?:key|api_key|apikey|token|access_token|sig|signature)=)[^&\s"]+`), "${1}[REDACTED]"},
	{regexp.MustCompile(`\b(sk|pk|rk)-[A-Za-z0-9_-]{12,}`), "${1}-[REDACTED]"},
	{regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}`), "gh_[REDACTED]"},
	{regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{20,}`), "AIza[REDACTED]"},
	{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(-----END [A-Z ]*PRIVATE KEY-----|$)`), "[REDACTED PRIVATE KEY]"},
}

// Redact masks secrets in s.
func Redact(s string) string {
	for _, r := range redactREs {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}

// Trunc redacts and caps s at n runes.
func Trunc(s string, n int) string {
	s = Redact(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + fmt.Sprintf("…(+%d)", len(r)-n)
	}
	return s
}

var secretKeys = regexp.MustCompile(`(?i)(api.?key|token|secret|password|authorization|cookie)`)

type redactHandler struct{ slog.Handler }

func (h *redactHandler) Handle(ctx context.Context, r slog.Record) error {
	nr := slog.NewRecord(r.Time, r.Level, Redact(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		nr.AddAttrs(redactAttr(a))
		return true
	})
	return h.Handler.Handle(ctx, nr)
}

func redactAttr(a slog.Attr) slog.Attr {
	if secretKeys.MatchString(a.Key) && a.Value.Kind() != slog.KindGroup {
		if a.Value.String() == "" {
			return a
		}
		return slog.String(a.Key, "[REDACTED]")
	}
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, Redact(a.Value.String()))
	case slog.KindAny:
		if err, ok := a.Value.Any().(error); ok {
			return slog.String(a.Key, Redact(err.Error()))
		}
		return slog.String(a.Key, Redact(a.Value.String()))
	case slog.KindGroup:
		var as []any
		for _, g := range a.Value.Group() {
			as = append(as, redactAttr(g))
		}
		return slog.Group(a.Key, as...)
	}
	return a
}

func (h *redactHandler) WithAttrs(as []slog.Attr) slog.Handler {
	out := make([]slog.Attr, len(as))
	for i, a := range as {
		out[i] = redactAttr(a)
	}
	return &redactHandler{h.Handler.WithAttrs(out)}
}

func (h *redactHandler) WithGroup(n string) slog.Handler { return &redactHandler{h.Handler.WithGroup(n)} }

// Since is a millisecond duration attr.
func Since(t time.Time) slog.Attr { return slog.Int64("ms", time.Since(t).Milliseconds()) }
