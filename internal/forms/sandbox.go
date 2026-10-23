package forms

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// sandbox.go keeps OpenUI / schedule demos off non-existent ~/Downloads
// and ~/Documents (common on headless CI boxes). PreferPath returns the
// real home folder when it exists; otherwise it creates a temp sandbox
// with sample files and points there.

var (
	demoOnce sync.Once
	demoRoot string
	demoErr  error
)

// EnsureDemoSandbox creates <TempDir>/sunvale-openui-demo with Downloads,
// Documents (and common subfolders), Backups, Movies, plus a few sample
// files. Safe to call repeatedly.
func EnsureDemoSandbox() string {
	demoOnce.Do(func() {
		root := filepath.Join(os.TempDir(), "sunvale-openui-demo")
		dirs := []string{
			filepath.Join(root, "Downloads"),
			filepath.Join(root, "Documents"),
			filepath.Join(root, "Documents", "来自下载"),
			filepath.Join(root, "Documents", "文档"),
			filepath.Join(root, "Documents", "图片"),
			filepath.Join(root, "Documents", "压缩包"),
			filepath.Join(root, "Backups"),
			filepath.Join(root, "Movies"),
		}
		for _, d := range dirs {
			if err := os.MkdirAll(d, 0o755); err != nil {
				demoErr = err
				return
			}
		}
		files := map[string]string{
			filepath.Join(root, "Downloads", "会议纪要.md"):      "# 会议纪要\n",
			filepath.Join(root, "Downloads", "IMG_0001.jpg"): "\xff\xd8\xff demo",
			filepath.Join(root, "Downloads", "资料.zip"):       "PK\x03\x04 demo",
			filepath.Join(root, "Documents", "报告.pdf"):       "%PDF-1.4 demo",
			filepath.Join(root, "Movies", "demo.mp4"):        "ftypmp42",
		}
		for path, body := range files {
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				demoErr = err
				return
			}
		}
		demoRoot = root
	})
	return demoRoot
}

// DemoSandboxErr is set if EnsureDemoSandbox failed (tests may check).
func DemoSandboxErr() error { EnsureDemoSandbox(); return demoErr }

// PreferPath returns ~/rel if that path exists; otherwise the same relative
// path under EnsureDemoSandbox(). rel uses forward segments like "Downloads"
// or "Documents/报告.pdf".
func PreferPath(rel string) string {
	rel = filepath.Clean(rel)
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		p := filepath.Join(home, rel)
		if pathUsable(p) {
			return p
		}
		// File under a missing tree: still fall through to sandbox.
	}
	root := EnsureDemoSandbox()
	if root == "" {
		if home != "" {
			return filepath.Join(home, rel)
		}
		return rel
	}
	return filepath.Join(root, rel)
}

// ExistingDir returns path if it is an existing directory; otherwise walks
// parents, then PreferPath("Downloads"), then home, then TempDir.
func ExistingDir(path string) string {
	path = strings.TrimSpace(path)
	if path != "" {
		if st, err := os.Stat(path); err == nil {
			if st.IsDir() {
				return path
			}
			return filepath.Dir(path)
		}
		for p := path; p != "" && p != "." && p != string(filepath.Separator); p = filepath.Dir(p) {
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				return p
			}
			if filepath.Dir(p) == p {
				break
			}
		}
	}
	if d := PreferPath("Downloads"); pathUsable(d) {
		return d
	}
	if home, err := os.UserHomeDir(); err == nil && pathUsable(home) {
		return home
	}
	return os.TempDir()
}

func pathUsable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && (st.IsDir() || st.Mode().IsRegular())
}
