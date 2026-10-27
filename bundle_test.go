package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMacOSBundleName(t *testing.T) {
	cases := []struct {
		name   string
		bundle bool
		app    bool
	}{
		{"Foo.app", true, true},
		{"Foo.APP", true, true},
		{"Bar.framework", true, false},
		{"X.xcodeproj", true, false},
		{"Lib.photoslibrary", true, false},
		{"notes.zip", false, false},
		{"folder", false, false},
		{"app", false, false},
		{"myapp.txt", false, false},
	}
	for _, c := range cases {
		if got := isMacOSBundleName(c.name); got != c.bundle {
			t.Fatalf("%s: isMacOSBundleName = %v, want %v", c.name, got, c.bundle)
		}
		if got := isAppBundleName(c.name); got != c.app {
			t.Fatalf("%s: isAppBundleName = %v, want %v", c.name, got, c.app)
		}
	}
}

func TestAppBundleNotArchive(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Foo.app")
	if err := os.MkdirAll(filepath.Join(app, "Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := filepath.Join(app, "Contents", "Info.plist")
	if err := os.WriteFile(plist, []byte(`<?xml version="1.0"?><plist></plist>`), 0o644); err != nil {
		t.Fatal(err)
	}

	e := Entry{Name: "Foo.app", Path: app, IsDir: true}
	if entryIsArchive(e) {
		t.Fatal("Foo.app directory must not be an archive")
	}
	if isArchivePath(app) {
		t.Fatal("isArchivePath(.app dir) must be false")
	}
	if isArchive("Foo.app") {
		// Name-only helper still sees no archive suffix — .app is not zip/tar.
		// Kept as a regression lock so nobody adds "app" to archiveKind.
		t.Fatal("isArchive(Foo.app) must be false")
	}

	zipPath := filepath.Join(dir, "pack.zip")
	if err := writeEmptyZip(zipPath); err != nil {
		t.Fatal(err)
	}
	if !isArchivePath(zipPath) {
		t.Fatal("ordinary .zip must still be an archive")
	}
	if !entryIsArchive(Entry{Name: "pack.zip", Path: zipPath, IsDir: false}) {
		t.Fatal("entryIsArchive(.zip) must be true")
	}

	// A directory whose name ends in .zip is still not an archive.
	fake := filepath.Join(dir, "looks.zip")
	if err := os.Mkdir(fake, 0o755); err != nil {
		t.Fatal(err)
	}
	if isArchivePath(fake) {
		t.Fatal("directory named *.zip must not be an archive")
	}
	if entryIsArchive(Entry{Name: "looks.zip", Path: fake, IsDir: true}) {
		t.Fatal("entryIsArchive on dir must be false")
	}
}

func TestBundleTypeLabel(t *testing.T) {
	if got := typeLabel(Entry{Name: "Safari.app", IsDir: true}); got != "应用程序" {
		t.Fatalf("typeLabel(.app) = %q, want 应用程序", got)
	}
	if got := typeLabel(Entry{Name: "普通文件夹", IsDir: true}); got != "文件夹" {
		t.Fatalf("typeLabel(dir) = %q, want 文件夹", got)
	}
	if got := typeLabel(Entry{Name: "a.zip", Ext: "zip"}); got != "ZIP 文件" {
		t.Fatalf("typeLabel(.zip) = %q", got)
	}
	if got := bundleTypeLabel("Foo.framework"); got != "框架" {
		t.Fatalf("framework label = %q", got)
	}
}

func TestShouldLaunchAppBundle(t *testing.T) {
	e := Entry{Name: "Foo.app", IsDir: true}
	want := runtime.GOOS == "darwin"
	if got := shouldLaunchAppBundle(e); got != want {
		t.Fatalf("shouldLaunchAppBundle = %v, want %v on %s", got, want, runtime.GOOS)
	}
	if shouldLaunchAppBundle(Entry{Name: "Foo.app", IsDir: false}) {
		t.Fatal("file named .app must not launch as bundle")
	}
	if shouldLaunchAppBundle(Entry{Name: "Foo.framework", IsDir: true}) {
		t.Fatal("framework is not launched as an app")
	}
	if shouldLaunchAppBundle(Entry{Name: "pack.zip", IsDir: false}) {
		t.Fatal("zip must not launch as app bundle")
	}
}

func TestOpenDirEntryBranches(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Demo.app")
	if err := os.MkdirAll(filepath.Join(app, "Contents"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "folder")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	a := newStoreApp(t, dir)
	// Ordinary folder: navigate in.
	a.openDirEntry(Entry{Name: "folder", Path: sub, IsDir: true})
	if a.cur().path != sub {
		t.Fatalf("folder open navigated to %q, want %q", a.cur().path, sub)
	}

	before := a.cur().path
	a.openDirEntry(Entry{Name: "Demo.app", Path: app, IsDir: true})
	if shouldLaunchAppBundle(Entry{Name: "Demo.app", Path: app, IsDir: true}) {
		// On darwin, launch path does not navigate into the package.
		if a.cur().path == app {
			t.Fatal("darwin .app open must not navigate into the bundle")
		}
		if a.cur().path != before {
			// openWithOS may fail in CI without Launch Services for a fake
			// bundle; either way we must not have entered the package.
			t.Logf("path after launch attempt: %s (status %q)", a.cur().path, a.statusMsg)
		}
	} else {
		// Non-macOS: .app stays a folder you can enter.
		if a.cur().path != app {
			t.Fatalf("non-darwin .app open = %q, want enter %q", a.cur().path, app)
		}
	}
}

func writeEmptyZip(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := zip.NewWriter(f)
	return w.Close()
}
