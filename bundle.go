package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// bundle.go treats macOS package directories the way Finder does. They are
// real directories on disk (.app, .framework, .xcodeproj, …) but must never
// be mistaken for archives: archive detection looks at the name alone, and
// a naive caller that forgets IsDir would otherwise offer 全部解压 on them.
// On macOS, double-clicking an .app launches it via open(1); 显示包内容
// still lets the user walk inside.

// macOSBundleSuffixes are common package-directory endings. Finder shows
// them as opaque items; browsing needs 显示包内容 (or equivalent).
var macOSBundleSuffixes = []string{
	".app",
	".bundle",
	".framework",
	".plugin",
	".kext",
	".pkg",
	".xcodeproj",
	".xcworkspace",
	".playground",
	".photoslibrary",
	".scrapbook",
	".appex",
	".qlgenerator",
	".saver",
	".service",
	".action",
	".mdimporter",
	".prefPane",
}

// isMacOSBundleName reports whether name looks like a macOS package
// directory (case-insensitive). The path need not exist.
func isMacOSBundleName(name string) bool {
	l := strings.ToLower(name)
	for _, suf := range macOSBundleSuffixes {
		if strings.HasSuffix(l, suf) {
			return true
		}
	}
	return false
}

// isAppBundleName is the .app case of isMacOSBundleName.
func isAppBundleName(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".app")
}

// isMacOSBundle is a directory whose name is a known package suffix.
func isMacOSBundle(e Entry) bool {
	return e.IsDir && isMacOSBundleName(e.Name)
}

// isAppBundle is a directory ending in .app.
func isAppBundle(e Entry) bool {
	return e.IsDir && isAppBundleName(e.Name)
}

// shouldLaunchAppBundle is true when opening should call the OS to launch
// the app instead of navigating into the package. Only macOS Launch
// Services knows how to start an .app; elsewhere the directory stays a
// folder the user can enter.
func shouldLaunchAppBundle(e Entry) bool {
	return runtime.GOOS == "darwin" && isAppBundle(e)
}

// entryIsArchive reports whether e is a supported archive file.
// Directories — including macOS packages — are never archives.
func entryIsArchive(e Entry) bool {
	return !e.IsDir && isArchive(e.Name)
}

// isArchivePath is the path form of entryIsArchive: a missing path, a
// directory, or a non-archive name yields false.
func isArchivePath(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return isArchive(filepath.Base(path))
}

// bundleTypeLabel is the 类型 column for a macOS package directory.
// Ordinary folders keep returning "" so the caller falls back to 文件夹.
func bundleTypeLabel(name string) string {
	l := strings.ToLower(name)
	switch {
	case strings.HasSuffix(l, ".app"):
		return "应用程序"
	case strings.HasSuffix(l, ".framework"):
		return "框架"
	case strings.HasSuffix(l, ".xcodeproj"), strings.HasSuffix(l, ".xcworkspace"):
		return "Xcode 项目"
	case strings.HasSuffix(l, ".playground"):
		return "Playground"
	case strings.HasSuffix(l, ".photoslibrary"):
		return "照片图库"
	case strings.HasSuffix(l, ".pkg"):
		return "安装包"
	case strings.HasSuffix(l, ".kext"):
		return "内核扩展"
	case strings.HasSuffix(l, ".plugin"), strings.HasSuffix(l, ".appex"),
		strings.HasSuffix(l, ".qlgenerator"), strings.HasSuffix(l, ".saver"),
		strings.HasSuffix(l, ".service"), strings.HasSuffix(l, ".action"),
		strings.HasSuffix(l, ".mdimporter"), strings.HasSuffix(l, ".prefPane"):
		return "插件"
	case strings.HasSuffix(l, ".bundle"), strings.HasSuffix(l, ".scrapbook"):
		return "包"
	}
	return ""
}
