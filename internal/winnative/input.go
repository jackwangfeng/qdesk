//go:build windows

package winnative

import (
	"fmt"
	"unsafe"

	"github.com/jeffwang/qdesk/internal/winserver"
	"github.com/jeffwang/qdesk/internal/winserver/keymap"
)

var (
	procSetCursorPos = user32.NewProc("SetCursorPos")
	procSendInput    = user32.NewProc("SendInput")
)

const (
	inputMouse    = 0
	inputKeyboard = 1

	mouseEventLeftDown   = 0x0002
	mouseEventLeftUp     = 0x0004
	mouseEventRightDown  = 0x0008
	mouseEventRightUp    = 0x0010
	mouseEventMiddleDown = 0x0020
	mouseEventMiddleUp   = 0x0040
	mouseEventWheel      = 0x0800
	mouseEventHWheel     = 0x01000

	keyEventKeyUp   = 0x0002
	keyEventUnicode = 0x0004
)

// INPUT struct for SendInput. Total size on x64 is 40 bytes
// (8-byte type + pad + 32-byte union).
type input struct {
	Type uint32
	_    uint32 // pad to 8-byte align mi
	Data [32]byte
}

type mouseInput struct {
	DX        int32
	DY        int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

type keybdInput struct {
	WVk       uint16
	WScan     uint16
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

func click(req winserver.ClickReq) error {
	procSetCursorPos.Call(uintptr(req.X), uintptr(req.Y))

	mainDown, mainUp, err := mouseFlags(req.Button)
	if err != nil {
		return err
	}
	count := 1
	if req.Double {
		count = 2
	}

	for _, m := range req.Modifiers {
		if vk, ok := modVK(m); ok {
			sendKey(vk, false)
		}
	}
	for i := 0; i < count; i++ {
		sendMouse(mainDown)
		sendMouse(mainUp)
	}
	for i := len(req.Modifiers) - 1; i >= 0; i-- {
		if vk, ok := modVK(req.Modifiers[i]); ok {
			sendKey(vk, true)
		}
	}
	return nil
}

func modVK(m string) (uint16, bool) {
	switch m {
	case "ctrl":
		return keymap.VKControl, true
	case "shift":
		return keymap.VKShift, true
	case "alt":
		return keymap.VKMenu, true
	case "win":
		return keymap.VKLWin, true
	}
	return 0, false
}

func mouseFlags(button string) (uint32, uint32, error) {
	switch button {
	case "", "left":
		return mouseEventLeftDown, mouseEventLeftUp, nil
	case "right":
		return mouseEventRightDown, mouseEventRightUp, nil
	case "middle":
		return mouseEventMiddleDown, mouseEventMiddleUp, nil
	}
	return 0, 0, fmt.Errorf("unknown button %q", button)
}

func sendMouse(flags uint32) {
	in := input{Type: inputMouse}
	mi := mouseInput{Flags: flags}
	*(*mouseInput)(unsafe.Pointer(&in.Data)) = mi
	procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

// sendKey sends one keyboard transition. up=true means release.
func sendKey(vk uint16, up bool) {
	in := input{Type: inputKeyboard}
	ki := keybdInput{WVk: vk}
	if up {
		ki.Flags |= keyEventKeyUp
	}
	*(*keybdInput)(unsafe.Pointer(&in.Data)) = ki
	procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

// sendUnicodeChar uses KEYEVENTF_UNICODE to inject one UTF-16 code unit
// (surrogates handled by the caller).
func sendUnicodeChar(ch uint16) {
	for _, up := range []bool{false, true} {
		in := input{Type: inputKeyboard}
		ki := keybdInput{WScan: ch, Flags: keyEventUnicode}
		if up {
			ki.Flags |= keyEventKeyUp
		}
		*(*keybdInput)(unsafe.Pointer(&in.Data)) = ki
		procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	}
}

func typeText(text string) error {
	utf16 := []uint16{}
	for _, r := range text {
		if r < 0x10000 {
			utf16 = append(utf16, uint16(r))
		} else {
			r -= 0x10000
			utf16 = append(utf16, 0xD800+uint16(r>>10), 0xDC00+uint16(r&0x3FF))
		}
	}
	for _, u := range utf16 {
		sendUnicodeChar(u)
	}
	return nil
}

func keyCombo(combo string) error {
	events, err := keymap.Parse(combo)
	if err != nil {
		return err
	}
	for _, e := range events {
		sendKey(e.VK, !e.Down)
	}
	return nil
}

func scroll(req winserver.ScrollReq) error {
	procSetCursorPos.Call(uintptr(req.X), uintptr(req.Y))
	if req.DY != 0 {
		// 120 wheel notches per delta unit.
		in := input{Type: inputMouse}
		mi := mouseInput{Flags: mouseEventWheel, MouseData: uint32(int32(req.DY * 120))}
		*(*mouseInput)(unsafe.Pointer(&in.Data)) = mi
		procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	}
	if req.DX != 0 {
		in := input{Type: inputMouse}
		mi := mouseInput{Flags: mouseEventHWheel, MouseData: uint32(int32(req.DX * 120))}
		*(*mouseInput)(unsafe.Pointer(&in.Data)) = mi
		procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	}
	return nil
}
