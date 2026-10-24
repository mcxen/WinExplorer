package main

import "fmt"

// deleteerr.go puts SHFileOperationW's return codes into the status line's
// wording. The API mixes its own DE_* range with raw Win32 errors, and a
// bare "删除错误 120" tells the user nothing; it is shared (not behind the
// windows build tag) so its table is testable on any platform.

// shellDeleteErrorText maps a SHFileOperationW return code to a reason, or
// keeps the number for the unmapped rest.
func shellDeleteErrorText(code int) string {
	switch code {
	case 0x2, 0x3: // Win32 file/path not found
		return "文件不存在或已被移动"
	case 0x5, 0x78: // Win32 access denied, DE_ACCESSDENIEDSRC
		return "拒绝访问：文件可能被占用或没有权限"
	case 0x20: // Win32 sharing violation
		return "文件正被其他程序占用"
	case 0x74: // DE_ROOTDIR
		return "不能删除驱动器根目录"
	case 0x7B: // Win32 invalid name
		return "文件名无效"
	case 0x7F: // DE_FILENAMETOOLONG
		return "路径过长（超过了 260 个字符）"
	case 0x85: // DE_ERRORONNET
		return "网络错误"
	case 0x10000: // DE_UNKNOWN_ERROR
		return "未知错误"
	}
	return fmt.Sprintf("删除错误 %d", code)
}
