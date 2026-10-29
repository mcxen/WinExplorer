//go:build windows

package main

import (
	"image"
	"runtime"
	"syscall"
	"unsafe"
)

// appicon_windows.go asks the Shell for an .exe's or .lnk's own icon
// through IShellItemImageFactory, the call Explorer's views use, on a
// thread of its own with COM initialised. It runs in thumb.go's background
// pool, never in the view.

var (
	gdi32                  = syscall.NewLazyDLL("gdi32.dll")
	user32Icon             = syscall.NewLazyDLL("user32.dll")
	procCoInitializeEx     = ole32.NewProc("CoInitializeEx")
	procCoUninitialize     = ole32.NewProc("CoUninitialize")
	procSHCreateItemFromPN = shell32.NewProc("SHCreateItemFromParsingName")
	procGetDIBits          = gdi32.NewProc("GetDIBits")
	procGetObjectW         = gdi32.NewProc("GetObjectW")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procGetDC              = user32Icon.NewProc("GetDC")
	procReleaseDC          = user32Icon.NewProc("ReleaseDC")
)

// IID_IShellItemImageFactory {bcc18b79-ba16-442f-80c4-8a59c30c463b}
var iidShellItemImageFactory = syscall.GUID{Data1: 0xbcc18b79, Data2: 0xba16, Data3: 0x442f,
	Data4: [8]byte{0x80, 0xc4, 0x8a, 0x59, 0xc3, 0x0c, 0x46, 0x3b}}

func appIconExt(ext string) bool {
	switch ext {
	case "exe", "lnk", "desktop":
		return true
	}
	return false
}

func platformAppIcon(path string, side int) image.Image {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	const coinitApartmentThreaded = 0x2
	if hr, _, _ := procCoInitializeEx.Call(0, coinitApartmentThreaded); int32(hr) >= 0 {
		defer procCoUninitialize.Call()
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil
	}
	var fac *struct{ vtbl *[4]uintptr }
	hr, _, _ := procSHCreateItemFromPN.Call(uintptr(unsafe.Pointer(p)), 0,
		uintptr(unsafe.Pointer(&iidShellItemImageFactory)), uintptr(unsafe.Pointer(&fac)))
	if int32(hr) < 0 || fac == nil {
		return nil
	}
	vtbl := fac.vtbl
	self := uintptr(unsafe.Pointer(fac))
	defer syscall.SyscallN(vtbl[2], self) // Release
	const siigbfBiggerSizeOK, siigbfIconOnly = 0x1, 0x4
	var hbm uintptr
	if unsafe.Sizeof(uintptr(0)) == 8 {
		size := uintptr(uint32(side)) | uintptr(uint32(side))<<32
		hr, _, _ = syscall.SyscallN(vtbl[3], self, size, siigbfBiggerSizeOK|siigbfIconOnly, uintptr(unsafe.Pointer(&hbm)))
	} else {
		hr, _, _ = syscall.SyscallN(vtbl[3], self, uintptr(side), uintptr(side), siigbfBiggerSizeOK|siigbfIconOnly, uintptr(unsafe.Pointer(&hbm)))
	}
	if int32(hr) < 0 || hbm == 0 {
		return nil
	}
	defer procDeleteObject.Call(hbm)
	return hbitmapImage(hbm)
}

// hbitmapImage copies a 32-bit HBITMAP into an image.RGBA (the Shell hands
// back premultiplied BGRA).
func hbitmapImage(hbm uintptr) image.Image {
	var bm struct {
		Type, Width, Height, WidthBytes int32
		Planes, BitsPixel               uint16
		Bits                            uintptr
	}
	if r, _, _ := procGetObjectW.Call(hbm, unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm))); r == 0 {
		return nil
	}
	w, h := int(bm.Width), int(bm.Height)
	if w <= 0 || h <= 0 || w > 1024 || h > 1024 {
		return nil
	}
	var bi struct {
		Size                          uint32
		Width, Height                 int32
		Planes, BitCount              uint16
		Compression, SizeImage        uint32
		XPels, YPels, ClrUsed, ClrImp int32
	}
	bi.Size = uint32(unsafe.Sizeof(bi))
	bi.Width, bi.Height = int32(w), -int32(h) // top-down
	bi.Planes, bi.BitCount = 1, 32
	buf := make([]byte, w*h*4)
	dc, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, dc)
	if r, _, _ := procGetDIBits.Call(dc, hbm, 0, uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), 0); r == 0 {
		return nil
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	opaque := true
	for i := 3; i < len(buf); i += 4 {
		if buf[i] != 0 {
			opaque = false
			break
		}
	}
	for i := 0; i < len(buf); i += 4 {
		a := buf[i+3]
		if opaque {
			a = 255
		}
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = buf[i+2], buf[i+1], buf[i], a
	}
	return img
}
