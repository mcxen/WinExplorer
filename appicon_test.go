package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestKindOf(t *testing.T) {
	cases := map[string]iconKind{
		"a.EXE": kindApp, "a.msi": kindInstaller, "a.dmg": kindDisk, "a.pkg": kindInstaller,
		"a.deb": kindInstaller, "a.AppImage": kindApp, "a.lnk": kindShortcut, "a.tar.gz": kindArchive,
		"a.tar.zst": kindArchive, "a.7z": kindArchive, "a.iso": kindDisk, "a.PNG": kindImage,
		"a.mkv": kindVideo, "a.flac": kindAudio, "a.pdf": kindPDF, "a.docx": kindWord,
		"a.csv": kindSheet, "a.pptx": kindSlides, "a.md": kindText, "a.ts": kindCode,
		"a.yaml": kindCode, "a.ps1": kindScript, "a.woff2": kindFont, "a.torrent": kindTorrent,
		"Makefile": kindCode, "a.xyz": kindFile,
	}
	for name, want := range cases {
		if got := kindOf(Entry{Name: name, Ext: lowerExt(name)}); got != want {
			t.Errorf("%s: got %d want %d", name, got, want)
		}
	}
	if kindOf(Entry{Name: "X.app", IsDir: true}) != kindApp || kindOf(Entry{Name: "x", IsDir: true}) != kindFolder {
		t.Error("dir kinds")
	}
}

func TestMacAppIcon(t *testing.T) {
	icns, err := os.ReadFile("testdata/demo.icns")
	if err != nil {
		t.Fatal(err)
	}
	if img := decodeICNS(icns, 64); img == nil || img.Bounds().Dx() != 128 {
		t.Fatalf("icns: smallest PNG >= 64 should be the 128 entry, got %v", img)
	}
	bplist, _ := base64.StdEncoding.DecodeString("YnBsaXN0MDDSAQIDBF8QEENGQnVuZGxlSWNvbkZpbGVcQ0ZCdW5kbGVOYW1lV0FwcEljb25URGVtbwgNIC01AAAAAAAAAQEAAAAAAAAABQAAAAAAAAAAAAAAAAAAADo=")
	if got := plistString(bplist, "CFBundleIconFile"); got != "AppIcon" {
		t.Fatalf("bplist: %q", got)
	}
	app := filepath.Join(t.TempDir(), "Demo.app")
	res := filepath.Join(app, "Contents", "Resources")
	os.MkdirAll(res, 0o755)
	os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte("<plist><dict><key>CFBundleIconFile</key><string>AppIcon</string></dict></plist>"), 0o644)
	os.WriteFile(filepath.Join(res, "AppIcon.icns"), icns, 0o644)
	if img := loadAppIcon(app, true, 64); img == nil || img.Bounds().Dx() != 64 {
		t.Fatalf("bundle icon: %v", img)
	}
	if loadAppIcon(filepath.Join(t.TempDir(), "Empty.app"), true, 64) != nil {
		t.Fatal("bundle without icon should fall back")
	}
}
