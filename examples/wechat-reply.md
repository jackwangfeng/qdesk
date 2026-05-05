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
2. Call `wechat.list_chats` to see available chats (no vision needed).
3. Call `wechat.open_chat` with `{"name": "张三"}`.
4. Call `wechat.screenshot` to verify the chat opened and find the input box.
5. Call `wechat.click` on the input box, then `wechat.type` with the message.
6. Call `wechat.key` with `{"combo": "return"}` to send.

If something goes wrong (unread badge, wrong window), Claude can call
`wechat.screenshot` again to re-orient.

## Cost

~5–10 tool calls × ~1 screenshot = one Claude API request per call. With
Sonnet, expect $0.01–$0.03 per reply session.

## Limitations (v1)

- Single account only (whichever WeChat is currently logged in).
- Does not auto-launch WeChat — you must start it manually first.
- Screenshots include your full desktop. Don't run on a screen with sensitive
  windows you don't want the model to see.
