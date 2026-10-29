//go:build windows

package main

import (
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"sunvale/internal/fsops"

	"golang.org/x/sys/windows/registry"
)

// sys_windows.go is Windows: Explorer (start /select), the Open-With
// dialog (shell32), Windows Terminal / cmd, drives through kernel32, and
// the Recycle Bin's own $I/$R metadata — delete goes through
// SHFileOperationW so the item lands in the system bin, and the menu
// lists, restores and empties it by parsing every volume's bin files,
// all without cgo.

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")

	procGetLogicalDrives    = kernel32.NewProc("GetLogicalDrives")
	procGetVolumeInfoW      = kernel32.NewProc("GetVolumeInformationW")
	procGetDiskFreeSpaceExW = kernel32.NewProc("GetDiskFreeSpaceExW")
	procGetDriveTypeW       = kernel32.NewProc("GetDriveTypeW")
	procSHFileOperationW    = shell32.NewProc("SHFileOperationW")
	procSHGetKnownFolder    = shell32.NewProc("SHGetKnownFolderPath")
	procCoTaskMemFree       = ole32.NewProc("CoTaskMemFree")
)

// Drive types GetDriveTypeW reports; the rest are unused here.
const (
	driveRemote = 4 // DRIVE_REMOTE — a share, no client-side bin
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

// driveTypeOf reports GetDriveTypeW's verdict on a volume root.
func driveTypeOf(root string) uint32 {
	p, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return 0
	}
	r, _, _ := procGetDriveTypeW.Call(uintptr(unsafe.Pointer(p)))
	return uint32(r)
}

// sysTrashRecyclable reports whether deleting path can land in a bin. A
// network share (SMB, \\wsl$) has no client-side bin — delete there is
// permanent, so callers confirm first, the way Explorer does.
func sysTrashRecyclable(path string) bool {
	return driveTypeOf(volumeRoot(path)) != driveRemote
}

