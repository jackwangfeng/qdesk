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

/// clipboardPasteImpl is the testable form. It performs the
/// backup → set → run-the-paste-action → wait → restore sequence
/// around an arbitrary `paste` closure. Production passes `postCmdV`;
/// unit tests pass a no-op closure (or one that observes the pasteboard
/// mid-flight) so cmd+v is not posted globally onto whatever app
/// happened to be in focus when the test ran.
func clipboardPasteImpl(text: String, paste: () throws -> Void) throws {
    let backup = backupPasteboard()

    let pb = NSPasteboard.general
    pb.clearContents()
    pb.setString(text, forType: .string)

    do {
        try paste()
    } catch {
        restorePasteboard(backup)
        throw error
    }

    // Let the focused app consume the paste before we overwrite the
    // pasteboard. 150 ms is empirical — long enough for WeChat's input
    // box, short enough to feel instant.
    Thread.sleep(forTimeInterval: 0.15)

    restorePasteboard(backup)
}

/// clipboardPaste is the production entry point: pasteboard plumbing
/// plus an actual cmd+v keystroke at the system event tap. Caller is
/// responsible for the foreground guard (Go side does this).
func clipboardPaste(text: String) throws {
    try clipboardPasteImpl(text: text, paste: postCmdV)
}

/// postCmdV synthesises a Command+V keystroke at the system event tap.
/// Pulled out as a free function so unit tests can pass a no-op closure
/// to clipboardPasteImpl — the global CGEvent post otherwise lands on
/// whatever app is in focus during the test run.
func postCmdV() throws {
    let cmdV: CGKeyCode = 0x09 // virtual key for 'v'
    guard let down = CGEvent(keyboardEventSource: nil, virtualKey: cmdV, keyDown: true),
          let up = CGEvent(keyboardEventSource: nil, virtualKey: cmdV, keyDown: false)
    else {
        throw HelperRPCError(code: "internal", message: "create cmd+v CGEvent failed")
    }
    down.flags = .maskCommand
    up.flags = .maskCommand
    down.post(tap: .cghidEventTap)
    up.post(tap: .cghidEventTap)
}
