import Foundation

func writeResponse(_ resp: RPCResponse) {
    let enc = JSONEncoder()
    enc.outputFormatting = [.withoutEscapingSlashes]
    guard let data = try? enc.encode(resp) else { return }
    FileHandle.standardOutput.write(data)
    FileHandle.standardOutput.write(Data([0x0A])) // newline
}

func writeError(id: Int, code: String, message: String) {
    writeResponse(RPCResponse(id: id, result: nil, error: RPCError(code: code, message: message)))
}

func writeOK(id: Int) {
    writeResponse(RPCResponse(id: id, result: .object(["ok": .bool(true)]), error: nil))
}

func writeResult<T: Encodable>(id: Int, value: T) {
    let enc = JSONEncoder()
    guard let data = try? enc.encode(value),
          let obj = try? JSONDecoder().decode(JSONValue.self, from: data) else {
        writeError(id: id, code: "internal", message: "encode result")
        return
    }
    writeResponse(RPCResponse(id: id, result: obj, error: nil))
}

func dispatch(_ req: RPCRequest) {
    switch req.method {
    case "health":
        writeResult(id: req.id, value: health())
    default:
        writeError(id: req.id, code: "internal", message: "method not implemented: \(req.method)")
    }
}

// stdin loop: one JSON object per line.
let stdin = FileHandle.standardInput
let dec = JSONDecoder()
var buffer = Data()
while true {
    let chunk = stdin.availableData
    if chunk.isEmpty { break }
    buffer.append(chunk)
    while let nl = buffer.firstIndex(of: 0x0A) {
        let line = buffer.subdata(in: 0..<nl)
        buffer.removeSubrange(0...nl)
        if line.isEmpty { continue }
        do {
            let req = try dec.decode(RPCRequest.self, from: line)
            dispatch(req)
        } catch {
            FileHandle.standardError.write(Data("decode error: \(error)\n".utf8))
        }
    }
}
