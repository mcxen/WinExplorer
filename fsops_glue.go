package main

import (
	"os"

	"sunvale/internal/fsops"
)

// fsops_glue.go keeps the short names the assistant tools and the folder
// tree call, over internal/fsops, so those files need not change while the
// filesystem operations live in their own package.

func pathWithin(path, dir string) bool { return fsops.PathWithin(path, dir) }

func uniqueNewFolderName(dir string) (string, error) { return fsops.UniqueNewFolderName(dir) }

func uniqueCopyName(dir, name string, isDir bool) (string, error) {
	return fsops.UniqueCopyName(dir, name, isDir)
}

func copyDir(src, dst string) error { return fsops.CopyDir(src, dst) }

func copyFile(src, dst string, info os.FileInfo) error { return fsops.CopyFile(src, dst, info) }

func absPath(p string) (string, error) { return fsops.AbsFrom("", p) }

func freeName(dir, name string, isDir bool) (string, error) {
	return fsops.FreeName(dir, name, isDir)
}

func placeItem(src, dst string, move bool) error { return fsops.PlaceItem(src, dst, move) }
