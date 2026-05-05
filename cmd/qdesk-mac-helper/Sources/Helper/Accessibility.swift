import Foundation
import ApplicationServices
import AppKit

struct AXFrame: Encodable {
    let x, y, w, h: Double
}

struct AXNode: Encodable {
    let path: String
    let role: String
    let title: String?
    let value: String?
    let description: String?
    let frame: AXFrame
}

struct AXTreeResponse: Encodable {
    let nodes: [AXNode]
}

private func string(from el: AXUIElement, attr: String) -> String? {
    var raw: CFTypeRef?
    let r = AXUIElementCopyAttributeValue(el, attr as CFString, &raw)
    if r != .success { return nil }
    return raw as? String
}

private func frame(from el: AXUIElement) -> AXFrame {
    var pos: CFTypeRef?
    var size: CFTypeRef?
    AXUIElementCopyAttributeValue(el, kAXPositionAttribute as CFString, &pos)
    AXUIElementCopyAttributeValue(el, kAXSizeAttribute as CFString, &size)
    var p = CGPoint.zero
    var s = CGSize.zero
    if let pv = pos { AXValueGetValue(pv as! AXValue, .cgPoint, &p) }
    if let sv = size { AXValueGetValue(sv as! AXValue, .cgSize, &s) }
    return AXFrame(x: p.x, y: p.y, w: s.width, h: s.height)
}

// Caps for AX walks. Real WeChat sidebars are well under these; the caps
// just prevent runaway on apps with deep/wide trees (e.g. Finder's full
// desktop tree, which can hit thousands of icons).
private let axMaxNodesVisited = 5000
private let axMaxDeadline: TimeInterval = 3.0

private func walk(_ el: AXUIElement, path: String, into nodes: inout [AXNode],
                  filter: (String) -> Bool, depth: Int,
                  visited: inout Int, deadline: Date) {
    if depth > 200 { return }
    if visited >= axMaxNodesVisited { return }
    if Date() >= deadline { return }
    visited += 1
    let role = string(from: el, attr: kAXRoleAttribute as String) ?? ""
    if filter(role) {
        nodes.append(AXNode(
            path: path,
            role: role,
            title: string(from: el, attr: kAXTitleAttribute as String),
            value: string(from: el, attr: kAXValueAttribute as String),
            description: string(from: el, attr: kAXDescriptionAttribute as String),
            frame: frame(from: el)
        ))
    }
    var children: CFTypeRef?
    AXUIElementCopyAttributeValue(el, kAXChildrenAttribute as CFString, &children)
    if let arr = children as? [AXUIElement] {
        for (i, child) in arr.enumerated() {
            walk(child, path: path.isEmpty ? "\(i)" : "\(path)/\(i)",
                 into: &nodes, filter: filter, depth: depth + 1,
                 visited: &visited, deadline: deadline)
        }
    }
}

func axTree(bundleId: String, query: String) throws -> AXTreeResponse {
    guard let app = NSRunningApplication.runningApplications(withBundleIdentifier: bundleId).first else {
        throw HelperRPCError(code: "wechat-not-running",
                             message: "app \(bundleId) not running")
    }
    let appEl = AXUIElementCreateApplication(app.processIdentifier)
    var roleFilter: String? = nil
    if query.hasPrefix("role=") {
        roleFilter = String(query.dropFirst("role=".count))
    }
    let filter: (String) -> Bool = { role in
        if let rf = roleFilter { return role == rf }
        return true
    }
    var nodes: [AXNode] = []
    var visited = 0
    let deadline = Date().addingTimeInterval(axMaxDeadline)
    walk(appEl, path: "", into: &nodes, filter: filter, depth: 0,
         visited: &visited, deadline: deadline)
    return AXTreeResponse(nodes: nodes)
}

func axClick(bundleId: String, path: String) throws {
    guard let app = NSRunningApplication.runningApplications(withBundleIdentifier: bundleId).first else {
        throw HelperRPCError(code: "wechat-not-running",
                             message: "app \(bundleId) not running")
    }
    let appEl = AXUIElementCreateApplication(app.processIdentifier)
    let parts = path.split(separator: "/").compactMap { Int($0) }
    var cur: AXUIElement = appEl
    for idx in parts {
        var children: CFTypeRef?
        AXUIElementCopyAttributeValue(cur, kAXChildrenAttribute as CFString, &children)
        guard let arr = children as? [AXUIElement], idx < arr.count else {
            throw HelperRPCError(code: "internal",
                                 message: "AX path out of range at index \(idx)")
        }
        cur = arr[idx]
    }
    let r = AXUIElementPerformAction(cur, kAXPressAction as CFString)
    if r != .success {
        throw HelperRPCError(code: "internal", message: "AXPress failed: \(r.rawValue)")
    }
}
