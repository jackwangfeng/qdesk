# qdesk Mac host-mode v1.1 — clipboard-based chat tools

**日期**: 2026-05-06
**状态**: 设计阶段（v1 已合并；本 spec 启动 v1.1 迭代）
**作者**: jeff (with Claude)
**前置**: `2026-05-05-mac-host-mode-wechat-design.md` (v1 spec)
**触发**: v1 E2E 在 macOS WeChat 4.x build 4066646301 实测，三个 v1 假设被打破

---

## 1. 问题陈述

v1 设计假设两条路在 WeChat Mac 上都通：

- **AX 路**（`wechat.list_chats` / `wechat.open_chat`）—— 通过 AXUIElement 抓 sidebar 的 AXRow 列表。
- **CGEvent unicode 路**（`wechat.type`）—— 直接发 unicode 字符到当前输入框。

实测两条都断了：

1. **WeChat 4.x 主窗口对 AX 不透明**：完整 AX 树只见 1 AXApplication + 2 AXWindow + 3 AXButton + 2 AXGroup + 171 AXMenuItem（菜单栏）。sidebar、消息区、输入框全部自绘，AX 完全看不到。`list_chats` 返回空，`open_chat` 永远 chat-not-found。
2. **CGEvent unicode 模式中文进不去 WeChat 输入框**：`keyboardSetUnicodeString` 发的中文 UTF-16 在搜索框和聊天输入框都不显示（推测 WeChat 自己的 IME 拦截），ASCII 没问题。
3. **`resolveKeyCombo` 字母键码不全**：v1 实现只有 a/c/v/x/z，cmd+f 直接报错。已在 v1 fix commit `38b754d` 补全 a-z + 0-9。

实测跑通的方案是绕开两条 v1 假设，全程走"键盘 + 剪贴板"：

```
ensure_foreground → key cmd+f → [pbcopy 聊天名] → key cmd+v → key return
                  → [pbcopy 消息] → key cmd+v → key return
```

## 2. v1.1 范围

只动 *设计假设错的部分*，不重做整个架构。

| 改动 | v1 行为 | v1.1 行为 |
|---|---|---|
| `wechat.type` 输入中文 | 走 CGEvent unicode，进不去 | 内部检测含非 ASCII → 走剪贴板 + cmd+v |
| `wechat.open_chat` | 抓 AXRow，4.x 永远找不到 | 走 cmd+f + 剪贴板粘贴 + return |
| `wechat.list_chats` | 抓 AXRow，4.x 永远空 | **删除** —— 没有可靠的非 vision 替代；让 LLM 用 screenshot 自己看 |
| 剪贴板污染 | 不涉及 | 必须 save/restore，不能让用户的剪贴板被默默改 |
| 内部 RPC | `axTree`/`axClick` 仍存在但 4.x 上无用 | 保留（老版本/其它 Mac app 还有用），但 chat tools 不再依赖 |

不在范围：
- 改架构（Go + Swift sidecar 拓扑不变）
- 加新 MCP 工具
- 解决 WeChat 5.x（如果出了）的 AX 行为变化
- 处理 WeChat 输入设置（"回车发送" vs "cmd+回车发送"）—— 让 LLM 看截图自己判断

## 3. 设计

### 3.1 `wechat.type` 中文 → 剪贴板路径

伪代码：

```go
func (s *MCPServer) toolType(ctx, args) {
    text := args.text
    requireWeChatForeground()
    if isASCII(text) {
        return helper.Call("type", {text})  // v1 path, fast for ASCII
    }
    // Non-ASCII: clipboard route
    return s.helper.Call("clipboardPaste", {text})  // 新 helper RPC
}
```

新 helper RPC `clipboardPaste`（在 Swift helper 里）：
1. 读当前 NSPasteboard.changeCount + 内容（备份）
2. setString(text) 到 NSPasteboard
3. post CGEvent cmd+v
4. 等 ~150ms（让 paste 生效，否则 restore 立刻覆盖会让 cmd+v 拿到错的）
5. 把备份内容写回剪贴板（restore）

为什么把整个 paste-restore 包进 helper：
- 时序敏感（restore 太早会覆盖 paste 中间状态），Go 侧通过 JSON-RPC 控制不可靠
- 所有 NSPasteboard 操作集中在 Swift 一处

边界：
- 如果备份内容太大（比如几 MB 文件），仍 restore；用户的 expense 是必要的
- 如果备份是非文本 pasteboard items（图片、文件），暂不 restore（v1.1 范围内只 restore 字符串）；记录在文档里
- 非 ASCII 检测用简单的 `strings.ContainsFunc(text, func(r rune) bool { return r > 0x7F })`

