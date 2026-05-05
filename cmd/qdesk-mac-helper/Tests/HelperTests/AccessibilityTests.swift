import XCTest
@testable import Helper

final class AccessibilityTests: XCTestCase {
    func testAXTreeReturnsArrayShape() throws {
        let h = health()
        try XCTSkipUnless(h.accessibilityGranted, "Accessibility not granted; skipping")
        // Finder is always running. Query its full tree (no filter).
        let r = try axTree(bundleId: "com.apple.finder", query: "")
        // Either Finder has a window (nodes non-empty) or it doesn't —
        // we only assert the call returns without throwing.
        _ = r.nodes
    }

    func testQueryFilterRestrictsByRole() throws {
        let h = health()
        try XCTSkipUnless(h.accessibilityGranted, "Accessibility not granted; skipping")
        let r = try axTree(bundleId: "com.apple.finder", query: "role=AXWindow")
        for n in r.nodes {
            XCTAssertEqual(n.role, "AXWindow")
        }
    }
}
