import Foundation
import ApplicationServices
import CoreGraphics

struct HealthResponse: Encodable {
    let ok: Bool
    let screenRecordingGranted: Bool
    let accessibilityGranted: Bool
}

func health() -> HealthResponse {
    let sr = CGPreflightScreenCaptureAccess()
    let ax = AXIsProcessTrusted()
    return HealthResponse(
        ok: sr && ax,
        screenRecordingGranted: sr,
        accessibilityGranted: ax
    )
}
