package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// archive_password.go is the extracting-side password book. Entries are
// archive password candidates, not accounts: every archive is tried with
// the user's order from the pane. Secrets stay on disk in assistant.json
// and are never put in a model message or a transcript.

const assistMaxArchivePasswords = 40

// archivePassword is one candidate. Label is only for the pane; Secret is
// what an extractor receives.
type archivePassword struct {
	ID     string `json:"id"`
	Label  string `json:"label,omitempty"`
	Secret string `json:"secret"`
}

var errArchivePassword = errors.New("归档需要密码")

func archivePasswordCandidates(entries []archivePassword) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range entries {
		if p.Secret == "" || seen[p.Secret] {
			continue
		}
		seen[p.Secret] = true
		out = append(out, p.Secret)
	}
	return out
}

// extractWithPasswords tries no password first, then each saved candidate
// in order. A failed try must not leave half a tree behind for the next
// one, so every attempt gets a clean destination.
func extractWithPasswords(archive, dstDir string, passwords []string) (string, error) {
	try := func(password string) error {
		_ = os.RemoveAll(dstDir)
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			return err
		}
		if password == "" {
			return extractArchive(archive, dstDir)
		}
		return extractArchivePassword(archive, dstDir, password)
	}
	last := try("")
	if last == nil {
		return "", nil
	}
	for _, password := range passwords {
		last = try(password)
		if last == nil {
			return password, nil
		}
	}
	if archivePasswordError(last) {
		if len(passwords) == 0 {
			return "", fmt.Errorf("%w；请先在助手密码本添加解压密码", errArchivePassword)
		}
		return "", fmt.Errorf("%w；密码本里的 %d 个密码都不对", errArchivePassword, len(passwords))
	}
	return "", last
}

func archivePasswordError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, part := range []string{
		"password", "encrypted", "encryption", "wrong password",
		"密码", "加密", "口令", "checksum", "unsupported method", "not a valid zip",
	} {
		if strings.Contains(s, part) {
			return true
		}
	}
	return false
}

// extractArchivePassword selects the password-capable path. The standard
// library cannot decrypt ZIP, so encrypted ZIP and 7z/RAR both go through
// an extractor that accepts a password.
func extractArchivePassword(archive, dstDir, password string) error {
	switch archiveKind(filepath.Base(archive)) {
	case "zip", "7z", "rar":
		return extractExternalPassword(archive, dstDir, password)
	}
	return extractArchive(archive, dstDir)
}

func extractExternalPassword(archive, dstDir, password string) error {
	if bin := sevenZipBin(); bin != "" {
		return archiveRunExtract(bin, []string{"x", "-y", "-p" + password, "-o" + dstDir, archive})
	}
	for _, c := range []struct {
		name string
		args []string
	}{
		{name: "unar", args: []string{"-force-overwrite", "-password", password, "-o", dstDir, archive}},
		{name: "unrar", args: []string{"x", "-p" + password, "-y", archive, dstDir + string(filepath.Separator)}},
	} {
		if path, err := archiveLookPath(c.name); err == nil {
			return archiveRunExtract(path, c.args)
		}
	}
	return errors.New("需要安装 7-Zip 或 unar 才能解压加密归档")
}

var (
	archiveLookPath   = exec.LookPath
	archiveRunExtract = func(path string, args []string) error {
		return runExtract(exec.Command(path, args...))
	}
)
