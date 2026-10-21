package schedule

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Built-in assistant job presets (SCH2). file_rule stays extension-based;
// content classification lives only in these assistant prompts.
const (
	PresetClassifyDownloads = "classify_downloads"
	PresetInboxTriage       = "inbox_triage"
	PresetRenameByContent   = "rename_by_content"
)

// Preset is one selectable assistant template: stable ID, Chinese label,
// and a headless Autopilot prompt. Prompt may contain {{folder}} and
// {{dest}} placeholders filled by ApplyPreset.
type Preset struct {
	ID      string
	Label   string
	Summary string
	Prompt  string
}

// AllPresets returns built-in templates in UI order.
func AllPresets() []Preset {
	return []Preset{
		{
			ID:      PresetClassifyDownloads,
			Label:   "分类下载",
			Summary: "列出下载，读文本/PDF 摘要，按主题移到目标下的子文件夹",
			Prompt: `你在全托管模式下整理下载目录。
工作目录：{{folder}}
目标根目录：{{dest}}（若不存在请先 mkdir）

步骤：
1. 用 list_dir / folder_summary 查看工作目录里的直接子项（不要递归整盘）。
2. 对文本、Markdown、可读的文档，用 read_file / read_files 读开头摘要；PDF 能读则读，不能读则按文件名与扩展名判断。
3. 按内容主题（不要只看扩展名）把每个文件移动到目标根下的子文件夹之一：
   - 文档：报告、合同、笔记、表格、幻灯片等
   - 图片：照片、截图、设计图
   - 安装包：安装程序、磁盘镜像、压缩安装包
   - 其他：无法判断或杂项
4. 子文件夹不存在就创建。用 move_items 移动；不要删除原文件。
5. 完成后用一两句中文说明各类各移了多少、目标路径。`,
		},
		{
			ID:      PresetInboxTriage,
			Label:   "收件箱整理",
			Summary: "整理指定文件夹：读内容后按主题归类到子文件夹",
			Prompt: `你在全托管模式下整理收件箱式文件夹。
工作目录：{{folder}}
目标根目录：{{dest}}（默认可与工作目录相同，在其下建子文件夹；若不存在请先创建）

步骤：
1. list_dir / folder_summary 查看直接子项。
2. 对可读文本与文档读开头摘要，结合文件名判断主题。
3. 在目标根下建立合适的主题子文件夹（例如：待办、资料、图片、归档、其他——按实际内容命名，简体中文，不超过 8 个字）。
4. 用 move_items 归类；不要删除。跳过已是合理子文件夹的目录。
5. 用一两句中文汇报结果与路径。`,
		},
		{
			ID:      PresetRenameByContent,
			Label:   "按内容重命名",
			Summary: "阅读文档后给出有意义的文件名并应用",
			Prompt: `你在全托管模式下按内容重命名文件。
工作目录：{{folder}}

步骤：
1. list_dir 列出直接子项中的文件（跳过隐藏项与子文件夹，除非用户另有说明）。
2. 对文本/Markdown/可读文档用 read_file 或 read_files 读开头，归纳简短主题。
3. 用 rename_plan 生成批量重命名方案：新名应简短、表意、保留原扩展名，使用简体中文或原文关键术语，避免特殊字符。
4. 全托管下方案会自动应用；不要再用 rename_item 逐个改。
5. 用一两句中文说明改了哪些名字。`,
		},
	}
}

// PresetByID looks up a built-in preset.
func PresetByID(id string) (Preset, bool) {
	id = strings.TrimSpace(id)
	for _, p := range AllPresets() {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

// PresetLabel returns the Chinese label for id, or id itself.
func PresetLabel(id string) string {
	if p, ok := PresetByID(id); ok {
		return p.Label
	}
	return strings.TrimSpace(id)
}

// DefaultFolderForPreset returns a sensible absolute folder for a preset.
func DefaultFolderForPreset(id string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch id {
	case PresetClassifyDownloads:
		return filepath.Join(home, "Downloads")
	default:
		return home
	}
}

// DefaultDestForPreset returns a dest root under home for classification presets.
func DefaultDestForPreset(id string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch id {
	case PresetClassifyDownloads:
		return filepath.Join(home, "Documents", "来自下载")
	default:
		return ""
	}
}

// FillPresetPrompt substitutes {{folder}} and {{dest}} in a preset prompt.
func FillPresetPrompt(tmpl, folder, dest string) string {
	s := tmpl
	s = strings.ReplaceAll(s, "{{folder}}", folder)
	s = strings.ReplaceAll(s, "{{dest}}", dest)
	return s
}

// ApplyPreset sets Action=assistant, Preset, Folder, Prompt (and DestDir as
// the classification root when relevant). Empty folder/dest use defaults.
func ApplyPreset(j *Job, presetID, folder, dest string) error {
	p, ok := PresetByID(presetID)
	if !ok {
		return fmt.Errorf("未知助手预设：%s", presetID)
	}
	if folder == "" {
		folder = DefaultFolderForPreset(presetID)
	}
	if dest == "" {
		dest = DefaultDestForPreset(presetID)
	}
	if dest == "" {
		dest = folder
	}
	j.Action = ActionAssistant
	j.Preset = p.ID
	j.Folder = folder
	j.DestDir = dest
	j.Prompt = FillPresetPrompt(p.Prompt, folder, dest)
	if j.Name == "" {
		j.Name = p.Label
	}
	return nil
}

// ResolvePresetID accepts English IDs or Chinese labels from MCP/UI.
func ResolvePresetID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "自定义" || s == "custom" || s == "none" {
		return ""
	}
	if _, ok := PresetByID(s); ok {
		return s
	}
	for _, p := range AllPresets() {
		if p.Label == s {
			return p.ID
		}
	}
	return s
}
