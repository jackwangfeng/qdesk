import Foundation
import CoreGraphics

func clickGlobal(x: Double, y: Double, button: String, clicks: Int) throws {
    let pt = CGPoint(x: x, y: y)
    let mouseButton: CGMouseButton
    let downType: CGEventType
    let upType: CGEventType
    switch button {
    case "left":
        mouseButton = .left; downType = .leftMouseDown; upType = .leftMouseUp
    case "right":
        mouseButton = .right; downType = .rightMouseDown; upType = .rightMouseUp
    case "middle":
        mouseButton = .center; downType = .otherMouseDown; upType = .otherMouseUp
    default:
        throw HelperRPCError(code: "internal", message: "unknown button: \(button)")
    }
    for i in 1...max(1, clicks) {
        guard let down = CGEvent(mouseEventSource: nil, mouseType: downType,
                                 mouseCursorPosition: pt, mouseButton: mouseButton),
              let up = CGEvent(mouseEventSource: nil, mouseType: upType,
                               mouseCursorPosition: pt, mouseButton: mouseButton)
        else {
            throw HelperRPCError(code: "internal", message: "create CGEvent failed")
        }
        down.setIntegerValueField(.mouseEventClickState, value: Int64(i))
        up.setIntegerValueField(.mouseEventClickState, value: Int64(i))
        down.post(tap: .cghidEventTap)
        up.post(tap: .cghidEventTap)
    }
}

func typeText(_ text: String) throws {
    // Unicode mode: send each scalar via keyboard event with Unicode payload.
    // This bypasses the active IME, which is what we want for WeChat input
    // boxes that may otherwise interfere.
    for scalar in text.unicodeScalars {
        guard let down = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: true),
              let up = CGEvent(keyboardEventSource: nil, virtualKey: 0, keyDown: false)
        else {
            throw HelperRPCError(code: "internal", message: "create keyboard CGEvent failed")
        }
        let utf16 = Array(String(scalar).utf16)
        utf16.withUnsafeBufferPointer { buf in
            down.keyboardSetUnicodeString(stringLength: buf.count, unicodeString: buf.baseAddress)
            up.keyboardSetUnicodeString(stringLength: buf.count, unicodeString: buf.baseAddress)
        }
        down.post(tap: .cghidEventTap)
        up.post(tap: .cghidEventTap)
    }
}

func resolveKeyCombo(_ combo: String) throws -> (CGKeyCode, CGEventFlags) {
    var flags: CGEventFlags = []
    var keyName = combo.lowercased()
    let parts = combo.lowercased().split(separator: "+").map(String.init)
    if parts.count > 1 {
        for mod in parts.dropLast() {
            switch mod {
            case "cmd", "command": flags.insert(.maskCommand)
            case "shift": flags.insert(.maskShift)
            case "alt", "option", "opt": flags.insert(.maskAlternate)
            case "ctrl", "control": flags.insert(.maskControl)
            default:
                throw HelperRPCError(code: "internal", message: "unknown modifier: \(mod)")
            }
        }
        keyName = parts.last!
    }
    let keyCode: CGKeyCode
    switch keyName {
    case "return", "enter": keyCode = 0x24
    case "tab": keyCode = 0x30
    case "space": keyCode = 0x31
    case "escape", "esc": keyCode = 0x35
    case "delete", "backspace": keyCode = 0x33
    case "left": keyCode = 0x7B
    case "right": keyCode = 0x7C
    case "down": keyCode = 0x7D
    case "up": keyCode = 0x7E
    case "a": keyCode = 0x00
    case "c": keyCode = 0x08
    case "v": keyCode = 0x09
    case "x": keyCode = 0x07
    case "z": keyCode = 0x06
    default:
        throw HelperRPCError(code: "internal", message: "unknown key: \(keyName)")
    }
    return (keyCode, flags)
}

func sendKey(_ combo: String) throws {
    let (code, flags) = try resolveKeyCombo(combo)
    guard let down = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: true),
          let up = CGEvent(keyboardEventSource: nil, virtualKey: code, keyDown: false)
    else {
        throw HelperRPCError(code: "internal", message: "create CGEvent failed")
    }
    down.flags = flags
    up.flags = flags
    down.post(tap: .cghidEventTap)
    up.post(tap: .cghidEventTap)
}

func scroll(x: Double, y: Double, dx: Double, dy: Double) throws {
    guard let ev = CGEvent(scrollWheelEvent2Source: nil,
                           units: .line, wheelCount: 2,
                           wheel1: Int32(dy), wheel2: Int32(dx),
                           wheel3: 0)
    else {
        throw HelperRPCError(code: "internal", message: "create scroll CGEvent failed")
    }
    ev.location = CGPoint(x: x, y: y)
    ev.post(tap: .cghidEventTap)
}
