import XCTest
@testable import Helper

final class InputTests: XCTestCase {
    func testClickAtOffscreenIsNoOp() throws {
        let h = health()
        try XCTSkipUnless(h.accessibilityGranted, "Accessibility not granted; skipping")
        // Click far off-screen — should not crash, should not raise.
        try clickGlobal(x: -10_000, y: -10_000, button: "left", clicks: 1)
    }

    func testKeyComboParsesKnownKeys() throws {
        XCTAssertNoThrow(try resolveKeyCombo("return"))
        XCTAssertNoThrow(try resolveKeyCombo("escape"))
        XCTAssertNoThrow(try resolveKeyCombo("cmd+v"))
        XCTAssertThrowsError(try resolveKeyCombo("totally-not-a-key"))
    }
}
