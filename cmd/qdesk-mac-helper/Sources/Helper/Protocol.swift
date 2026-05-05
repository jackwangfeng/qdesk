import Foundation

struct RPCRequest: Decodable {
    let id: Int
    let method: String
    let params: Data?

    enum CodingKeys: String, CodingKey { case id, method, params }

    init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(Int.self, forKey: .id)
        method = try c.decode(String.self, forKey: .method)
        if c.contains(.params) {
            // Re-encode the raw value so handlers can decode their own params type.
            let raw = try c.decode(JSONValue.self, forKey: .params)
            params = try JSONEncoder().encode(raw)
        } else {
            params = nil
        }
    }
}

struct RPCResponse: Encodable {
    let id: Int
    let result: JSONValue?
    let error: RPCError?
}

struct RPCError: Encodable {
    let code: String
    let message: String
}

// Minimal JSON value used to round-trip arbitrary param payloads.
enum JSONValue: Codable {
    case null
    case bool(Bool)
    case number(Double)
    case string(String)
    case array([JSONValue])
    case object([String: JSONValue])

    init(from decoder: Decoder) throws {
        let c = try decoder.singleValueContainer()
        if c.decodeNil() { self = .null; return }
        if let v = try? c.decode(Bool.self) { self = .bool(v); return }
        if let v = try? c.decode(Double.self) { self = .number(v); return }
        if let v = try? c.decode(String.self) { self = .string(v); return }
        if let v = try? c.decode([JSONValue].self) { self = .array(v); return }
        if let v = try? c.decode([String: JSONValue].self) { self = .object(v); return }
        throw DecodingError.dataCorruptedError(in: c, debugDescription: "unknown JSON value")
    }

    func encode(to encoder: Encoder) throws {
        var c = encoder.singleValueContainer()
        switch self {
        case .null: try c.encodeNil()
        case .bool(let v): try c.encode(v)
        case .number(let v): try c.encode(v)
        case .string(let v): try c.encode(v)
        case .array(let v): try c.encode(v)
        case .object(let v): try c.encode(v)
        }
    }
}