// volumeRoot widens a path to its volume's root — "C:…" → "C:\", a UNC
// path to its \\server\share — for the per-volume Win32 calls, which all
// accept either form (GetDiskFreeSpaceExW, GetDriveTypeW).
func volumeRoot(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	vol := filepath.VolumeName(abs)
	if strings.HasPrefix(vol, `\\`) {
		return vol + `\`
	}
	if len(vol) == 2 && vol[1] == ':' {
		return strings.ToUpper(vol[:1]) + `:\`
	}
	return `C:\`
}

// sysHiddenName applies Explorer's hidden rule to a folder entry. Dot names
// remain hidden for compatibility, while the Win32 attribute is what makes
// ordinary names such as AppData hidden in the address-bar completion.
func sysHiddenName(dir string, e os.DirEntry) bool {
	if strings.HasPrefix(e.Name(), ".") {
		return true
	}
	if info, err := e.Info(); err == nil {
		if fa, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
			return fa.FileAttributes&syscall.FILE_ATTRIBUTE_HIDDEN != 0
		}
	}
	if info, err := os.Stat(filepath.Join(dir, e.Name())); err == nil {
		if fa, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
			return fa.FileAttributes&syscall.FILE_ATTRIBUTE_HIDDEN != 0
		}
	}
	return false
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

// knownFolderGUIDs maps the sidebar's folder keys to their KNOWNFOLDERIDs.
var knownFolderGUIDs = map[string]syscall.GUID{
	"desktop":   {Data1: 0xB4BFCC3A, Data2: 0xDB2C, Data3: 0x424C, Data4: [8]byte{0xB0, 0x29, 0x7F, 0xE9, 0x9A, 0x87, 0xC6, 0x41}},
	"downloads": {Data1: 0x374DE290, Data2: 0x123F, Data3: 0x4565, Data4: [8]byte{0x91, 0x64, 0x39, 0xC4, 0x92, 0x5E, 0x46, 0x7B}},
	"documents": {Data1: 0xFDD39AD0, Data2: 0x238F, Data3: 0x46AF, Data4: [8]byte{0xAD, 0xB4, 0x6C, 0x85, 0x48, 0x03, 0x69, 0xC7}},
	"pictures":  {Data1: 0x33E28130, Data2: 0x4E1E, Data3: 0x4676, Data4: [8]byte{0x83, 0x5A, 0x98, 0x39, 0x5C, 0x3B, 0xC3, 0xBB}},
	"music":     {Data1: 0x4BD8D571, Data2: 0x6D19, Data3: 0x48D3, Data4: [8]byte{0xBE, 0x97, 0x42, 0x22, 0x20, 0x08, 0x0E, 0x43}},
	"videos":    {Data1: 0x18989B1D, Data2: 0x99B5, Data3: 0x455B, Data4: [8]byte{0x84, 0x1C, 0xAB, 0x7C, 0x74, 0xE4, 0xDD, 0xFC}},
}

// sysKnownFolders resolves the sidebar's known folders through the shell,
// so redirects the user set up — OneDrive's known-folder move above all —
// are honored instead of the home-relative default. Entries that fail to
// resolve are absent, and fs.go falls back per folder.
func sysKnownFolders() map[string]string {
	out := make(map[string]string, len(knownFolderGUIDs))
	for key, guid := range knownFolderGUIDs {
		var p *uint16
		r, _, _ := procSHGetKnownFolder.Call(
			uintptr(unsafe.Pointer(&guid)), 0, 0, uintptr(unsafe.Pointer(&p)))
		if r != 0 || p == nil {
			continue
		}
		out[key] = syscall.UTF16ToString((*[1 << 16]uint16)(unsafe.Pointer(p))[:])
		procCoTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
	}
	return out
}

func sysOpen(path string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
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
// sysHasTerminal: Terminal.app / Windows Terminal or cmd are always there.
func warmTerminal() {}

func sysHasTerminal() bool { return true }

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

// recycleRootName is the folder every volume keeps its own bin in, each
// user's deleted items inside as a $I metadata file beside its $R payload.
const recycleRootName = `$Recycle.Bin`

// recycleRoots lists every mounted volume's bin — a file deleted from E:
// lands in E:'s bin, never C:'s, so bin operations walk all of them.
// Remote drives are skipped: they have no client-side bin, and statting a
// disconnected share can block for the whole SMB timeout.
func recycleRoots() []string {
	r, _, _ := procGetLogicalDrives.Call()
	mask := uint32(r)
	var roots []string
	for i := 0; i < 26; i++ {
		if mask&(1<<i) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		if driveTypeOf(root) == driveRemote {
			continue
		}
		if info, err := os.Stat(root + recycleRootName); err == nil && info.IsDir() {
			roots = append(roots, root+recycleRootName)
		}
	}
	return roots
}

// recycleIndex maps each bin item's original path to its $R payload.
func recycleIndex() map[string]string {
	out := map[string]string{}
	for _, root := range recycleRoots() {
		sids, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, sid := range sids {
			if !sid.IsDir() {
				continue
			}
			dir := filepath.Join(root, sid.Name())
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
				orig, ok := fsops.ParseRecycleInfo(meta)
				if !ok || orig == "" {
					continue
				}
				payload := filepath.Join(dir, "$R"+strings.TrimPrefix(f.Name(), "$I"))
				out[strings.ToLower(orig)] = payload
			}
		}
	}
	return out
}

// sysTrashDir is the bin's root, for callers that just need a place.
func sysTrashDir() (string, error) {
	if roots := recycleRoots(); len(roots) > 0 {
		return roots[0], nil
	}
	return "", fmt.Errorf("找不到回收站")
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
	// UTF16FromString rejects embedded NULs and appends the first one, so
	// the second is appended here — composing path+"\x00" up front made
	// every delete fail with EINVAL before the shell was ever called.
	from, err := syscall.UTF16FromString(path)
	if err != nil {
		return err
	}
	from = append(from, 0)
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
		return fmt.Errorf("%s", shellDeleteErrorText(int(r)))
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

// sysTrashList enumerates the bins of every volume, showing each item
// under its original name.
func sysTrashList() []Entry {
	var entries []Entry
	for _, root := range recycleRoots() {
		entries = append(entries, trashListRoot(root)...)
	}
	return entries
}

// trashListRoot lists one volume's bin across its SID folders.
func trashListRoot(root string) []Entry {
	sids, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var entries []Entry
	for _, sid := range sids {
		if !sid.IsDir() {
			continue
		}
		dir := filepath.Join(root, sid.Name())
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
			orig, ok := fsops.ParseRecycleInfo(meta)
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
				ModTime: fsops.RecycleDeletedAt(meta),
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

// sysTrashEmpty clears every volume's bin.
func sysTrashEmpty() error {
	for _, root := range recycleRoots() {
		if err := trashEmptyRoot(root); err != nil {
			return err
		}
	}
	return nil
}

// trashEmptyRoot clears one volume's bin of its SID folders.
func trashEmptyRoot(root string) error {
	sids, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, sid := range sids {
		if !sid.IsDir() {
			continue
		}
		dir := filepath.Join(root, sid.Name())
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

// The 打开方式 registry reads the registry's own OpenWithList entries for
// the extension — the same names Explorer's menu offers — and launches
// them by their EXE command line, since applications are not named here.

func sysOpenWithScan() { scanOpenWithAppsWindows() }

// sysOpenWithMenuTail is openWithMenu's Windows tail: the system's
// Open-With dialog, which reaches every installed application.
func sysOpenWithMenuTail(a *App, m *xMenu, e Entry) {
	if m.Item("选择其他应用…").Chosen() {
		if err := sysOpenAs(e.Path); err != nil {
			a.statusMsg = "无法打开选择器：" + err.Error()
		}
	}
}

// scanOpenWithAppsWindows fills the 打开方式 registry from
// HKCR\.ext\OpenWithList / OpenWithProgids: each key names the programs
// Explorer itself would offer, and each ProgID's shell\open\command says
// how to run it. Recorded per extension as the friendly command name —
// the EXE's own, "notepad" for %SystemRoot%\system32\notepad.exe — since
// sysOpenWith matches apps by that token.
func scanOpenWithAppsWindows() {
	r := &openWithRegistry{byExt: map[string][]string{}}
	for _, ext := range registeredExtensions() {
		apps := map[string]bool{}
		for _, progid := range registrySubkeys(`SOFTWARE\Classes\.` + ext + `\OpenWithList`) {
			for _, name := range registryValueNames(`SOFTWARE\Classes\.` + ext + `\OpenWithList\` + progid) {
				// The subkey holds one unnamed value per alternate exe.
				if name == "" {
					if cmd := registryStringValue(`SOFTWARE\Classes\`+progid+`\shell\open\command`, ""); cmd != "" {
						apps[commandToken(cmd)] = true
					}
				}
			}
			if cmd := registryStringValue(`SOFTWARE\Classes\`+progid+`\shell\open\command`, ""); cmd != "" {
				apps[commandToken(cmd)] = true
			}
		}
		for _, progid := range registryValueNames(`SOFTWARE\Classes\.` + ext + `\OpenWithProgids`) {
			if cmd := registryStringValue(`SOFTWARE\Classes\`+progid+`\shell\open\command`, ""); cmd != "" {
				apps[commandToken(cmd)] = true
			}
		}
		delete(apps, "")
		list := make([]string, 0, len(apps))
		for app := range apps {
			list = append(list, app)
		}
		slices.Sort(list)
		r.byExt[ext] = list
	}
	openWith.Store(r)
}

// registeredExtensions lists the extensions HKCR's Classes tree holds an
// OpenWithList or OpenWithProgids for.
func registeredExtensions() []string {
	var out []string
	for _, ext := range registrySubkeys(`SOFTWARE\Classes\.`) {
		if strings.HasPrefix(ext, ".") {
			out = append(out, strings.TrimPrefix(ext, "."))
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// commandToken is the runnable name inside an open command: the last
// path segment of the quoted or first bare word, lower-cased without its
// .exe. sysOpenWith rebuilds a command from it, so this round-trips.
func commandToken(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if rest, ok := strings.CutPrefix(cmd, `"`); ok {
		cmd, _, _ = strings.Cut(rest, `"`)
	} else {
		cmd, _, _ = strings.Cut(cmd, " ")
	}
	base := strings.ToLower(filepath.Base(cmd))
	return strings.TrimSuffix(base, ".exe")
}

