# qdesk Windows host-mode 设计文档

**日期**: 2026-05-07
**状态**: 设计阶段（brainstorming 完成，待用户复审）
**作者**: jeff (with Claude)

---

## 1. 一句话定位

给 qdesk 加 **Windows host mode**：让 AI agent 通过 MCP 直接控制一台联网的 Windows 机器（用户自己的 Windows 工作站、家里的台式机、或一台 always-on 的 Windows VM），作为现有 Linux Docker 沙箱模式 + Mac host mode 之外的第三条产品线。形态镜像 Mac host mode（host 模式，单租户），namespace 用 `windows.*`，对称 `mac.*`。

## 2. 为什么做、为什么现在

- qdesk 现有 Linux Docker 沙箱跑不了 Windows-only 应用（Office、企微、各类国内桌面 client）。
- Mac host mode 的产品形态（MCP-first、远端 / stdio 双通道、generic primitives + 少量 vision）已经验证可用，Windows 没理由不复用。
- Mac mode 在 Windows 用户群里反复被问"什么时候有 Windows 版"。jeff 自己开发机是 Mac，但有不少潜在用户是 Windows 主力，加上 jeff 现在搭了 VM 当稳定测试目标，时机合适。

### 不做什么
- 不做 Windows 沙箱多租户（option 已排除：那条路成本高、license 复杂，要做就走 Linux Docker 那条线，不在 host mode 里搞）。
- 不做 RPA 平台 / workflow 编排。
- 不做服务化安装 / 自启 / 代码签名（v1 dev install，README 说清楚）。

## 3. 范围（v1 边界）

| 维度 | v1 决定 |
|---|---|
| 控制目标 | 一台联网 Windows 机器（用户的 PC 或 VM） |
| 部署形态 | 单 Go 二进制 `qdesk-win.exe`，无 sidecar |
| 传输 | 仅 HTTP listen + Bearer API key（同 `qdesk-mac --listen`）；stdio 模式 v1 不上 |
| 截屏范围 | 整个主显示器（全屏 PNG） |
| 多显示器 | v1 仅主屏 |
| 操作守卫 | 除 `screenshot` / `activate` / `front_app` 外，每个 action 接 optional `expected_exe`，前台进程不匹配则拒绝（per-call guard，对照 `mac.*`） |
| 多账号 / 多开 | 不支持 |
| UIA / AX 树 | v1 不上（Win32 COM 从 Go 调成本高，且 Electron / DirectX 自绘应用 UIA 也看不到，参考 Mac 在 WeChat 4.x 的结论） |
| 平台 | Windows 10 1809+ / Windows Server 2019+；x64 优先，arm64 v1 不构建 |
| 服务化 / 签名 | v1 不做 |

## 4. 架构

### 4.1 进程拓扑

```
┌─ Mac (用户机) ───────────────────────┐
│ MCP client / Claude Code             │
│      │ HTTP + Bearer                 │
│      ▼                               │
└─── http://win-host:8765/mcp ─────────┘
                                          
┌─ Windows host ──────────────────────┐
│ qdesk-win.exe (single Go binary)     │
│  ├─ MCP HTTP listener (JSON-RPC)     │
│  └─ syscalls → user32 / gdi32 /      │
│       kernel32 / shcore              │
│       (SendInput, BitBlt,            │
│        OpenClipboard,                │
│        GetForegroundWindow,          │
│        SetProcessDpiAwarenessContext)│
└──────────────────────────────────────┘
```

### 4.2 关键差异 vs Mac host mode

| 维度 | Mac mode | Windows mode |
|---|---|---|
| 进程数 | Go server + Swift sidecar (helper) | 单 Go binary |
| Native API 访问 | 必须 sidecar — Cocoa / SCK / CGEvent / AX 不能从 Go 调 | Win32 是 C-style，Go `golang.org/x/sys/windows` 直接 syscall |
| Transport | stdio + HTTP listen | 仅 HTTP listen（v1） |
| 主要部署位置 | 用户自己的 Mac（本地 stdio） | 远端 Windows，从用户 Mac 跨网络访问 |

