import Foundation

let writeQueue = DispatchQueue(label: "qdesk.helper.write")

func writeResponse(_ resp: RPCResponse) {
    writeQueue.sync {
        let enc = JSONEncoder()
        enc.outputFormatting = [.withoutEscapingSlashes]
        guard let data = try? enc.encode(resp) else { return }
        FileHandle.standardOutput.write(data)
        FileHandle.standardOutput.write(Data([0x0A]))
    }
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
    case "frontApp":
        writeResult(id: req.id, value: frontApp())
    case "activate":
        do {
            let p = try JSONDecoder().decode(ActivateRequest.self, from: req.params ?? Data("{}".utf8))
            try activate(p)
            writeOK(id: req.id)
        } catch let e as HelperRPCError {
            writeError(id: req.id, code: e.code, message: e.message)
        } catch {
            writeError(id: req.id, code: "internal", message: "\(error)")
        }
    case "screenshot":
        let id = req.id
        Task {
            do {
                let r = try await screenshot()
                writeResult(id: id, value: r)
            } catch let e as HelperRPCError {
                writeError(id: id, code: e.code, message: e.message)
            } catch {
                writeError(id: id, code: "internal", message: "\(error)")
            }
        }
    case "click":
        do {
            struct P: Decodable { let x, y: Double; let button: String; let clicks: Int }
            let p = try JSONDecoder().decode(P.self, from: req.params ?? Data("{}".utf8))
            try clickGlobal(x: p.x, y: p.y, button: p.button, clicks: p.clicks)
            writeOK(id: req.id)
        } catch let e as HelperRPCError {
            writeError(id: req.id, code: e.code, message: e.message)
        } catch {
            writeError(id: req.id, code: "internal", message: "\(error)")
        }
    case "type":
        do {
            struct P: Decodable { let text: String }
            let p = try JSONDecoder().decode(P.self, from: req.params ?? Data("{}".utf8))
            try typeText(p.text)
            writeOK(id: req.id)
        } catch let e as HelperRPCError {
            writeError(id: req.id, code: e.code, message: e.message)
        } catch {
            writeError(id: req.id, code: "internal", message: "\(error)")
        }
    case "key":
        do {
            struct P: Decodable { let combo: String }
            let p = try JSONDecoder().decode(P.self, from: req.params ?? Data("{}".utf8))
            try sendKey(p.combo)
            writeOK(id: req.id)
        } catch let e as HelperRPCError {
            writeError(id: req.id, code: e.code, message: e.message)
        } catch {
            writeError(id: req.id, code: "internal", message: "\(error)")
        }
    case "scroll":
        do {
            struct P: Decodable { let x, y, dx, dy: Double }
            let p = try JSONDecoder().decode(P.self, from: req.params ?? Data("{}".utf8))
            try scroll(x: p.x, y: p.y, dx: p.dx, dy: p.dy)
            writeOK(id: req.id)
        } catch let e as HelperRPCError {
            writeError(id: req.id, code: e.code, message: e.message)
        } catch {
            writeError(id: req.id, code: "internal", message: "\(error)")
        }
    default:
        writeError(id: req.id, code: "internal", message: "method not implemented: \(req.method)")
    }
}

DispatchQueue.global(qos: .userInitiated).async {
    let stdin = FileHandle.standardInput
    let dec = JSONDecoder()
    var buffer = Data()
    while true {
        let chunk = stdin.availableData
        if chunk.isEmpty {
            exit(0)
        }
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
}
RunLoop.main.run()