// sysOpenWith launches app on the file the way Explorer's own menu does:
// the recorded token is an executable name, run through the shell's
// association so its arguments — "notepad %1" — still apply.
func sysOpenWith(app, path string) error {
	cmd := registryStringValue(`SOFTWARE\Classes\`+app+`\shell\open\command`, "")
	if cmd == "" {
		// Not a ProgID after all — a bare executable name; let the shell
		// resolve it on PATH as notepad does.
		return exec.Command("cmd", "/c", "start", "", app, path).Start()
	}
	if !strings.Contains(cmd, "%1") {
		cmd += ` "%1"`
	}
	cmd = strings.ReplaceAll(cmd, "%1", path)
	// Split respecting quotes: the first token is the program.
	prog := cmd
	rest := ""
	if rest2, ok := strings.CutPrefix(cmd, `"`); ok {
		prog, rest, _ = strings.Cut(rest2, `"`)
	} else if i := strings.IndexByte(cmd, ' '); i >= 0 {
		prog, rest = cmd[:i], cmd[i+1:]
	}
	args := []string{}
	for _, f := range strings.Fields(rest) {
		args = append(args, strings.Trim(f, `"`))
	}
	return exec.Command(prog, args...).Start()
}

// registrySubkeys lists a Classes-tree key's subkey names, empty when the
// key is absent — the common case for the extensions a machine has no
// open-with entries for.
func registrySubkeys(path string) []string {
	k, err := registry.OpenKey(registry.CLASSES_ROOT, path, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer k.Close()
	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}
	return names
}

// registryValueNames lists a key's value names, empty when absent.
func registryValueNames(path string) []string {
	k, err := registry.OpenKey(registry.CLASSES_ROOT, path, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	names, err := k.ReadValueNames(-1)
	if err != nil {
		return nil
	}
	return names
}

// registryStringValue reads a key's string value, empty when either is
// missing.
func registryStringValue(path, name string) string {
	k, err := registry.OpenKey(registry.CLASSES_ROOT, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	if err != nil {
		return ""
	}
	return v
}

// sysRootName is the root volume's name in the sidebar, the breadcrumb and
// This PC: only reached by a Unix-style root path.
func sysRootName() string { return "本地磁盘" }
