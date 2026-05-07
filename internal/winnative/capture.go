//go:build windows

package winnative

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"unsafe"

	"github.com/jeffwang/qdesk/internal/winserver"
	"golang.org/x/sys/windows"
)

var (
	gdi32 = windows.NewLazySystemDLL("gdi32.dll")

	procGetDC              = user32.NewProc("GetDC")
	procReleaseDC          = user32.NewProc("ReleaseDC")
	procGetSystemMetrics   = user32.NewProc("GetSystemMetrics")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procBitBlt             = gdi32.NewProc("BitBlt")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
)

const (
	smCXScreen = 0
	smCYScreen = 1
	srcCopy    = 0x00CC0020
	biRGB      = 0
	dibRGB     = 0
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32 // unused; placeholder to match ABI
}

func screenshot() (winserver.Screenshot, error) {
	w, _, _ := procGetSystemMetrics.Call(smCXScreen)
	h, _, _ := procGetSystemMetrics.Call(smCYScreen)
	if w == 0 || h == 0 {
		return winserver.Screenshot{}, fmt.Errorf("GetSystemMetrics returned zero size")
	}

	srcDC, _, _ := procGetDC.Call(0)
	if srcDC == 0 {
		return winserver.Screenshot{}, fmt.Errorf("GetDC failed")
	}
	defer procReleaseDC.Call(0, srcDC)

	memDC, _, _ := procCreateCompatibleDC.Call(srcDC)
	if memDC == 0 {
		return winserver.Screenshot{}, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(memDC)

	// CreateDIBSection gives us a bitmap whose pixel buffer we can
	// access directly via `bits` — no GetDIBits step. BitBlt populates
	// the buffer in-place. Top-down DIB (negative height) so memory
	// order matches image.RGBA expectations.
	bi := bitmapInfo{Header: bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       int32(w),
		Height:      -int32(h),
		Planes:      1,
		BitCount:    32,
		Compression: biRGB,
	}}
	var bits unsafe.Pointer
	bmp, _, lastErr := procCreateDIBSection.Call(srcDC,
		uintptr(unsafe.Pointer(&bi)), dibRGB,
		uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		return winserver.Screenshot{}, fmt.Errorf("CreateDIBSection failed (lastErr=%v)", lastErr)
	}
	defer procDeleteObject.Call(bmp)

	procSelectObject.Call(memDC, bmp)
	r, _, _ := procBitBlt.Call(memDC, 0, 0, w, h, srcDC, 0, 0, srcCopy)
	if r == 0 {
		return winserver.Screenshot{}, fmt.Errorf("BitBlt failed")
	}

	// Copy pixels out of the DIB section before we DeleteObject it.
	// The DIB section memory belongs to the bitmap; once deleted it's
	// gone. unsafe.Slice gives us a Go-managed view of len pixels*4.
	pixelCount := int(w * h)
	src := unsafe.Slice((*byte)(bits), pixelCount*4)
	pixels := make([]byte, pixelCount*4)
	copy(pixels, src)

	// BGRA → RGBA in place.
	for i := 0; i < len(pixels); i += 4 {
		pixels[i], pixels[i+2] = pixels[i+2], pixels[i]
	}

	img := &image.RGBA{
		Pix:    pixels,
		Stride: int(w) * 4,
		Rect:   image.Rect(0, 0, int(w), int(h)),
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return winserver.Screenshot{}, fmt.Errorf("png encode: %w", err)
	}
	return winserver.Screenshot{
		PNGBase64: base64.StdEncoding.EncodeToString(buf.Bytes()),
		Width:     int(w),
		Height:    int(h),
	}, nil
}