### 4.3 代码布局

```
cmd/qdesk-win/
  main.go            # flag 解析、HTTP 启动
internal/winserver/
  mcp.go             # tools/list, tools/call 派发
  tools.go           # windows.* tool 实现的薄壳（参数解析）
  guard.go           # per-call expected_exe 守卫
internal/winnative/
  dpi.go             # SetProcessDpiAwarenessContext on init
  capture.go         # GetDesktopWindow + BitBlt → PNG
  input.go           # SendInput (mouse/keyboard/wheel)
  clipboard.go       # OpenClipboard / SetClipboardData / 备份恢复
  foreground.go      # GetForegroundWindow + EnumWindows + SetForegroundWindow
  keymap.go          # "ctrl+f" / "win+r" 等组合键 → VK 码序列
```

`internal/winserver` 与 `internal/macserver` 同构（MCP 派发逻辑应该考虑后续抽公共，但 v1 先复制结构，避免过早抽象）。

## 5. 工具面（`windows.*`）

镜像 `mac.*`：

| Tool | 输入 | 输出 | 实现 |
|---|---|---|---|
| `windows.front_app` | — | `{hwnd, pid, exe, title}` | `GetForegroundWindow` + `GetWindowThreadProcessId` + `QueryFullProcessImageNameW` + `GetWindowTextW` |
| `windows.activate` | `{exe?, hwnd?, title_regex?}`（至少给一个；优先级 `hwnd > exe > title_regex`；`exe` 多匹配取 `EnumWindows` 顺序中首个 `IsWindowVisible == true`） | `{hwnd, actually_foreground}` | `EnumWindows` 找匹配 → `ShowWindow(SW_RESTORE)` + `SetForegroundWindow`（含 AttachThreadInput 焦点偷渡）；`actually_foreground` 由调用后的 `GetForegroundWindow` 决定 |
| `windows.screenshot` | — | `{png_base64, width, height}` | `GetDC(NULL)` + `BitBlt` + PNG encode |
| `windows.click` | `{x, y, button=left, double=false, modifiers=[], expected_exe?}` | `{}` | `SetCursorPos` + `SendInput(MOUSEINPUT)` |
| `windows.type` | `{text, expected_exe?}` | `{path: "unicode"\|"clipboard", clipboard_restored?: bool}` | ASCII → `SendInput KEYEVENTF_UNICODE`；非 ASCII → clipboard + Ctrl+V，跑完恢复 clipboard |
| `windows.key` | `{combo: "ctrl+f"\|"win+r"\|..., expected_exe?}` | `{}` | `keymap` 解析 → `SendInput(KEYBDINPUT)` 序列 |
| `windows.scroll` | `{x, y, dx, dy, expected_exe?}` | `{}` | `SendInput(WHEELINPUT)`，正负代表方向 |
| `windows.clipboard_paste` | `{text, expected_exe?}` | `{clipboard_restored?: bool}` | 显式 set clipboard + 发 Ctrl+V，再恢复原 clipboard |

除 `front_app` / `screenshot` / `activate` 外，每个 action 接 optional `expected_exe`：foreground 进程的 basename（不区分大小写）不等于 `expected_exe` 时 tool 返回结构化 error，不动手。`screenshot` 不带 guard（捕屏读屏永远安全）；`activate` 本身就是要切换前台，guard 没有意义。

## 6. 已知踩坑 & 工程要点

1. **`SetForegroundWindow` 限制**：Windows 只允许"拥有前台 input"的进程偷焦点。`windows.activate` 实现里要做 AttachThreadInput + 模拟一次 `keybd_event(VK_MENU, 0)` 拿 input 资格再 `SetForegroundWindow`，仍然偶发不前台。tool 出参带 `actually_foreground: bool`，调用方有责任拒绝继续；不假装成功。

