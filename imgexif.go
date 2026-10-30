package main

import (
	"encoding/binary"
	"image"
	"image/draw"
)

// imgexif.go 让照片按相机写的 EXIF 方向竖起来。mygo 的 DecodeBitmap 会转，
// 但它解的是全尺寸；这里的解码先缩小再转，转一张 2048 px 的图只要几毫秒。

// jpegOrientation 读 JPEG 的 EXIF 方向（1–8），没有则 0。只扫图像数据之前的段。
func jpegOrientation(b []byte) int {
	if len(b) < 4 || b[0] != 0xff || b[1] != 0xd8 {
		return 0
	}
	for i := 2; i+4 <= len(b); {
		if b[i] != 0xff {
			return 0
		}
		marker, size := b[i+1], int(binary.BigEndian.Uint16(b[i+2:]))
		if marker == 0xda || size < 2 || i+2+size > len(b) {
			return 0
		}
		seg := b[i+4 : i+2+size]
		if marker == 0xe1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		i += 2 + size
	}
	return 0
}

func tiffOrientation(t []byte) int {
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0
	}
	if len(t) < 8 {
		return 0
	}
	ifd := int(bo.Uint32(t[4:]))
	if ifd < 8 || ifd+2 > len(t) {
		return 0
	}
	n := int(bo.Uint16(t[ifd:]))
	for e := ifd + 2; e+12 <= len(t) && n > 0; e, n = e+12, n-1 {
		if bo.Uint16(t[e:]) == 0x0112 && bo.Uint16(t[e+2:]) == 3 {
			if o := int(bo.Uint16(t[e+8:])); o >= 1 && o <= 8 {
				return o
			}
			return 0
		}
	}
	return 0
}

// orientImage 按方向 o 旋转/翻转 img；o ≤ 1 原样返回。
func orientImage(img image.Image, o int) image.Image {
	if o <= 1 || o > 8 {
		return img
	}
	src, ok := img.(*image.RGBA)
	if !ok {
		b := img.Bounds()
		src = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(src, src.Bounds(), img, b.Min, draw.Src)
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			var sx, sy int
			switch o {
			case 2:
				sx, sy = w-1-x, y
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sx, sy = x, h-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			si := src.PixOffset(b.Min.X+sx, b.Min.Y+sy)
			di := dst.PixOffset(x, y)
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}
