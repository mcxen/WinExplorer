//go:build !windows

package main

import "image"

// appIconExt reports whether files with ext carry an icon of their own
// that platformAppIcon can read. Outside Windows only .desktop entries do
// (read in appicon.go); .AppImage keeps the generic application glyph.
func appIconExt(ext string) bool { return ext == "desktop" }

func platformAppIcon(path string, side int) image.Image { return nil }
