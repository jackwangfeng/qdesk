# qdesk Mac host-mode (WeChat 控制) 设计文档

**日期**: 2026-05-05
**状态**: 设计阶段（brainstorming 完成，待用户复审）
**作者**: jeff (with Claude)

---

## 1. 一句话定位

给 qdesk 加一个 **Mac host mode**：让 AI agent 通过 MCP 直接控制 *本机* macOS 上的微信，作为现有 Linux Docker 沙箱模式之外的第二条产品线。v1 聚焦"AI-friendly 工具"：generic primitives + 少量 AX 助手，前台必须是 WeChat。

## 2. 为什么做、为什么现在

### 痛点
- 现有 qdesk 是 Linux Docker 沙箱，跑不了原生 macOS app。
- 微信桌面版 90%+ 在 Mac/Windows，没有可用的 Linux native client，网页版功能严重阉割。
- "AI 帮我处理微信"是 Mac 上 prosumer / 个人用户的真实高频诉求（回消息、读群信息、跨群转发等），目前没有 AI-native 工具填这个空。

### 为什么和 qdesk 同仓
- 共用品牌、共用 MCP 工具调用心智模型（用户/AI 都是"qdesk 提供的工具"）。
- 共用 vision-as-primitive 哲学：不依赖 app 内部 instrumentation，靠截屏 + 输入。
- 工具命名 namespace 区隔（`wechat.*`），未来可扩展 `slack.*` `notion-mac.*` 等其它 Mac app。

### 不做什么（位置定义）
- 不做隔离/沙箱化（option 1 已被排除：Mac 不是 Docker，host mode 本质就是开放的）。
- 不做 RPA 平台 / workflow 编排 — 那是上层应用，不是 qdesk 的层次。
- 不做微信 bot 服务 / 常驻监听（use case 2/4，留给 v2）。

## 3. 范围（v1 边界）

| 维度 | v1 决定 |
|---|---|
| 控制目标 | 用户当前登录的 macOS 微信（单实例） |
| 截屏范围 | 全屏（整个桌面） |
| 操作守卫 | 所有 action 类工具内部先确保前台是 WeChat，否则拒绝 |
| 触发方式 | MCP server，stdio transport，被 Claude Code / Cursor / 其它 MCP client spawn |
| 多账号 / 多开 | 不支持 |
| 消息推送 / 监听 | 不支持（轮询由调用方决定） |
| 录屏 / 流式 | 不支持 |
| 签名 / 公证 | v1 不做，README 说明 dev install 方式；v1.x 再做 codesign + notarize |
| 平台 | 仅 macOS 14+（Apple Silicon + Intel 都支持）；Intel 优先级低。`SCScreenshotManager.captureImage` 是 macOS 14 引入的 |
| 微信版本 | 微信 Mac 官方版（非 App Store 阉割版）。AX 助手在大版本变更后可能失效，generic primitives 不受影响 |
| 中英双语 | UI 不依赖语言；AX 助手按 role 找节点不按文本，能跨简中/英文 |

## 4. 架构

### 4.1 进程拓扑

```
┌────────────────────────────────────────────────────────┐
│  MCP client (Claude Code / Cursor)                      │
└──────────────────────────┬─────────────────────────────┘
                           │ stdio / JSON-RPC (MCP)
                           ▼
┌────────────────────────────────────────────────────────┐
│  qdesk-mac (Go binary)                                  │
│   - MCP server (复用现有 qdesk-mcp 的代码骨架)            │
│   - Tool dispatcher + foreground guard                  │
│   - Spawns helper as child, manages lifecycle           │
└──────────────────────────┬─────────────────────────────┘
                           │ stdio / JSON-RPC (内部协议)
                           ▼
┌────────────────────────────────────────────────────────┐
│  qdesk-mac-helper (Swift binary)                        │
│   - ScreenCaptureKit (截屏)                              │
│   - CGEvent (鼠标 / 键盘 / 滚动)                          │
│   - AXUIElement (前台检查 / 聊天列表抓取 / 打开会话)        │
│   - NSWorkspace (前台 app / activate)                    │
└────────────────────────────────────────────────────────┘
```

