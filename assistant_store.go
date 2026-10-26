package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	assistant "sunvale/internal/assistant"
)

// assistant_store.go is the assistant's local config: providers, the password
// book, and a few pane preferences. It lives beside state.json and is mode
// 0600 because it holds API keys and passwords. Transcripts and cross-session
// memory are not written here; those stay under assistant/.

type aiProvider = assistant.Provider
type assistPrefs = assistant.Prefs
type assistantDisk = assistant.Config[archivePassword]

func (u *assistantUI) ensure() {
	if u.loaded {
		return
	}
	u.loaded = true
	u.plans = map[string]aiRenamePlan{}
	u.applied = map[string]bool{}
	if u.width < assistantMinW {
		u.width = assistantPaneW
	}
	if dir, err := os.UserConfigDir(); err == nil {
		u.diskPath = filepath.Join(dir, stateDirName, "assistant.json")
	}
	if u.diskPath == "" {
		return
	}
	u.readConfig()
	u.loadSide()
}

func (u *assistantUI) readConfig() {
	assistantConfigMu.Lock()
	defer assistantConfigMu.Unlock()
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	u.readConfigLocked()
}

// refreshConfig merges a headless writer's changes before the pane mutates
// one of the shared lists. It always merges so unsaved local changes survive.
func (u *assistantUI) refreshConfig() {
	if u.baseAbsent {
		return
	}
	if u.diskPath == "" || u.configBroken {
		return
	}
	if assistantConfigStamp() == u.baseConfigEpoch {
		return
	}
	assistantConfigMu.Lock()
	defer assistantConfigMu.Unlock()
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	if assistantConfigStamp() == u.baseConfigEpoch {
		return
	}
	disk, absent, err := readAssistantDisk(u.diskPath)
	if err != nil {
		u.configBroken = true
		return
	}
	if absent {
		if len(u.providers) == 0 && len(u.passwords) == 0 {
			u.baseAbsent = true
		}
		return
	}
	u.baseAbsent = false
	u.mergeConfigLocked(disk)
}

func (u *assistantUI) readConfigLocked() {
	data, err := os.ReadFile(u.diskPath)
	if err != nil {
		u.baseAbsent = os.IsNotExist(err)
		u.savedFull = u.full
		u.savedAutopilot = u.autopilot
		u.snapshotConfig()
		return
	}
	var disk assistantDisk
	if json.Unmarshal(data, &disk) != nil {
		// A torn config still holds API keys. Later session saves must not
		// replace it with an empty provider list.
		u.configBroken = true
		u.savedFull = u.full
		u.savedAutopilot = u.autopilot
		return
	}
	u.baseAbsent = false
	u.providers = disk.Providers
	u.activeID = disk.ActiveID
	u.passwords = disk.ArchivePasswords
	u.legacyPasswords = append(json.RawMessage(nil), disk.Passwords...)
	if disk.Prefs.Width >= assistantMinW {
		u.width = disk.Prefs.Width
	}
	u.full = disk.Prefs.Full
	u.showHist = disk.Prefs.HistOpen
	if disk.Prefs.HistWidth >= assistHistMinW {
		u.histW = disk.Prefs.HistWidth
	}
	u.savedHist = u.showHist
	u.autopilot = disk.Prefs.Autopilot
	u.extraInstruction = disk.Prefs.ExtraInstruction
	u.disabledSkills = append([]string(nil), disk.Prefs.DisabledSkills...)
	u.savedFull = u.full
	u.savedAutopilot = u.autopilot
	u.snapshotConfig()
}

func (u *assistantUI) save() {
	if u.diskPath == "" || u.configBroken {
		return
	}
	assistantConfigMu.Lock()
	defer assistantConfigMu.Unlock()
	u.writeConfigLocked()
}

func (u *assistantUI) writeConfigLocked() {
	assistantDiskMu.Lock()
	defer assistantDiskMu.Unlock()
	if assistantConfigStamp() != u.baseConfigEpoch {
		disk, _, err := readAssistantDisk(u.diskPath)
		if err != nil {
			u.configBroken = true
			return
		}
		u.mergeConfigLocked(disk)
	}
	disk := assistantDisk{
		Providers: u.providers, ActiveID: u.activeID, ArchivePasswords: u.passwords,
		Passwords: append(json.RawMessage(nil), u.legacyPasswords...),
		Prefs: assistPrefs{
			Width: u.width, Full: u.full, Autopilot: u.autopilot, HistOpen: u.showHist, HistWidth: u.histW,
			ExtraInstruction: u.extraInstruction, DisabledSkills: append([]string(nil), u.disabledSkills...),
		},
	}
	data, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		return
	}
	if writePrivate(u.diskPath, data) != nil {
		return
	}
	markAssistantConfigDirty()
	u.snapshotConfig()
}