### 3.2 `wechat.open_chat` → cmd+f 路径

伪代码：

```go
func (s *MCPServer) toolOpenChat(ctx, args) {
    name := args.name
    requireWeChatForeground()
    helper.Call("key", {combo: "cmd+f"})
    sleep(300ms)  // 让搜索框出现 + 拿到焦点
    if isASCII(name) {
        helper.Call("type", {text: name})
    } else {
        helper.Call("clipboardPaste", {text: name})
    }
    sleep(200ms)  // 让搜索框过滤到第一条
    helper.Call("key", {combo: "return"})
    return ok("opened chat matching %q (verify with screenshot if needed)", name)
}
```

- 不再尝试 axTree
- 不再 fuzzy-match —— 直接交给 WeChat 的搜索匹配
- 不能保证打开的就是用户期望的那个聊天（可能匹配到别的）—— LLM 必须用 screenshot 验证
- 如果搜索没结果，cmd+f 关闭后回到主界面 —— 表现"无害"

### 3.3 `wechat.list_chats` 删除

理由：
- 4.x 上 axTree 抓不到任何 row
- 老版本上的实现保留在 git history，但不发布 MCP 工具描述
- 让 LLM 调 `wechat.screenshot` + vision 自己读 sidebar；这就是 qdesk 的 default vision-as-primitive 路线

`internal/macserver/chats.go` 里的 `fetchChats` / `matchChat` / `ChatRow` 类型保留（`open_chat` 重写时不再用，可以删 —— 但占 50 行，留着也无害）—— v1.1 决策：**删掉，避免误导未来维护者**。

### 3.4 sleep 时序

v1.1 引入两处 sleep：
- `cmd+f` 后 300ms
- 输入完搜索词后 200ms

这些是 UI 响应延迟，不能去掉。在 Go 侧用 `time.Sleep`，不放进 helper（保持 helper 是纯无状态 RPC server）。

### 3.5 实测可能出现的问题（这次先识别）

- **回车发送 vs cmd+回车发送**：v1.1 不在 `open_chat` 内部按 enter 发消息（open_chat 只打开聊天，不输入消息）。消息是 LLM 后续自己决定怎么发。所以这个问题留给 prompt 层。
- **WeChat 启动慢，cmd+f 没立刻响应**：sleep 300ms 是经验值；如果不够 LLM 调用 screenshot 看 + retry 即可，不在 v1.1 范围内做更复杂的等待逻辑。

## 4. 测试

### 4.1 单测（mock helper）

- `TestTypeFallsBackToClipboardForChinese` —— `wechat.type` 收到中文，应调用 `clipboardPaste` 而不是 `type`
- `TestTypeUsesDirectTypeForASCII` —— `wechat.type` 收到 ASCII，应调用 `type`
- `TestOpenChatSequencesCmdFAndPasteAndReturn` —— `wechat.open_chat` 应顺序触发 key cmd+f → clipboardPaste/type → key return

### 4.2 helper 单测（Swift）

- `ClipboardPasteRoundTrip` —— 备份当前 pasteboard → setString → cmd+v(only verify CGEvent created, not actual paste effect since no app focus in test) → restore → 验证 pasteboard 内容回到备份

### 4.3 E2E（手动）

- 在 WeChat 4.x 上：`wechat.open_chat` "文件传输助手" → 应该跳到该聊天
- `wechat.type` "你好" → 输入框出现"你好"
- 跑完整 send 流程：open_chat → 移动焦点到输入框（截图验证）→ type 中文 → key return

## 5. 实施

后续 plan：`docs/superpowers/plans/2026-05-06-mac-host-mode-v1.1-plan.md`（独立 task list，按本 spec 实现）。

预计 task 数：
- 1: 加 helper RPC `clipboardPaste`（Swift NSPasteboard + CGEvent cmd+v）
- 2: 加 macproto `MethodClipboardPaste` 类型 + 测试
- 3: 改 Go `toolType` 加 ASCII 检测分支
- 4: 改 Go `toolOpenChat` 走 cmd+f 路径
- 5: 删 Go `toolListChats` + `chats.go` 内的死代码 + tools/list 里的描述
- 6: 改 example 文档 + README
- 7: E2E 复跑（在真 WeChat 上）

约 6 个 commit，1 day 工作量。

## 6. 不解决的问题（v1.2+）

- WeChat 升级时 cmd+f 行为可能变（少见但要监控）
- 没有"批量发消息给多人"等高级 use case
- 截图+vision 路径的 token 成本仍然高（这是 qdesk 整体定位的特征，不是 bug）
- 用户 IME 在中文输入法下 → cmd+v 可能被 IME 拦截（暂未实测）