**为什么分两进程**：Mac native API 全部 Swift/Obj-C 起家，Go 走 cgo 调 Cocoa runtime 又丑又脆；MCP server / 协议处理用 Go 复用最大化（现有 qdesk-mcp 已成熟）。中间用 stdio JSON-RPC，接口面小（~10 个 RPC），可独立测试 helper。

### 4.2 进程生命周期
- `qdesk-mac` 启动时 spawn `qdesk-mac-helper` 作为 child。
- helper 死了：Go 主进程探测到 stdio 关闭，标记 unhealthy，下一次 tool call 返回明确错误，并尝试重启一次。
- `qdesk-mac` 退出（MCP client 断开）：发送 SIGTERM 给 helper，5s grace 后 SIGKILL。

### 4.3 仓库布局
```
qdesk/
├── cmd/
│   ├── qdesk-mac/              # Go: MCP server + helper supervisor
│   └── qdesk-mac-helper/       # Swift package
│       ├── Package.swift
│       └── Sources/Helper/
│           ├── main.swift          # JSON-RPC loop
│           ├── Capture.swift       # ScreenCaptureKit
│           ├── Input.swift         # CGEvent
│           ├── Accessibility.swift # AXUIElement
│           └── Foreground.swift    # NSWorkspace
├── internal/
│   ├── macproto/               # Go: 内部 RPC 类型（与 helper 共享 schema）
│   └── macserver/              # Go: MCP tool 实现 → 转译到 helper RPC
└── docs/superpowers/specs/2026-05-05-mac-host-mode-wechat-design.md
```

### 4.4 内部 RPC（Go ↔ Swift helper）

最小协议，每个请求一行 JSON，响应一行 JSON。

| Method | Params | Response |
|---|---|---|
| `health` | — | `{ok: bool, screenRecordingGranted, accessibilityGranted}` |
| `frontApp` | — | `{bundleId, name, pid}` |
| `activate` | `{bundleId}` | `{ok}` |
| `screenshot` | `{format: "png"}` | `{pngBase64, width, height, scaleFactor}` （width/height 是 logical points，PNG 实际像素 = width × scaleFactor） |
| `click` | `{x, y, button, clicks}` | `{ok}` |
| `type` | `{text}` | `{ok}` |
| `key` | `{combo}` 比如 `"cmd+v"`、`"return"` | `{ok}` |
| `scroll` | `{x, y, dx, dy}` | `{ok}` |
| `axTree` | `{bundleId, query}` query 是简单 selector（如 `role=AXOutline`） | `{nodes: [...]}` |
| `axClick` | `{bundleId, path}` | `{ok}` |

`axTree` / `axClick` 是 AX 助手的底层；`wechat.list_chats` 在 Go 侧把 axTree 结果整理成 chat list。

### 4.5 v1 MCP 工具

全部在 `wechat.` 命名空间下。

**通用原语**
- `wechat.screenshot()` → `{image: png, frontApp: {bundleId, name}}`
  截全屏，附带当前前台 app（让 LLM 自己判断是否需要 `ensure_foreground`）
- `wechat.click({x, y, button?, clicks?})` — 全局屏幕坐标，先 ensure_foreground
- `wechat.type({text})` — UTF-8 / 中文走 Unicode 模式，先 ensure_foreground
- `wechat.key({combo})` — `enter` / `escape` / `cmd+a` / `cmd+v` 等
- `wechat.scroll({x, y, dy})`
- `wechat.ensure_foreground()` — 把 WeChat 激活到前台；微信没运行时报清楚错（不做"自动启动 app"，避免误启动）

**AX 助手**
- `wechat.list_chats()` → `[{name, unread_count, last_msg_preview}]`
  从 WeChat 主窗口左侧 sidebar 的 AXOutline / AXTable 抓
- `wechat.open_chat({name})` — 模糊匹配 sidebar 中 chat name；找不到则 fallback 用 `cmd+f` 唤起搜索框 + 输入 + Enter

**foreground guard**：除 `screenshot` / `ensure_foreground` 之外的所有工具，调用前都先 check 前台 = WeChat（Bundle ID `com.tencent.xinWeChat`），不是的话返回 `{error: "wechat-not-foreground", hint: "call wechat.ensure_foreground first"}`。

