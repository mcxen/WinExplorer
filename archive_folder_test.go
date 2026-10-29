package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

// archive_folder_test.go builds one real zip and one real tar.gz, then
// drives the virtual-folder path: split, root and nested listings, and
// extraction of a file and of a directory (including one no header ever
// spelled out).

func writeArchiveFolderFixture(t *testing.T, path string) {
	t.Helper()
	members := [][2]string{
		{"b.txt", "root"},
		{"子目录/a.txt", "sub"},
		{"只/隐/式.txt", "deep"},
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(path) == ".zip" {
		zw := zip.NewWriter(f)
		for _, m := range members {
			w, err := zw.Create(m[0])
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write([]byte(m[1])); err != nil {
				t.Fatal(err)
			}
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		gz := gzip.NewWriter(f)
		tw := tar.NewWriter(gz)
		for _, m := range members {
			body := []byte(m[1])
			hdr := &tar.Header{Name: m[0], Mode: 0o644, Size: int64(len(body))}
			if err := tw.WriteHeader(hdr); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write(body); err != nil {
				t.Fatal(err)
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func archiveEntriesByName(entries []Entry) map[string]Entry {
	out := make(map[string]Entry, len(entries))
	for _, e := range entries {
		out[e.Name] = e
	}
	return out
}

func TestArchiveFolderSplitAndList(t *testing.T) {
	for _, name := range []string{"示例.zip", "示例.tar.gz"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			arc := filepath.Join(dir, name)
			writeArchiveFolderFixture(t, arc)

			file := filepath.Join(arc, "子目录", "a.txt")
			gotArc, inner, ok := archiveSplit(file)
			if !ok || gotArc != arc || inner != "子目录/a.txt" {
				t.Fatalf("archiveSplit(%q) = %q, %q, %v", file, gotArc, inner, ok)
			}
			if _, _, ok := archiveSplit(filepath.Join(dir, "b.txt")); ok {
				t.Fatal("a plain file was split as an archive")
			}

			l := entryLister(arc)
			if _, ok := l.(archiveLister); !ok {
				t.Fatalf("entryLister returned %T for an archive", l)
			}
			root, err := l.List(arc)
			if err != nil {
				t.Fatal(err)
			}
			byName := archiveEntriesByName(root)
			if len(root) != 3 {
				t.Fatalf("root listed %v", root)
			}
			if e := byName["b.txt"]; e.IsDir || e.Size != 4 || e.Ext != "txt" {
				t.Fatalf("b.txt = %+v", e)
			}
			if e := byName["子目录"]; !e.IsDir || e.Path != filepath.Join(arc, "子目录") {
				t.Fatalf("子目录 = %+v", e)
			}
			if !byName["只"].IsDir {
				t.Fatalf("只 = %+v, want an implicit directory", byName["只"])
			}

			sub, err := l.List(filepath.Join(arc, "子目录"))
			if err != nil {
				t.Fatal(err)
			}
			if len(sub) != 1 || sub[0].Name != "a.txt" || sub[0].Path != file || sub[0].Size != 3 {
				t.Fatalf("子目录 listed %v", sub)
			}
			deep, err := l.List(filepath.Join(arc, "只", "隐"))
			if err != nil {
				t.Fatal(err)
			}
			if len(deep) != 1 || deep[0].Name != "式.txt" || deep[0].Path != filepath.Join(arc, "只", "隐", "式.txt") {
				t.Fatalf("只/隐 listed %v", deep)
			}
			if _, err := l.List(filepath.Join(arc, "没有这个目录")); err == nil || err.Error() != "压缩包中没有该文件夹" {
				t.Fatalf("missing folder error = %v", err)
			}
		})
	}
}

func TestArchiveFolderExtract(t *testing.T) {
	for _, name := range []string{"示例.zip", "示例.tar.gz"} {
		t.Run(name, func(t *testing.T) {
			arc := filepath.Join(t.TempDir(), name)
			writeArchiveFolderFixture(t, arc)

			fileOut := t.TempDir()
			written, err := extractArchiveEntry(arc, "子目录/a.txt", fileOut)
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(fileOut, "a.txt")
			if written != want {
				t.Fatalf("file written to %q, want %q", written, want)
			}
			if body, err := os.ReadFile(want); err != nil || string(body) != "sub" {
				t.Fatalf("a.txt = %q, %v", body, err)
			}

			dirOut := t.TempDir()
			written, err = extractArchiveEntry(arc, "子目录", dirOut)
			if err != nil {
				t.Fatal(err)
			}
			want = filepath.Join(dirOut, "子目录")
			if written != want {
				t.Fatalf("folder written to %q, want %q", written, want)
			}
			if body, err := os.ReadFile(filepath.Join(want, "a.txt")); err != nil || string(body) != "sub" {
				t.Fatalf("extracted a.txt = %q, %v", body, err)
			}

			implicitOut := t.TempDir()
			written, err = extractArchiveEntry(arc, "只", implicitOut)
			if err != nil {
				t.Fatal(err)
			}
			if written != filepath.Join(implicitOut, "只") {
				t.Fatalf("implicit folder written to %q", written)
			}
			if body, err := os.ReadFile(filepath.Join(written, "隐", "式.txt")); err != nil || string(body) != "deep" {
				t.Fatalf("implicit subtree = %q, %v", body, err)
			}
		})
	}
}

func TestArchiveFolderAppNavigation(t *testing.T) {
	root := t.TempDir()
	arc := filepath.Join(root, "示例.zip")
	writeArchiveFolderFixture(t, arc)

	a := newStoreApp(t, root)
	a.navigate(arc)
	if !a.inArchive() || a.cur().path != arc {
		t.Fatalf("navigating into the archive left path %q (inArchive=%v)", a.cur().path, a.inArchive())
	}
	sub := filepath.Join(arc, "子目录")
	a.navigate(sub)
	if a.cur().path != sub {
		t.Fatalf("inside path = %q", a.cur().path)
	}
	if target, msg := resolvePathInput(sub, root); msg != "" || target.dir != sub {
		t.Fatalf("address input for %q = %+v, %q", sub, target, msg)
	}
	if crumbs := a.breadcrumbs(); crumbs[len(crumbs)-1].Path != sub {
		t.Fatalf("breadcrumbs end at %q, want %q", crumbs[len(crumbs)-1].Path, sub)
	}

	file := filepath.Join(sub, "a.txt")
	if target, msg := resolvePathInput(file, root); msg != "" || target.dir != sub || target.file != file {
		t.Fatalf("address input for %q = %+v, %q", file, target, msg)
	}
	a.selectOnly(file)
	a.doDelete()
	if a.statusMsg != "压缩包为只读" {
		t.Fatalf("delete inside archive: status %q", a.statusMsg)
	}
	a.reload()
	if len(a.cur().entries) != 1 || a.cur().entries[0].Name != "a.txt" {
		t.Fatalf("delete changed the archive listing: %v", a.cur().entries)
	}

	a.up()
	if a.cur().path != arc {
		t.Fatalf("first 上一级 = %q, want the archive root %q", a.cur().path, arc)
	}
	a.up()
	if a.cur().path != root {
		t.Fatalf("second 上一级 = %q, want the containing folder %q", a.cur().path, root)
	}
}
