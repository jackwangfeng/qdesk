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

    /// Verifies the pasteboard mid-paste actually contains `text` AND
    /// that the original is restored after — without posting a real
    /// cmd+v event globally. v1.1's earlier version of this test
    /// posted cmd+v which would land on whatever app was in focus
    /// (often the running terminal / Claude Code), polluting it.
    func testClipboardPasteImplObservesNewTextThenRestores() throws {
        let pb = NSPasteboard.general
        pb.clearContents()
        pb.setString("original", forType: .string)

        var observedDuringPaste: String?
        try clipboardPasteImpl(text: "transient-payload") {
            observedDuringPaste = pb.string(forType: .string)
        }

        XCTAssertEqual(observedDuringPaste, "transient-payload",
                       "pasteboard during paste should contain the new text")
        XCTAssertEqual(pb.string(forType: .string), "original",
                       "clipboard not restored to original")
    }

    /// If the paste closure throws, the pasteboard must still be
    /// restored — never leave the user with the transient text on
    /// their clipboard.
    func testClipboardPasteImplRestoresOnPasteFailure() {
        let pb = NSPasteboard.general
        pb.clearContents()
        pb.setString("original", forType: .string)

        struct BoomError: Error {}
        XCTAssertThrowsError(try clipboardPasteImpl(text: "transient") {
            throw BoomError()
        })
        XCTAssertEqual(pb.string(forType: .string), "original",
                       "clipboard must be restored even when the paste closure throws")
    }
}