### 4.6 数据流（典型："给张三发消息"）

```
LLM
 │  wechat.list_chats()
 ▼
qdesk-mac (Go) ──► helper ──► AXUIElement(WeChat sidebar) ──► return list
 │
 │  wechat.open_chat({name: "张三"})
 ▼
qdesk-mac ──► helper ──► AXPress on matched cell
 │
 │  wechat.click({x, y})  # 输入框
 ▼
qdesk-mac ──► foreground guard ✓ ──► helper ──► CGEvent click
 │
 │  wechat.type({text: "晚点到，10分钟"})
 ▼
qdesk-mac ──► helper ──► CGEvent unicode type
 │
 │  wechat.key({combo: "return"})
 ▼
qdesk-mac ──► helper ──► CGEvent return
```

LLM 大部分判断（在哪点、字打哪、是不是打成了）靠 `screenshot` 看图。AX 助手只省 vision token 的几个高频痛点。

## 5. 权限 / 首次启动

### 5.1 需要的 macOS TCC 权限
- **Screen Recording** (`kTCCServiceScreenCapture`) — `ScreenCaptureKit` 需要
- **Accessibility** (`kTCCServiceAccessibility` / `kTCCServicePostEvent`) — `CGEvent` post + `AXUIElement` 都需要

### 5.2 谁拿权限
两个权限授给 **Swift helper 二进制**（不是 Go 主进程）—— 实际调 native API 的是它，TCC 跟踪的是这个二进制的代码签名。

### 5.3 安装 + 首次启动流程

1. `brew install qdesk-mac`（v1 之后；v1 提供 install.sh）→ 把 `qdesk-mac` 和 `qdesk-mac-helper` 放到 `/usr/local/bin/`。**helper 路径必须稳定**，否则 TCC 重新 prompt。
2. 用户跑 `qdesk-mac doctor`：
   - helper 检查 `CGPreflightScreenCaptureAccess()` + `AXIsProcessTrusted()`
   - 缺哪个就打开对应 System Settings 面板（`x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture` / `?Privacy_Accessibility`）
   - 打印一条提示："请把 `/usr/local/bin/qdesk-mac-helper` 添加到列表并启用"
3. 用户配置 MCP client（Claude Code 的 `mcp.json` 或 `claude mcp add`）指向 `qdesk-mac`。
4. 第一次 LLM 调任何 tool，Go 侧调 `health` RPC，权限缺失就在 MCP 响应里返回明确错误 + `doctor` 命令提示，不让 LLM 瞎尝试。

### 5.4 v1 不签名的代价
未签名 / 未公证 binary 的 TCC entry 会绑定到具体哈希；rebuild 后 TCC 把它当新 binary，需要重新 grant。开发期可接受；发布前必须解决（codesign + notarize + stable Developer ID）。

## 6. 错误处理

LLM-friendly：错误一律返回结构化对象，用稳定的 `code` 字段，`hint` 字段告诉 LLM 怎么自救。

| code | 何时 | hint |
|---|---|---|
| `wechat-not-running` | foreground guard 检测到 WeChat 没在跑 | "Start WeChat manually first; qdesk-mac does not auto-launch apps." |
| `wechat-not-foreground` | 前台是别的 app | "Call wechat.ensure_foreground first." |
| `permission-screen-recording` | helper 报 SR 缺权限 | "Run `qdesk-mac doctor` to fix." |
| `permission-accessibility` | helper 报 AX 缺权限 | 同上 |
| `helper-crashed` | helper 进程死掉 | "Helper restarted; retry the call." |
| `chat-not-found` | `open_chat` 模糊匹配失败 | "Check `wechat.list_chats()` for current names; or pass exact name." |
| `ax-tree-empty` | sidebar AX 树空（WeChat 没主窗口） | "Open WeChat main window first (cmd+1)." |

## 7. 测试策略

