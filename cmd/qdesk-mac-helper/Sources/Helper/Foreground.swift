import Foundation
import AppKit

struct FrontAppResponse: Encodable {
    let bundleId: String
    let name: String
    let pid: Int
}

func frontApp() -> FrontAppResponse {
    let app = NSWorkspace.shared.frontmostApplication
    return FrontAppResponse(
        bundleId: app?.bundleIdentifier ?? "",
        name: app?.localizedName ?? "",
        pid: Int(app?.processIdentifier ?? 0)
    )
}

struct ActivateRequest: Decodable {
    let bundleId: String
}

func activate(_ req: ActivateRequest) throws {
    guard let app = NSRunningApplication.runningApplications(withBundleIdentifier: req.bundleId).first else {
        throw HelperRPCError(code: "wechat-not-running",
                             message: "app with bundle ID \(req.bundleId) is not running")
    }
    app.activate(options: [.activateAllWindows])
}

struct HelperRPCError: Error {
    let code: String
    let message: String
}
