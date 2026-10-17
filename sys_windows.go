//go:build windows

package main

import (
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/egoist/mygo/ui"
)

// sys_windows.go is Windows: Explorer (start /select), the Open-With
// dialog (shell32), Windows Terminal / cmd, drives through kernel32, and
// the Recycle Bin's own $I/$R metadata — delete goes through
// SHFileOperationW so the item lands in the system bin, and the menu
// lists, restores and empties it by parsing the bin's files, all without
// cgo.

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	procGetLogicalDrives    = kernel32.NewProc("GetLogicalDrives")
	procGetVolumeInfoW      = kernel32.NewProc("GetVolumeInformationW")
	procGetDiskFreeSpaceExW = kernel32.NewProc("GetDiskFreeSpaceExW")
	procSHFileOperationW    = shell32.NewProc("SHFileOperationW")
)

// volumeCap reports a volume's free and total bytes.
func volumeCap(path string) (free, total int64, ok bool) {
	var f, t, _ uint64
	root, err := syscall.UTF16PtrFromString(volumeRoot(path))
	if err != nil {
		return 0, 0, false
	}
	r, _, _ := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(root)),
		uintptr(unsafe.Pointer(&f)),
		uintptr(unsafe.Pointer(&t)),
		0)
	if r == 0 {
		return 0, 0, false
	}
	return int64(f), int64(t), true
}