2. **DPI awareness**：`main.go` 启动时 `SetProcessDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2)`。否则在缩放 != 100% 的机器上截图坐标和 SetCursorPos / SendInput 坐标不一致。

3. **`SendInput KEYEVENTF_UNICODE` 失败兜底**：跟 Mac 上 CGEvent unicode 在 WeChat 失效一样，部分老 Win32 控件吞 Unicode 事件。`windows.type` 内部判 ASCII vs 非 ASCII，非 ASCII 走 clipboard 路径，并用 `GetClipboardSequenceNumber` 备份原内容、动作完成恢复。备份/恢复任一步失败：动作仍执行，结果里 `clipboard_restored: false` 并记 warning，不报错（用户预期"我让你 type 一段东西，剪贴板被改是次要损失"）。

4. **多线程 / clipboard 串扰**：`OpenClipboard` 是全局锁，并发请求会冲突。MCP server 单进程内对 clipboard 操作加 mutex 串行化。

5. **PNG 编码**：用 stdlib `image/png`；BitBlt 出来的 BGRA 转 RGBA 自己写一遍。

6. **Bearer 认证**：与 `qdesk-mac --listen` 完全同一套（参考 `cmd/qdesk-mac/http.go`）。`--api-key` flag 或 `QDESK_WIN_API_KEY` env，缺一启动失败。

## 7. v1 显式 defer

| 项 | 留到 |
|---|---|
| UIA / accessibility 树 | v2（且大概率会得出"对自绘应用没用"的结论） |
| `windows.list_windows` | v2（v1 让 LLM 用 screenshot + activate 试出来） |
| 每窗口 screenshot（`PrintWindow PW_RENDERFULLCONTENT`） | v2 |
| 多显示器 | v2 |
| stdio over SSH 传输 | v2（如果有用户要求） |
| 服务化（nssm / sc create）、auto-start | 文档给方案，不在二进制里 |
| 代码签名 | v1 后续 |
| ARM64 构建 | 等需求 |

## 8. 测试

### 8.1 单测
- `internal/winnative/keymap_test.go`：组合键解析。
- `internal/winserver/tools_test.go`：参数解析、`expected_exe` guard 行为；syscall 层 mock（用接口注入 fake native）。

### 8.2 E2E
靶机：jeff 已搭好的 Windows Server 2022 VM `Administrator@192.168.0.127`（OpenSSH on，PowerShell 5.1）。

冒烟序列（用 curl 直打 MCP）：
1. `windows.front_app` → 记录起始前台
2. 在 VM 上 `start notepad`
3. `windows.activate {exe: "notepad.exe"}` → 验证 `actually_foreground=true`
4. `windows.type {text: "hello qdesk", expected_exe: "notepad.exe"}` → 走 unicode 路径
5. `windows.type {text: "你好 qdesk", expected_exe: "notepad.exe"}` → 自动走 clipboard 路径
6. `windows.screenshot` → PNG 大小合理、内容含上面两行
7. `windows.key {combo: "ctrl+a"}` → `windows.key {combo: "delete"}` → 截图清空

部署方式：`scp qdesk-win.exe Administrator@192.168.0.127:` + ssh 启动 + 防火墙开 8765（dev only，生产场景走反向代理 / Tailscale，跟 mac mode 文档对齐）。

## 9. Open questions（不阻塞 v1 实现）

- 生产部署：scp + 手动启动 OK 吗？还是要 winsw / nssm 脚本一起出？倾向后者放 v1.1。
- API key 是不是该跟 mac mode 共享一个 env 名前缀，比如 `QDESK_HOST_API_KEY`？跟 mac mode 当前 `QDESK_MAC_API_KEY` 对称，v1 暂用 `QDESK_WIN_API_KEY`，后续可改。
- 滚轮 `dx` 是否真的有意义（Win32 horizontal wheel 大量 app 不响应）？v1 接收但实测兜不住时文档说明。
