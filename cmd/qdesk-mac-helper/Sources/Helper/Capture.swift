import Foundation
import ScreenCaptureKit
import CoreImage
import AppKit

struct ScreenshotResponse: Encodable {
    let pngBase64: String
    let width: Int
    let height: Int
    let scaleFactor: Double
}

func screenshot() async throws -> ScreenshotResponse {
    let content = try await SCShareableContent.excludingDesktopWindows(
        false, onScreenWindowsOnly: true)
    guard let display = content.displays.first else {
        throw HelperRPCError(code: "internal", message: "no display found")
    }
    let cfg = SCStreamConfiguration()
    let scale = NSScreen.main?.backingScaleFactor ?? 2.0
    cfg.width = Int(Double(display.width) * scale)
    cfg.height = Int(Double(display.height) * scale)
    cfg.pixelFormat = kCVPixelFormatType_32BGRA
    cfg.showsCursor = false
    cfg.scalesToFit = true

    let filter = SCContentFilter(display: display, excludingApplications: [], exceptingWindows: [])
    let cgImage = try await SCScreenshotManager.captureImage(
        contentFilter: filter, configuration: cfg)

    // Encode PNG
    let bitmap = NSBitmapImageRep(cgImage: cgImage)
    guard let png = bitmap.representation(using: .png, properties: [:]) else {
        throw HelperRPCError(code: "internal", message: "PNG encode failed")
    }
    return ScreenshotResponse(
        pngBase64: png.base64EncodedString(),
        width: display.width,
        height: display.height,
        scaleFactor: scale
    )
}
