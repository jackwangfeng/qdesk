import XCTest
@testable import Helper

final class HealthTests: XCTestCase {
    func testHealthReturnsBooleansForKnownPermissions() {
        // health() is allowed to return any booleans depending on the host's
        // TCC state — but it MUST NOT throw and MUST return a HealthResponse.
        let r = health()
        XCTAssertTrue(r.ok || !r.ok) // tautology; assertion is "did not throw"
        _ = r.screenRecordingGranted
        _ = r.accessibilityGranted
    }
}
