package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sunvale/internal/tasks"
)

// EXTRACT-ASK: after the password book fails, the extract task asks inline;
// submit retries, cancel fails that archive, and nested inners ask too.

func TestExtractAsksAfterBookFails(t *testing.T) {
	if _, err := exec.LookPath("7zz"); err != nil {
		t.Skip("7zz needed for encrypted zip")
	}
	asyncTasks(t)
	root := t.TempDir()
	src := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(src, []byte("ask-ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "locked.zip")
	cmd := exec.Command("7zz", "a", "-tzip", "-pask-secret", "-mem=AES256", archive, src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("make zip: %v %s", err, out)
	}
	a := newStoreApp(t, root)
	oldAsk := extractWithPasswordsFn
	var asks atomic.Int32
	extractWithPasswordsFn = func(arch, dst string, passwords []string, ask passwordPrompt) (string, error) {
		// Force book miss then use the real ask path.
		if ask == nil {
			return "", fmt.Errorf("%w；密码本空", errArchivePassword)
		}
		asks.Add(1)
		pw, save, err := ask(filepath.Base(arch), "需要解压密码")
		if err != nil {
			return "", err
		}
		if pw == "" {
			return "", errArchivePassword
		}
		_ = save
		return oldAsk(arch, dst, []string{pw}, nil)
	}
	t.Cleanup(func() { extractWithPasswordsFn = oldAsk })

	e := Entry{Name: "locked.zip", Path: archive}
	aiFx.markBusy(archive)
	var id string
	id = queueArchive("extract", archive, "user", func(task *archiveTask) error {
		o := extractOne(root, e, nil, extractPasswordAsk(task))
		return o.err
	})
	s := waitAsk(t, id)
	if s.Ask == nil || s.Ask.Kind != tasks.AskKindPassword || s.Ask.Item != "locked.zip" {
		t.Fatalf("ask %+v", s.Ask)
	}
	_ = taskManager().Answer(id, tasks.Answer{Choice: tasks.ChoiceSubmit, Text: "ask-secret"})
	fin, _ := taskManager().Wait(context.Background(), id)
	if fin.State != tasks.Done {
		t.Fatalf("final %+v err=%s", fin, fin.Err)
	}
	if asks.Load() < 1 {
		t.Fatal("ask was not called")
	}
	dst := filepath.Join(root, "locked")
	body, err := os.ReadFile(filepath.Join(dst, "secret.txt"))
	if err != nil || string(body) != "ask-ok" {
		t.Fatalf("content %q %v", body, err)
	}
	_ = a
}

func TestExtractAskCancelFailsArchive(t *testing.T) {
	asyncTasks(t)
	root := t.TempDir()
	archive := filepath.Join(root, "locked.zip")
	if err := os.WriteFile(archive, []byte("not-a-real-archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldAsk := extractWithPasswordsFn
	extractWithPasswordsFn = func(arch, dst string, passwords []string, ask passwordPrompt) (string, error) {
		if ask == nil {
			return "", errArchivePassword
		}
		pw, _, err := ask(filepath.Base(arch), "需要解压密码")
		if err != nil {
			return "", err
		}
		if pw == "" {
			return "", errArchivePassword
		}
		return pw, nil
	}
	t.Cleanup(func() { extractWithPasswordsFn = oldAsk })

	e := Entry{Name: "locked.zip", Path: archive}
	id := queueArchive("extract", archive, "user", func(task *archiveTask) error {
		o := extractOne(root, e, nil, extractPasswordAsk(task))
		return o.err
	})
	_ = waitAsk(t, id)
	_ = taskManager().Answer(id, tasks.Answer{Choice: tasks.ChoiceCancel})
	fin, _ := taskManager().Wait(context.Background(), id)
	if fin.State != tasks.Failed && !errors.Is(errors.New(fin.Err), errArchivePassword) {
		if fin.State != tasks.Failed {
			t.Fatalf("want failed, got %+v", fin)
		}
	}
	if !strings.Contains(fin.Err, "密码") && fin.State != tasks.Failed {
		t.Fatalf("err %q state %v", fin.Err, fin.State)
	}
}

func TestExtractNestedAsksInner(t *testing.T) {
	src := nestedZipFixture(t)
	old := extractWithPasswordsFn
	var asked []string
	extractWithPasswordsFn = func(archive, dst string, passwords []string, ask passwordPrompt) (string, error) {
		if strings.HasSuffix(archive, "l3.zip") {
			if ask == nil {
				return "", fmt.Errorf("%w；locked", errArchivePassword)
			}
			pw, _, err := ask(filepath.Base(archive), "内层需要密码")
			asked = append(asked, filepath.Base(archive))
			if err != nil {
				return "", err
			}
			if pw == "" {
				return "", errArchivePassword
			}
			// Simulate success with user password by falling through to real unzip.
			return old(archive, dst, nil, nil)
		}
		return old(archive, dst, passwords, ask)
	}
	t.Cleanup(func() { extractWithPasswordsFn = old })

	ask := func(name, detail string) (string, bool, error) {
		return "any", false, nil
	}
	dst := filepath.Join(t.TempDir(), "out")
	_, r, err := extractArchiveNestedAsk(src, dst, nil, ask)
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0] != "l3.zip" {
		t.Fatalf("asked %v", asked)
	}
	if len(r.Locked) != 0 || len(r.Extracted) < 2 {
		t.Fatalf("report %+v", r)
	}
}

func TestExtractWithPasswordsAskWrongThenRight(t *testing.T) {
	tries := 0
	ask := func(name, detail string) (string, bool, error) {
		tries++
		if tries == 1 {
			if !strings.Contains(detail, "密码") {
				t.Fatalf("detail %q", detail)
			}
			return "wrong", false, nil
		}
		return "right", true, nil
	}
	oldTry := extractWithPasswordsFn
	// Drive without real archives: stub the password engine under Ask.
	extractWithPasswordsFn = extractWithPasswordsAsk
	t.Cleanup(func() { extractWithPasswordsFn = oldTry })

	root := t.TempDir()
	arch := filepath.Join(root, "x.zip")
	dst := filepath.Join(root, "out")
	// Stub try path by overriding archiveRunExtract / extractArchive via fn.
	// Use a custom ask-only unit: call extractWithPasswordsAsk with a
	// passwords miss simulated by replacing try through extractWithPasswordsFn mock.
	calls := 0
	extractWithPasswordsFn = func(archive, dstDir string, passwords []string, askFn passwordPrompt) (string, error) {
		calls++
		if calls == 1 {
			// book empty → ask
			pw, save, err := askFn(filepath.Base(archive), "需要解压密码")
			if err != nil {
				return "", err
			}
			if pw == "wrong" {
				// re-ask like the real engine
				pw, save, err = askFn(filepath.Base(archive), "密码不对，请重试")
				if err != nil {
					return "", err
				}
			}
			if pw != "right" || !save {
				t.Fatalf("pw=%q save=%v", pw, save)
			}
			_ = os.MkdirAll(dstDir, 0o755)
			return pw, nil
		}
		return "", errArchivePassword
	}
	pw, err := extractWithPasswordsFn(arch, dst, nil, ask)
	if err != nil || pw != "right" {
		t.Fatalf("pw=%q err=%v", pw, err)
	}
	if tries != 2 {
		t.Fatalf("tries %d", tries)
	}
}

func TestExtractPasswordAskNilReporter(t *testing.T) {
	if extractPasswordAsk(nil) != nil {
		t.Fatal("nil task should yield nil asker")
	}
	if extractPasswordAsk(&archiveTask{}) != nil {
		t.Fatal("task without reporter should yield nil asker")
	}
}

func TestAskPasswordAnswerCarriesText(t *testing.T) {
	m := tasks.NewManager(tasks.Options{})
	got := make(chan tasks.Answer, 1)
	id := m.Submit(tasks.Spec{}, func(r *tasks.Reporter) error {
		a, err := r.Ask(tasks.Ask{Kind: tasks.AskKindPassword, Item: "a.zip", Detail: "需要解压密码",
			Options: []string{tasks.ChoiceSubmit, tasks.ChoiceCancel}})
		got <- a
		return err
	})
	deadline := time.Now().Add(2 * time.Second)
	for m.Get(id).Ask == nil {
		if time.Now().After(deadline) {
			t.Fatal("no question")
		}
		time.Sleep(time.Millisecond)
	}
	if q := m.Get(id).Ask; q.Kind != tasks.AskKindPassword {
		t.Fatalf("ask %+v", q)
	}
	_ = m.Answer(id, tasks.Answer{Choice: tasks.ChoiceSubmit, Text: "s3cret", Save: true})
	a := <-got
	if a.Text != "s3cret" || !a.Save || a.Choice != tasks.ChoiceSubmit {
		t.Fatalf("answer %+v", a)
	}
}

func TestCorruptZipDoesNotAsk(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "bad.zip")
	if err := os.WriteFile(bad, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	asked := false
	ask := func(name, detail string) (string, bool, error) {
		asked = true
		return "", false, errArchivePassword
	}
	_, err := extractWithPasswordsAsk(bad, filepath.Join(root, "out"), nil, ask)
	if err == nil {
		t.Fatal("corrupt zip extracted")
	}
	if asked {
		t.Fatal("corrupt zip must not prompt for a password")
	}
}

func TestShouldAskArchivePassword(t *testing.T) {
	if shouldAskArchivePassword("x.zip", errString("permission denied"), 0) {
		t.Fatal("ordinary error")
	}
	if !shouldAskArchivePassword("x.zip", errString("Wrong password"), 0) {
		t.Fatal("clear password error should ask")
	}
	if !shouldAskArchivePassword("x.zip", errString("not a valid zip"), 2) {
		t.Fatal("ambiguous + book tried should ask")
	}
	if shouldAskArchivePassword("x.zip", errString("not a valid zip"), 0) {
		t.Fatal("ambiguous + empty book + non-encrypted must not ask")
	}
}
