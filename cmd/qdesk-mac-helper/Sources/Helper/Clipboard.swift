import Foundation
import AppKit
import CoreGraphics

/// PasteboardBackup holds the string contents of the pasteboard and the
/// changeCount at the time of capture. Non-string items (images, files)
/// are not preserved by v1.1; document this in the README.
struct PasteboardBackup {
    let string: String?
    let changeCount: Int
}

func backupPasteboard() -> PasteboardBackup {
    let pb = NSPasteboard.general
    return PasteboardBackup(
        string: pb.string(forType: .string),
        changeCount: pb.changeCount
    )
}

func restorePasteboard(_ backup: PasteboardBackup) {
    let pb = NSPasteboard.general
    pb.clearContents()
    if let s = backup.string {
        pb.setString(s, forType: .string)
    }
}

/// clipboardPaste:
///   1. Backup pasteboard.
///   2. Write `text` to pasteboard.
///   3. Post cmd+v to the focused app.
///   4. Wait 150 ms for the paste to consume the pasteboard.
///   5. Restore the original pasteboard contents.
///
/// The active app must accept paste. Caller is responsible for foreground
/// guard (Go side does this).
func clipboardPaste(text: String) throws {
    let backup = backupPasteboard()

    let pb = NSPasteboard.general
    pb.clearContents()
    pb.setString(text, forType: .string)

    // cmd+v: keycode 0x09 ('v') with Command modifier.
    let cmdV: CGKeyCode = 0x09
    guard let down = CGEvent(keyboardEventSource: nil, virtualKey: cmdV, keyDown: true),
          let up = CGEvent(keyboardEventSource: nil, virtualKey: cmdV, keyDown: false)
    else {
        restorePasteboard(backup)
        throw HelperRPCError(code: "internal", message: "create cmd+v CGEvent failed")
    }
    down.flags = .maskCommand
    up.flags = .maskCommand
    down.post(tap: .cghidEventTap)
    up.post(tap: .cghidEventTap)

    // Let the focused app consume the paste before we overwrite the
    // pasteboard. 150 ms is empirical — long enough for WeChat's input
    // box, short enough to feel instant.
    Thread.sleep(forTimeInterval: 0.15)

    restorePasteboard(backup)
}
