package main

import (
	"archive/zip"
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
//
// When the book fails (or is empty) for an encrypted archive, an optional
// passwordPrompt can ask the user — right-click extract wires this to the
// task card (EXTRACT-ASK); assistant / remote leave it nil.

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

// passwordPrompt asks for one more password after the book failed.
// name is the archive base name; detail is the card hint. Empty password
// or cancel should return errArchivePassword (or ErrCanceled). Save means
// append the password to the book once it opens the archive.
type passwordPrompt func(name, detail string) (password string, save bool, err error)

// onArchivePasswordSaved, when set, learns a password that just opened an
// archive (session book + last-used). Tests leave it nil.
var onArchivePasswordSaved func(secret string)

// extractWithPasswords tries no password first, then each saved candidate
// in order. A failed try must not leave half a tree behind for the next
// one, so every attempt gets a clean destination.
func extractWithPasswords(archive, dstDir string, passwords []string) (string, error) {
	return extractWithPasswordsAsk(archive, dstDir, passwords, nil)
}

// extractWithPasswordsAsk is extractWithPasswords plus an optional user
// prompt after the book is exhausted. Wrong user passwords re-prompt
// until cancel / empty.
func extractWithPasswordsAsk(archive, dstDir string, passwords []string, ask passwordPrompt) (string, error) {
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
	if !archivePasswordError(last) {
		return "", last
	}
	if ask == nil || !shouldAskArchivePassword(archive, last, len(passwords)) {
		if len(passwords) == 0 {
			return "", fmt.Errorf("%w；请先在助手密码本添加解压密码", errArchivePassword)
		}
		return "", fmt.Errorf("%w；密码本里的 %d 个密码都不对", errArchivePassword, len(passwords))
	}
	name := filepath.Base(archive)
	detail := "需要解压密码"
	if len(passwords) > 0 {
		detail = fmt.Sprintf("密码本 %d 个都不对，请输入密码", len(passwords))
	}
	for {
		pw, save, err := ask(name, detail)
		if err != nil {
			return "", err
		}
		pw = strings.TrimSpace(pw)
		if pw == "" {
			return "", fmt.Errorf("%w", errArchivePassword)
		}
		last = try(pw)
		if last == nil {
			if save {
				_ = saveAssistantArchivePassword(name, pw)
			}
			if onArchivePasswordSaved != nil {
				onArchivePasswordSaved(pw)
			}
			return pw, nil
		}
		if !archivePasswordError(last) {
			return "", last
		}
		detail = "密码不对，请重试"
	}
}

// shouldAskArchivePassword decides whether to prompt the user. Clear
// password/encrypted errors always ask; ambiguous ones (e.g. "not a valid
// zip") ask only when the book was already tried, or a zip member is
// marked encrypted — so a corrupt file with an empty book fails fast
// instead of hanging the task card.
func shouldAskArchivePassword(archive string, err error, bookTried int) bool {
	if !archivePasswordError(err) {
		return false
	}
	if archivePasswordClear(err) || bookTried > 0 {
		return true
	}
	return zipMembersEncrypted(archive)
}

func archivePasswordClear(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, part := range []string{
		"password", "encrypted", "encryption", "wrong password",
		"密码", "加密", "口令",
	} {
		if strings.Contains(s, part) {
			return true
		}
	}
	return false
}

func zipMembersEncrypted(archive string) bool {
	if archiveKind(filepath.Base(archive)) != "zip" {
		return false
	}
	r, err := zip.OpenReader(archive)
	if err != nil {
		return false
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Flags&0x1 != 0 {
			return true
		}
	}
	return false
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
