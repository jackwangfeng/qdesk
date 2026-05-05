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

> Use qdesk-mac. Send "晚点到，10分钟" to 张三 in WeChat.

Claude will typically:

1. `wechat.ensure_foreground` — bring WeChat to front.
2. `wechat.open_chat` with `{"name": "张三"}` — drives the cmd+f
   search bar; matches whatever WeChat itself surfaces as the top hit.
3. `wechat.screenshot` — verify the right chat opened (the only
   reliable check; WeChat's search may pick the wrong contact for
   ambiguous names).
4. `wechat.click` on the input box, then `wechat.type` "晚点到，10分钟".
   The type call automatically uses the clipboard fallback for
   non-ASCII text.
5. `wechat.key` `{"combo": "return"}` — send.

If the screenshot in step 3 shows the wrong chat, Claude can press
`escape` and retry with a more specific name.

## Cost

~5–10 tool calls × ~1 screenshot = one Claude API request per call. With
Sonnet, expect $0.01–$0.03 per reply session.

## Limitations (v1.1)

- Single account only (whichever WeChat is currently logged in).
- Does not auto-launch WeChat — you must start it manually first.
- Screenshots include your full desktop. Don't run on a screen with
  sensitive windows you don't want the model to see.
- `wechat.open_chat` does not verify which chat actually opened —
  Claude must check with `wechat.screenshot` before sending anything.
- Clipboard fallback for non-ASCII text temporarily replaces your
  pasteboard contents (~150ms) and restores them. Non-string clipboard
  items (images, files) are NOT preserved — they will be replaced by
  empty contents after a non-ASCII type call.
