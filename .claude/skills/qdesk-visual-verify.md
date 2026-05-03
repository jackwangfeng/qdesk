---
name: qdesk-visual-verify
description: Use when you've changed a web/desktop UI (HTML/CSS/JS, React, Flutter Web, Vue, Svelte, etc.) and want to verify the user-visible behaviour actually works — not just compiles. Writes a tiny YAML test, runs an AI agent in a sandboxed Chromium, captures screenshots + AI verification of expectations, returns pass/fail with a report. Cheap (~$0.005 per test) and fast (~30-60s). Available when `qdesk` and `qdesk-control` binaries are on PATH and `qdesk/ubuntu-chrome:dev` Docker image exists.
---

# qdesk — visual verification skill

You can use qdesk to **visually verify** a UI you just built or changed. Faster than asking the human to "go check", and produces a report you can attach to a PR.

## Decision: should I use qdesk for this change?

```
Did my last change touch a UI surface (page render, route, button, form, canvas) that a human would visually inspect?
   ├─ no  → don't use qdesk; pick another tool
   └─ yes → does the project already have a passing E2E suite (Playwright / Cypress) that covers this?
              ├─ yes → run that first; only use qdesk for what they can't cover (canvas / vision-grounded checks)
              └─ no  → write a qdesk test (high leverage)
```

## Quick check: is qdesk available?

Run these in parallel; all three must succeed:

```bash
command -v qdesk
command -v qdesk-control
docker images qdesk/ubuntu-chrome:dev --format '{{.Repository}}'
```

If any fails: read `/path/to/qdesk/SKILL.md` Prerequisites section and either install or escalate to the human.

## Quick check: is the control plane running?

```bash
curl -fsS http://127.0.0.1:8090/v1/health
```

If it 404s or connection refuses, ask the human to start it in a separate terminal:
```bash
export QDESK_DEV_KEY=devkeysecret
qdesk-control --listen 127.0.0.1:8090 --dev-key $QDESK_DEV_KEY --image qdesk/ubuntu-chrome:dev
```

## Workflow

1. **Identify what to verify.** Pick the smallest user-observable behaviour you changed. Examples:
   - "After login, the dashboard shows the user's name."
   - "Clicking 'Save' on the new entry form makes the entry appear in the list."
   - "Switching theme to dark makes the body background go dark."

2. **Make sure the app is reachable.** Either the user has a dev server up (`flutter run -d web-server`, `npm run dev`, etc.) or you serve a static build with `python3 -m http.server 8888`. Note the URL — use `http://host.docker.internal:PORT` from inside the sandbox to reach the host.

3. **Write a YAML test under `tests/qdesk/`.** Filename: descriptive, ends in `.qdesk.yaml`.
   ```yaml
   name: "<one-line label>"
   template: ubuntu-chrome
   url: http://host.docker.internal:8888

   goal: |
     <one short paragraph describing what the test should accomplish>

   expect:
     - <specific observable assertion #1>
     - <specific observable assertion #2>

   ttl_seconds: 180
   max_steps: 10
   ```

4. **Run.**
   ```bash
   export GEMINI_API_KEY=...
   export QDESK_DEV_KEY=...    # match what control plane was started with
   qdesk run --control http://127.0.0.1:8090 tests/qdesk/<name>.qdesk.yaml
   ```

5. **Interpret.**
   - Exit 0 + `✅ PASS` → behaviour verified, commit the test.
   - Exit 1 + `❌ FAIL` → read the printed `Diagnosis:` and the `report.html`. Three possibilities:
     a) Your code change is wrong → fix the code, re-run.
     b) The test description was ambiguous → rewrite `goal` / `expect`, re-run.
     c) Gemini mis-clicked (canvas Y-coord drift) → re-run; if still flaky, switch to `--llm gemini-2.5-pro`.

## Authoring quality bar

| | Bad | Good |
|---|---|---|
| `goal` | "test the login" | "Click the 'Sign in' button on the welcome screen, then enter `test@example.com` in the email field and click Continue" |
| `expect` | "the page works" | "A success toast labelled 'Welcome back' appears in the top-right corner" |
| `expect` count | 1 generic | 2-4 specific |
| URL | `localhost:8888` (won't resolve from sandbox) | `host.docker.internal:8888` |

## Cost / time budget per test

- Gemini Flash: ~$0.005, ~30-60s wall-clock. Default for routine checks.
- Gemini Pro: ~$0.05, ~60-120s. Use when Flash mis-clicks.
- For very long flows (10+ turns), bump `max_steps:`.

## When tests should be PERMANENT vs ONE-OFF

- **Permanent (commit to repo):** core flows, golden paths, regression-prone areas. Keep them updated as the UI evolves.
- **One-off (don't commit):** quick verification while iterating. Use `qdesk run` and discard.

When committing: place under `tests/qdesk/`, add to the project's CI pipeline if it has one, and reference from `AGENTS.md` / `CLAUDE.md`.

## What you should NOT do

- Don't write tests with hard-coded pixel coordinates in `goal` — let the agent figure out where to click.
- Don't chain 20+ `steps:` — break into multiple smaller tests.
- Don't put secrets in `expect` strings (they go to the LLM provider).
- Don't run qdesk for backend-only changes — wasteful.
- Don't ignore a FAIL because "the test description must be wrong" — investigate the screenshot first; fail-by-test-bug is rare in practice.

## Reference

Full documentation: `/path/to/qdesk/SKILL.md` and `/path/to/qdesk/README.md`.
