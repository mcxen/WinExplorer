package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchivePasswordBook(t *testing.T) {
	got := archivePasswordCandidates([]archivePassword{
		{Label: "a", Secret: "one"},
		{Label: "dup", Secret: "one"},
		{Label: "b", Secret: "two"},
		{Label: "empty"},
	})
	if strings.Join(got, ",") != "one,two" {
		t.Fatalf("candidates %q", got)
	}
	for _, msg := range []string{
		"7-Zip: Wrong password",
		"archive is encrypted",
		"invalid password",
		"密码错误",
	} {
		if !archivePasswordError(errString(msg)) {
			t.Fatalf("not recognized: %s", msg)
		}
	}
	if archivePasswordError(os.ErrInvalid) || archivePasswordError(nil) {
		t.Fatal("ordinary error treated as a password error")
	}
	if archivePasswordError(errString("permission denied")) {
		t.Fatal("ordinary I/O error treated as a password error")
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestEncryptedZIPUsesCandidate(t *testing.T) {
	if _, err := exec.LookPath("7zz"); err != nil {
		t.Skip("7zz is needed to create an encrypted zip fixture")
	}
	root := t.TempDir()
	src := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(src, []byte("opened with the archive password"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "locked.zip")
	cmd := exec.Command("7zz", "a", "-tzip", "-parchive-secret", "-mem=AES256", archive, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make encrypted zip: %v %s", err, out)
	}
	plainErr := extractArchive(archive, filepath.Join(root, "plain"))
	if plainErr == nil {
		t.Fatal("encrypted zip opened without a password")
	}
	if !archivePasswordError(plainErr) {
		t.Logf("plain extraction error was not classified as password: %v", plainErr)
	}
	dst := filepath.Join(root, "out")
	used, err := extractWithPasswords(archive, dst, []string{"wrong", "archive-secret"})
	if err != nil {
		t.Fatalf("candidate extract: %v", err)
	}
	if used != "archive-secret" {
		t.Fatalf("used %q", used)
	}
	data, err := os.ReadFile(filepath.Join(dst, "secret.txt"))
	if err != nil || string(data) != "opened with the archive password" {
		t.Fatalf("content %q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "secret.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := extractWithPasswords(archive, filepath.Join(root, "wrong"), []string{"wrong"}); err == nil {
		t.Fatal("wrong password succeeded")
	}
}

func TestEncrypted7zUsesCandidate(t *testing.T) {
	if _, err := exec.LookPath("7zz"); err != nil {
		t.Skip("7zz not installed")
	}
	root := t.TempDir()
	src := filepath.Join(root, "note.txt")
	if err := os.WriteFile(src, []byte("seven zip password works"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "locked.7z")
	cmd := exec.Command("7zz", "a", "-t7z", "-pseven-secret", "-mhe=on", archive, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make encrypted 7z: %v %s", err, out)
	}
	dst := filepath.Join(root, "out")
	used, err := extractWithPasswords(archive, dst, []string{"wrong", "seven-secret"})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if used != "seven-secret" {
		t.Fatalf("used %q", used)
	}
	data, err := os.ReadFile(filepath.Join(dst, "note.txt"))
	if err != nil || string(data) != "seven zip password works" {
		t.Fatalf("content %q %v", data, err)
	}
}