// volumeRoot widens a path to its drive root, "C:" → "C:\".
func volumeRoot(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if len(abs) >= 2 && abs[1] == ':' {
		return strings.ToUpper(abs[:1]) + `:\`
	}
	return `C:\`
}

// sysVolumes lists the machine's drives with their labels, as Explorer's
// This PC does, skipping empty card readers.
func sysVolumes() []Place {
	r, _, _ := procGetLogicalDrives.Call()
	mask := uint32(r)
	var places []Place
	for i := 0; i < 26; i++ {
		if mask&(1<<i) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		label := driveLabel(root)
		if label == "" {
			continue // unmounted, or no media
		}
		name := "本地磁盘 (" + string(rune('A'+i)) + ":)"
		if label != "" && label != "新卷" {
			name = label + " (" + string(rune('A'+i)) + ":)"
		}
		places = append(places, Place{Name: name, Path: root, Kind: "drive"})
	}
	return places
}

func driveLabel(root string) string {
	rootPtr, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return ""
	}
	var name [64]uint16
	r, _, _ := procGetVolumeInfoW.Call(
		uintptr(unsafe.Pointer(rootPtr)),
		uintptr(unsafe.Pointer(&name)), uintptr(len(name)),
		0, 0, 0, 0, 0)
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(name[:])
}

func sysOpen(path string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
}

// sysOpenWith is not the Windows way: applications are not named.
func sysOpenWith(app, path string) error {
	return sysOpen(path)
}

// sysOpenAs opens the system's Open With dialog for the file.
func sysOpenAs(path string) error {
	return exec.Command("rundll32", "shell32.dll,OpenAs_RunDLL", path).Start()
}

// sysReveal shows the file selected in Explorer.
func sysReveal(path string) error {
	return exec.Command("explorer.exe", "/select,"+path).Start()
}

// sysTerminal opens a terminal at dir: Windows Terminal when present,
// otherwise the classic console.
func sysTerminal(dir string) error {
	if _, err := exec.LookPath("wt.exe"); err == nil {
		return exec.Command("wt.exe", "-d", dir).Start()
	}
	return exec.Command("cmd", "/c", "start", "cmd", "/K",
		`cd /d "`+dir+`"`).Start()
}

// sysTerminalRun runs script in a new console window.
func sysTerminalRun(script string) error {
	return exec.Command("cmd", "/c", "start", "扩展命令", "cmd", "/K", script).Start()
}

// sysThumbImage: Windows has no thumbnail CLI; the stdlib formats cover
// the common pictures, and anything else previews as its type icon.
func sysThumbImage(path string, size int) (image.Image, error) {
	return nil, fmt.Errorf("系统缩略图不可用")
}

// sysFileMeta has no mdls equivalent; ffprobe covers media metadata.
func sysFileMeta(path string) ([]propRow, bool) { return nil, false }

// sysAudioPlay: MyGo's decoders cover WAV/MP3/Vorbis; ffplay, when the
// user has ffmpeg installed, plays the rest.
func sysAudioPlay(path string) (name string, args []string, dur time.Duration, ok bool) {
	if _, err := exec.LookPath("ffplay"); err != nil {
		return "", nil, 0, false
	}
	if d := videoDuration(path); d > 0 {
		return "ffplay", []string{"-nodisp", "-autoexit", "-loglevel", "quiet", path},
			time.Duration(d * float64(time.Second)), true
	}
	return "", nil, 0, false
}

// — The Recycle Bin —

// recycleRoot is where the system keeps every user's deleted items, each
// as a $I metadata file beside its $R payload.
const recycleRoot = `C:\$Recycle.Bin`

// recycleIndex maps each bin item's original path to its $R payload.
func recycleIndex() map[string]string {
	out := map[string]string{}
	sids, err := os.ReadDir(recycleRoot)
	if err != nil {
		return out
	}
	for _, sid := range sids {
		if !sid.IsDir() {
			continue
		}
		dir := filepath.Join(recycleRoot, sid.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasPrefix(f.Name(), "$I") {
				continue
			}
			meta, err := os.ReadFile(filepath.Join(dir, f.Name()))
			if err != nil {
				continue
			}
			orig, ok := parseRecycleInfo(meta)
			if !ok || orig == "" {
				continue
			}
			payload := filepath.Join(dir, "$R"+strings.TrimPrefix(f.Name(), "$I"))
			out[strings.ToLower(orig)] = payload
		}
	}
	return out
}

// sysTrashDir is the bin's root, for callers that just need a place.
func sysTrashDir() (string, error) {
	if info, err := os.Stat(recycleRoot); err != nil || !info.IsDir() {
		return "", fmt.Errorf("找不到回收站")
	}
	return recycleRoot, nil
}

// sysTrashMove sends path to the Recycle Bin with SHFileOperationW, then
// finds the payload it landed as by comparing the bin's index before and
// after — that payload path is the restore key's source.
func sysTrashMove(path string) (string, string, error) {
	before := recycleIndex()
	if err := shellDelete(path); err != nil {
		return "", "", err
	}
	after := recycleIndex()
	for orig, payload := range after {
		if _, was := before[orig]; !was && strings.EqualFold(orig, path) {
			return payload, recycleKeyOf(payload), nil
		}
	}
	// The item is in the bin but its payload did not surface in time; it
	// still lists (its $I knows the original), only our own restore log
	// cannot map it.
	return "", "", nil
}

// shellDelete moves path to the Recycle Bin, quietly.
func shellDelete(path string) error {
	// The list must end with two NULs, as the shell's string lists do.
	from, err := syscall.UTF16FromString(path + "\x00")
	if err != nil {
		return err
	}
	op := shFileOpStruct{
		wFunc: 3, // FO_DELETE
		pFrom: &from[0],
		fFlags: 0x40 | // FOF_ALLOWUNDO — to the Recycle Bin
			0x10 | // FOF_NOCONFIRMATION
			0x4 | // FOF_SILENT
			0x400, // FOF_NOERRORUI
	}
	r, _, _ := procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
	if r != 0 {
		return fmt.Errorf("删除错误 %d", r)
	}
	if op.fAnyOperationsAborted != 0 {
		return fmt.Errorf("删除被取消")
	}
	return nil
}

// shFileOpStruct is SHFILEOPSTRUCTW, laid out as the shell expects.
type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom, pTo            *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

// sysTrashList enumerates the bin across its SID folders, showing each
// item under its original name.
func sysTrashList() []Entry {
	sids, err := os.ReadDir(recycleRoot)
	if err != nil {
		return nil
	}
	var entries []Entry
	for _, sid := range sids {
		if !sid.IsDir() {
			continue
		}
		dir := filepath.Join(recycleRoot, sid.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if !strings.HasPrefix(f.Name(), "$I") {
				continue
			}
			meta, err := os.ReadFile(filepath.Join(dir, f.Name()))
			if err != nil {
				continue
			}
			orig, ok := parseRecycleInfo(meta)
			if !ok || orig == "" {
				continue
			}
			payload := filepath.Join(dir, "$R"+strings.TrimPrefix(f.Name(), "$I"))
			info, err := os.Lstat(payload)
			if err != nil {
				continue
			}
			entries = append(entries, Entry{
				Name:    filepath.Base(orig),
				Path:    payload,
				IsDir:   info.IsDir(),
				Size:    info.Size(),
				ModTime: recycleDeletedAt(meta),
			})
		}
	}
	return entries
}

// recycleKeyOf strips the $R prefix: $I and $R share their suffix, and
// the restore log keys on it.
func recycleKeyOf(payload string) string {
	return strings.TrimPrefix(filepath.Base(payload), "$R")
}

// sysTrashKey reads the log key of a listed item's payload.
func sysTrashKey(payload string) string { return recycleKeyOf(payload) }

// sysTrashRestore moves the payload back to dst and drops its metadata.
func sysTrashRestore(payload, dst string) error {
	if err := os.Rename(payload, dst); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(filepath.Dir(payload),
		"$I"+strings.TrimPrefix(filepath.Base(payload), "$R")))
	return nil
}

// sysTrashRemove deletes the payload and its metadata outright.
func sysTrashRemove(payload string) error {
	if err := os.RemoveAll(payload); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(filepath.Dir(payload),
		"$I"+strings.TrimPrefix(filepath.Base(payload), "$R")))
	return nil
}

// sysTrashEmpty clears every SID folder of the bin.
func sysTrashEmpty() error {
	sids, err := os.ReadDir(recycleRoot)
	if err != nil {
		return err
	}
	for _, sid := range sids {
		if !sid.IsDir() {
			continue
		}
		dir := filepath.Join(recycleRoot, sid.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			name := f.Name()
			if strings.HasPrefix(name, "$I") || strings.HasPrefix(name, "$R") {
				_ = os.RemoveAll(filepath.Join(dir, name))
			}
		}
	}
	return nil
}

// The 打开方式 registry: Windows hands the choice to its own dialog.

func sysOpenWithScan() {}

func sysOpenWithMenu(a *App, m *ui.Menu, e Entry) {
	if m.Item("选择应用打开…").Chosen() {
		if err := sysOpenAs(e.Path); err != nil {
			a.statusMsg = "无法打开选择器：" + err.Error()
		}
	}
}
