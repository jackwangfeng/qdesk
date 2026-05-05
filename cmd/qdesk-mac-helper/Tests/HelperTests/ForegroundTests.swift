import XCTest
@testable import Helper

final class ForegroundTests: XCTestCase {
    func testFrontAppReturnsNonEmptyBundleId() {
        // On any running macOS desktop something is in front (Finder at minimum).
        let r = frontApp()
        XCTAssertFalse(r.bundleId.isEmpty, "expected a non-empty front-app bundle ID")
        XCTAssertGreaterThan(r.pid, 0)
    }
}