// mergeConfigLocked is a three-way merge: base is what the pane last read,
// local is the pane's unsaved state, and disk is a headless writer's change.
// The pane wins only fields it actually changed, so a headless provider edit
// and a local width drag can both survive.
func (u *assistantUI) mergeConfigLocked(disk assistantDisk) {
	u.providers = assistant.MergeProviders(u.baseProviders, u.providers, disk.Providers)
	if u.activeID == u.baseActiveID {
		u.activeID = disk.ActiveID
	}
	u.passwords = mergePasswords(u.basePasswords, u.passwords, disk.ArchivePasswords)
	if len(disk.Passwords) > 0 {
		u.legacyPasswords = append(json.RawMessage(nil), disk.Passwords...)
	}
	if u.width == u.basePrefs.Width {
		u.width = disk.Prefs.Width
	}
	if u.full == u.basePrefs.Full {
		u.full = disk.Prefs.Full
	}
	if u.showHist == u.basePrefs.HistOpen {
		u.showHist = disk.Prefs.HistOpen
	}
	if u.histW == u.basePrefs.HistWidth && disk.Prefs.HistWidth >= assistHistMinW {
		u.histW = disk.Prefs.HistWidth
	}
	u.savedHist = u.showHist
	if u.autopilot == u.basePrefs.Autopilot {
		u.autopilot = disk.Prefs.Autopilot
	}
	if u.extraInstruction == u.basePrefs.ExtraInstruction {
		u.extraInstruction = disk.Prefs.ExtraInstruction
	}
	if equalStringSlice(u.disabledSkills, u.basePrefs.DisabledSkills) {
		u.disabledSkills = append([]string(nil), disk.Prefs.DisabledSkills...)
	}
	u.savedFull = u.full
	u.savedAutopilot = u.autopilot
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (u *assistantUI) snapshotConfig() {
	u.baseProviders = cloneProviders(u.providers)
	u.baseActiveID = u.activeID
	u.basePasswords = clonePasswords(u.passwords)
	u.basePrefs = assistPrefs{
		Width: u.width, Full: u.full, Autopilot: u.autopilot, HistOpen: u.showHist, HistWidth: u.histW,
		ExtraInstruction: u.extraInstruction, DisabledSkills: append([]string(nil), u.disabledSkills...),
	}
	u.baseConfigEpoch = assistantConfigStamp()
}

func cloneProviders(in []aiProvider) []aiProvider {
	return append([]aiProvider(nil), in...)
}

func clonePasswords(in []archivePassword) []archivePassword {
	return append([]archivePassword(nil), in...)
}

func mergePasswords(base, local, disk []archivePassword) []archivePassword {
	baseByID := make(map[string]archivePassword, len(base))
	for _, p := range base {
		baseByID[p.ID] = p
	}
	diskByID := make(map[string]archivePassword, len(disk))
	for _, p := range disk {
		diskByID[p.ID] = p
	}
	localByID := make(map[string]bool, len(local))
	out := make([]archivePassword, 0, len(local)+len(disk))
	for _, p := range local {
		localByID[p.ID] = true
		old, hadOld := baseByID[p.ID]
		fresh, hadFresh := diskByID[p.ID]
		switch {
		case !hadOld:
			out = append(out, p)
		case p == old:
			if hadFresh {
				out = append(out, fresh)
			}
		default:
			out = append(out, p)
		}
	}
	for _, p := range disk {
		if _, hadOld := baseByID[p.ID]; !hadOld && !localByID[p.ID] {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (u *assistantUI) providerLabel() string {
	p, ok := u.activeProvider()
	if !ok {
		return "未配置供应商"
	}
	return p.Name + " · " + p.Model
}

func (u *assistantUI) activeProvider() (aiProvider, bool) {
	return assistant.ActiveProvider(u.providers, u.activeID)
}

func (u *assistantUI) addProvider() {
	name := strings.TrimSpace(u.pName)
	base := strings.TrimSpace(u.pURL)
	model := strings.TrimSpace(u.pModel)
	key := strings.TrimSpace(u.pKey)
	if name == "" || base == "" || model == "" || key == "" {
		return
	}
	u.refreshConfig()
	p := aiProvider{ID: newID(), Name: name, BaseURL: base, Model: model, APIKey: key}
	u.providers = append(u.providers, p)
	u.activeID = p.ID
	u.pName, u.pURL, u.pModel, u.pKey = "", "", "", ""
	u.save()
}

func (u *assistantUI) deleteProvider(id string) {
	u.refreshConfig()
	next := u.providers[:0]
	for _, p := range u.providers {
		if p.ID != id {
			next = append(next, p)
		}
	}
	u.providers = next
	if u.activeID == id {
		u.activeID = ""
		if len(u.providers) > 0 {
			u.activeID = u.providers[0].ID
		}
	}
	u.save()
}

func (u *assistantUI) addArchivePassword() {
	secret := u.pwSecret
	if secret == "" {
		return
	}
	u.refreshConfig()
	label := strings.TrimSpace(u.pwName)
	if label == "" {
		label = fmt.Sprintf("解压密码 %d", len(u.passwords)+1)
	}
	u.passwords = append(u.passwords, archivePassword{ID: newID(), Label: label, Secret: secret})
	if len(u.passwords) > assistMaxArchivePasswords {
		u.passwords = u.passwords[len(u.passwords)-assistMaxArchivePasswords:]
	}
	u.pwName, u.pwSecret = "", ""
	u.save()
}

func (u *assistantUI) deleteArchivePassword(id string) {
	u.refreshConfig()
	next := u.passwords[:0]
	for _, p := range u.passwords {
		if p.ID != id {
			next = append(next, p)
		}
	}
	u.passwords = next
	u.save()
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
