# Example: AI replies a WeChat message

This example uses Claude Code as the MCP client. Other MCP-aware tools
(Cursor, etc.) work the same way.

## One-time setup

1. Build and install:
   ```
   ./scripts/install-mac.sh
   ```
2. Grant permissions:
   ```
   qdesk-mac doctor
   ```
   System Settings opens twice (Screen Recording + Accessibility). Add
   `/usr/local/bin/qdesk-mac-helper` to each list and enable it. Run
   `doctor` again to verify.
3. Register with Claude Code:
   ```
   claude mcp add --transport stdio qdesk-mac -- /usr/local/bin/qdesk-mac
   ```

## Replying to a chat

Open WeChat and log in. Then in Claude Code:

> Use qdesk-mac. Find the chat with 张三 in WeChat and send the message
> "晚点到，10分钟". Confirm it was sent.

Claude will typically:

1. Call `wechat.ensure_foreground` to bring WeChat to front.
2. Try `wechat.list_chats`. **On WeChat 4.x for Mac the chat sidebar is
   self-drawn and not exposed to Accessibility, so this returns an empty
   list.** Claude then falls back to the keyboard route (3a) instead of
   the AX route (3b).
3. **Either:**
   - **3a. Keyboard fallback (works on WeChat 4.x):** `wechat.key` with
     `cmd+f` to open the search bar, paste the chat name, press `return`.
     For Chinese chat names, paste via clipboard since `wechat.type` does
     not bypass WeChat's IME for non-ASCII characters.
   - **3b. AX route (works on older WeChat versions where the sidebar
     exposes AXRow):** `wechat.open_chat` with `{"name": "张三"}`.
4. Type the message and send. For Chinese, use a clipboard paste
   (`pbcopy` then `wechat.key cmd+v`); for ASCII you can use `wechat.type`.
5. `wechat.key` with `{"combo": "return"}` (or `cmd+return` if your WeChat
   is configured that way) to send.

A full real run that reached File Transfer Assistant on WeChat 4.x:

```
ensure_foreground
key  cmd+f
[set clipboard to "文件传输助手"]   key cmd+v
key  return
[set clipboard to "hi from qdesk"]  key cmd+v
key  return
```

(Claude can drive the clipboard via shell tools or via a small helper
prompt; that integration is up to the MCP client.)

Use `wechat.screenshot` to re-orient if something looks off — but
Screen Recording must be granted to the helper for screenshots to work.

## Cost

~5–10 tool calls × ~1 screenshot = one Claude API request per call. With
Sonnet, expect $0.01–$0.03 per reply session.

## Limitations (v1)

- Single account only (whichever WeChat is currently logged in).
- Does not auto-launch WeChat — you must start it manually first.
- Screenshots include your full desktop. Don't run on a screen with sensitive
  windows you don't want the model to see.
- **WeChat 4.x main UI is opaque to the Accessibility API.** `list_chats`
  and `open_chat` work only on older versions; on 4.x use the
  cmd+f / clipboard keyboard route shown above.
- **Chinese typing requires the clipboard.** `wechat.type` with Chinese
  text does not register in WeChat's input boxes (the IME pipeline drops
  it). Use `pbcopy` + `wechat.key cmd+v` for any non-ASCII text.
