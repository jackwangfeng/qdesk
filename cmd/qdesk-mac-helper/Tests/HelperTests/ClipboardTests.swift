import XCTest
import AppKit
@testable import Helper

final class ClipboardTests: XCTestCase {
    func testBackupAndRestoreString() {
        // Seed the pasteboard with a known string.
        let pb = NSPasteboard.general
        pb.clearContents()
        pb.setString("seed-value", forType: .string)

        // Backup, then mutate, then restore.
        let backup = backupPasteboard()
        pb.clearContents()
        pb.setString("transient", forType: .string)
        XCTAssertEqual(pb.string(forType: .string), "transient")
        restorePasteboard(backup)
        XCTAssertEqual(pb.string(forType: .string), "seed-value")
    }

    func testClipboardPasteRestoresOriginal() throws {
        let h = health()
        try XCTSkipUnless(h.accessibilityGranted, "Accessibility not granted; skipping (cmd+v post will fail without it)")

        let pb = NSPasteboard.general
        pb.clearContents()
        pb.setString("original", forType: .string)

        // clipboardPaste does NSPasteboard write + cmd+v + sleep + restore.
        // No app has focus that will accept the paste, but the function
        // must still complete and restore the original.
        try clipboardPaste(text: "transient-payload")

        XCTAssertEqual(pb.string(forType: .string), "original",
                       "clipboard not restored to original")
    }
}
