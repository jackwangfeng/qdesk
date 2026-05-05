import XCTest
@testable import Helper

final class CaptureTests: XCTestCase {
    func testScreenshotReturnsPNG() async throws {
        // Will throw if Screen Recording permission not granted on this host.
        // We keep the test optional: skip if perm missing.
        let h = health()
        try XCTSkipUnless(h.screenRecordingGranted, "Screen Recording not granted; skipping")

        let r = try await screenshot()
        XCTAssertGreaterThan(r.width, 0)
        XCTAssertGreaterThan(r.height, 0)
        guard let data = Data(base64Encoded: r.pngBase64) else {
            return XCTFail("invalid base64")
        }
        // PNG magic
        XCTAssertEqual(Array(data.prefix(4)), [0x89, 0x50, 0x4E, 0x47])
    }
}
