// Package keymap parses combo strings like "ctrl+f" / "win+r" /
// "alt+tab" into a sequence of virtual-key down/up events. Pure
// logic — no Win32 dependency — so it's tested on macOS dev hosts.
//
// VK codes match the Windows virtual-key constants (User32 VK_*),
// referenced by the windows-only winnative package when building
// SendInput INPUT structs.
package keymap

import (
	"fmt"
	"strings"
)

// Event is one keyboard transition.
type Event struct {
	VK   uint16
	Down bool
}

// Subset of Win32 VK_* values we use. Add more as needed.
const (
	VKBack    = 0x08
	VKTab     = 0x09
	VKReturn  = 0x0D
	VKShift   = 0x10
	VKControl = 0x11
	VKMenu    = 0x12 // Alt
	VKEscape  = 0x1B
	VKSpace   = 0x20
	VKLeft    = 0x25
	VKUp      = 0x26
	VKRight   = 0x27
	VKDown    = 0x28
	VKDelete  = 0x2E
	VKLWin    = 0x5B
	VKF1      = 0x70
	VKF2      = 0x71
	VKF3      = 0x72
	VKF4      = 0x73
	VKF5      = 0x74
	VKF6      = 0x75
	VKF7      = 0x76
	VKF8      = 0x77
	VKF9      = 0x78
	VKF10     = 0x79
	VKF11     = 0x7A
	VKF12     = 0x7B
)

var modifierVK = map[string]uint16{
	"ctrl":    VKControl,
	"control": VKControl,
	"shift":   VKShift,
	"alt":     VKMenu,
	"meta":    VKMenu, // alias
	"win":     VKLWin,
	"windows": VKLWin,
	"super":   VKLWin,
}

var namedKeyVK = map[string]uint16{
	"return":    VKReturn,
	"enter":     VKReturn,
	"tab":       VKTab,
	"escape":    VKEscape,
	"esc":       VKEscape,
	"backspace": VKBack,
	"delete":    VKDelete,
	"del":       VKDelete,
	"space":     VKSpace,
	"left":      VKLeft,
	"right":     VKRight,
	"up":        VKUp,
	"down":      VKDown,
	"f1":        VKF1, "f2": VKF2, "f3": VKF3, "f4": VKF4,
	"f5": VKF5, "f6": VKF6, "f7": VKF7, "f8": VKF8,
	"f9": VKF9, "f10": VKF10, "f11": VKF11, "f12": VKF12,
}

// Parse turns "ctrl+shift+a" into the press/release sequence for
// SendInput: each modifier down (in order), the main key down/up,
// then each modifier up (reverse order).
//
// The "main key" is the LAST token; everything before must be a
// known modifier. ASCII letters and digits map to their uppercase
// rune (Windows VK codes for A-Z and 0-9 are the ASCII codepoints).
func Parse(combo string) ([]Event, error) {
	combo = strings.TrimSpace(combo)
	if combo == "" {
		return nil, fmt.Errorf("empty combo")
	}
	parts := strings.Split(strings.ToLower(combo), "+")
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty combo")
	}

	mods := parts[:len(parts)-1]
	mainKey := parts[len(parts)-1]

	modVKs := make([]uint16, 0, len(mods))
	for _, m := range mods {
		vk, ok := modifierVK[m]
		if !ok {
			return nil, fmt.Errorf("unknown modifier: %q", m)
		}
		modVKs = append(modVKs, vk)
	}

	mainVK, err := resolveKey(mainKey)
	if err != nil {
		return nil, err
	}

	out := make([]Event, 0, 2+2*len(modVKs))
	for _, vk := range modVKs {
		out = append(out, Event{VK: vk, Down: true})
	}
	out = append(out, Event{VK: mainVK, Down: true}, Event{VK: mainVK, Down: false})
	for i := len(modVKs) - 1; i >= 0; i-- {
		out = append(out, Event{VK: modVKs[i], Down: false})
	}
	return out, nil
}

func resolveKey(k string) (uint16, error) {
	if vk, ok := namedKeyVK[k]; ok {
		return vk, nil
	}
	if len(k) == 1 {
		c := k[0]
		switch {
		case c >= 'a' && c <= 'z':
			return uint16(c - 32), nil // upper-case rune
		case c >= '0' && c <= '9':
			return uint16(c), nil
		}
	}
	return 0, fmt.Errorf("unknown key: %q", k)
}
