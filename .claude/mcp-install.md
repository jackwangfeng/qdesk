# Installing qdesk as an MCP server in Claude Code

After building qdesk, register it once:

```bash
claude mcp add --transport stdio qdesk -- /usr/local/bin/qdesk-mcp \
    --control http://127.0.0.1:8090 \
    --api-key "$QDESK_DEV_KEY" \
    --gemini-key "$GEMINI_API_KEY"
```

Or, project-scoped (recommended for team repos), add to your project's
`.mcp.json` so anyone cloning the repo gets it:

```json
{
  "mcpServers": {
    "qdesk": {
      "command": "qdesk-mcp",
      "args": [
        "--control", "http://127.0.0.1:8090",
        "--llm",     "gemini-2.5-flash"
      ],
      "env": {
        "QDESK_DEV_KEY":   "${QDESK_DEV_KEY}",
        "GEMINI_API_KEY":  "${GEMINI_API_KEY}"
      }
    }
  }
}
```

## Tools exposed

| Tool | When to use |
|---|---|
| `qdesk_health` | First check; confirm control plane is reachable |
| `qdesk_screenshot` | "Show me what this URL looks like" — returns a PNG Claude can see |
| `qdesk_quick_test` | "Verify clicking X does Y" — inline goal + expects, no YAML needed |
| `qdesk_run_test` | "Re-run this committed test" — by absolute YAML path |

## Prerequisites the user must provide once

```bash
# 1. Build sandbox image
docker build -t qdesk/ubuntu-chrome:dev -f images/ubuntu-chrome/Dockerfile .

# 2. Start control plane (long-running, leave in a terminal)
export QDESK_DEV_KEY=$(openssl rand -hex 16)
qdesk-control --listen 127.0.0.1:8090 --dev-key "$QDESK_DEV_KEY" \
              --image qdesk/ubuntu-chrome:dev

# 3. Set Gemini key in shell (or in Claude Code's MCP env block)
export GEMINI_API_KEY=AIza...
```

## Typical Claude Code session

```
User: I just added a new "Forgot password?" link on the login page. Make sure
      it actually navigates to the password-reset screen.

Claude:  [calls qdesk_quick_test with:
           url=http://host.docker.internal:8888/login,
           goal="click the 'Forgot password?' link",
           expect=["URL or page indicates we're on the password-reset screen",
                   "An email input is visible"]]

         [Claude sees the test result + screenshot]

         ✅ Verified — clicking the link navigates correctly to /reset and
         shows an email input. Report: file:///tmp/.../report.html
```

## What if it fails?

The MCP tool returns `isError: true` plus the AI-generated diagnosis text
and the final screenshot. Claude can then either:
- Fix the underlying bug (most common) and re-run.
- Adjust the test description if it was ambiguous.
- Ask the user for clarification.