### 7.1 helper 单元测试（Swift）
- `Capture` 测：跑一次截屏，检查 PNG header + 尺寸不为 0。
- `Input` 测：post 一个 click 到坐标 (0,0)（屏幕角落），不期望具体效果，只验 API 不报错。
- `Accessibility` 测：枚举 Finder 主窗口（任何 Mac 上都有的稳定 fixture），验返回 AX 树非空。
- 不测真实 WeChat（CI 上没装）。

### 7.2 helper RPC 集成测试（Go）
- spawn 真实 helper，跑 `health` / `frontApp` / `screenshot` 这种"任何 Mac 上都能跑"的 RPC。
- WeChat-相关的 RPC 在本地手测，不进 CI。

### 7.3 端到端冒烟（手动）
- README 给一个 `examples/wechat-reply.md`：用户用 Claude Code 调 qdesk-mac，让 AI 给某个置顶聊天发"hi"。手动验证消息真的发出去了。

### 7.4 CI 限制
- helper 的 Swift 测试只能在 macOS runner 上跑。GitHub Actions 用 `macos-latest`。
- TCC 权限在 CI 里没法 grant（需要交互 GUI），所以 CI 只跑 `health` 这种探测类的 RPC，能确认 binary 起得来、协议没坏即可。

## 8. 安全 / 风险

- **没有沙箱**：Mac host mode 本质就是把控制权给 LLM。foreground guard 是软约束，AI 只要让 WeChat 在前台就能点任何全屏坐标 — 比如 Cmd+Space 唤起 Spotlight 然后转去做别的（理论上）。这是 option 2 的明确取舍。文档里要 disclaimer 清楚：**生产/敏感账号用沙箱模式，不要用 host mode**。
- **截屏会包含桌面其它内容**：第三方 IDE / 邮件 / 浏览器内容都会进截图发给 LLM。文档明确 privacy 提醒；未来 v2 可加"只截 WeChat 窗口"模式（等真用上再加）。
- **误操作**：AI 算坐标错、点错聊天发错人。靠 LLM 自己 + 用户 review 兜底。可以加一个 `wechat.send_dry_run` mode（只准备好但不按 enter）—— 但 v1 YAGNI 不做，看是否真有用户反馈再加。

## 9. 不在 v1 范围

明确推迟 / 留给 v2+：

- 多账号 / 多开微信
- 消息推送 / 守护进程模式（"AI 自动回消息"）
- WeChat-语义工具（`wechat.send_text(to, text)` 这种"一行发完"的封装）
- 单窗口截屏隔离（option 1）
- 签名 + 公证 + Sparkle 自动更新
- 其它 Mac app（Slack / 飞书 / Notion）
- WeChat 改版后的 AX 修复自动化

## 10. 成功标准

v1 算"完成"，要满足以下全部：

1. macOS（Apple Silicon）上 `brew/curl install` 后 + `doctor` grant 权限后，Claude Code 能通过 MCP 调 `wechat.screenshot()` 拿到带 frontApp 信息的 PNG。
2. 在打开微信、登录账号的前提下，Claude Code 能自然语言指令"给已置顶的某个聊天发 hi"，全程通过 MCP 工具走通：list_chats → open_chat → click 输入框 → type → return。
3. 所有 `cmd/qdesk-mac` 单元测试 + helper Swift 测试 + GitHub Actions macOS CI 全绿。
4. README + `examples/wechat-reply.md` 让一个新用户能在 ≤10 分钟内把上面 #2 跑通（含权限授予）。

## 11. 公开问题（已识别但 v1 不解决）

- **WeChat 大版本升级 → AX 树结构变化**：v1 接受这个脆弱性，问题出现时手动修。v2 考虑 AX 助手做 schema 校验 + 自动 fallback 到 vision。
- **多 monitor / Retina 缩放下的坐标**：截屏返回的 width/height 是 logical 还是 physical？决定 LLM 算坐标用哪套。v1 默认返回 logical points + scaleFactor，LLM 直接用 logical 坐标 click（CGEvent 用 logical），不用换算。多显示器时仅支持主屏幕，v2 再扩。
- **中文输入法状态干扰**：`type` 用 CGEvent unicode 模式应该绕开 IME，但 WeChat 输入框可能有自己的 IME hook。v1 实测优先，跑不通再考虑用剪贴板 + paste 兜底。
