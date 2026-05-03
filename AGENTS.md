# Project conventions for AI assistants

Read this before touching code in this repo.

## What is qdesk

qdesk is a CLI + control plane for AI-driven visual UI testing.
**Your usage of qdesk as a tool** is documented in [`SKILL.md`](./SKILL.md) — read that first if you want to verify a UI you just built.

This file is for AI assistants **modifying the qdesk codebase itself**.

## Hard contracts (don't break)

| Thing | Where | Constraint |
|---|---|---|
| Action wire format | `pkg/protocol/action.go` | JSON tagged-union; field names are public API |
| Session create API | `pkg/protocol/session.go` + `internal/control/server.go` | `POST /v1/sessions` body shape stable |
| `qdesk-agentd` HTTP | `internal/agentd/server.go` | `/health`, `/screenshot`, `POST /actions` are the contract between control plane and sandbox |
| LLM agent interface | `internal/llm/agent.go` | `VisionAgent` is the only thing runner depends on |

## Adding things

### A new action type
1. Add the variant to `protocol.ActionType` constants (`pkg/protocol/action.go`).
2. Add the relevant fields to `protocol.Action` with `omitempty`.
3. Wire the variant in `internal/agentd/input.go::XdotoolInput.Execute`.
4. Update the `actSystemPrompt` in `internal/llm/gemini.go` to teach the model the new shape.
5. Add a unit test in `internal/agentd/input_test.go` and a JSON round-trip test in `pkg/protocol/action_test.go`.

### A new LLM backend
1. Create `internal/llm/<vendor>.go` with a struct implementing `VisionAgent`.
2. Re-use the JSON decision/verify/diagnose schema — the runner only knows about the interface.
3. Wire it into `cmd/qdesk/main.go` selection logic (`--llm` flag).
4. Document the new backend in `SKILL.md`.

### A new sandbox template
1. Create `images/<name>/Dockerfile` + `entrypoint.sh`.
2. Whitelist the template name in `control.handleCreateSession` (currently only `ubuntu-chrome`).
3. If the template needs different `OpenURL` semantics, extend `Runtime.OpenURL`.

## Testing

```bash
go test ./...                              # unit tests
./scripts/smoke-sandbox.sh                 # end-to-end sandbox smoke
./examples/recompdaily-landing.qdesk.yaml  # full pipeline (needs control plane + sandbox image)
```

CI must pass `go vet ./...` and `go test ./...`.

## Style

- Stdlib first (`net/http`, `encoding/json`, `log/slog`). New third-party deps need justification.
- Pure Go where possible (no CGO). `modernc.org/sqlite` is the SQLite choice.
- Exported types in `pkg/...`; internal-only in `internal/...`.
- Errors wrapped with `fmt.Errorf("context: %w", err)`. No `errors.Wrap`.

## Known issues / things in flight

- Gemini Flash systematically under-estimates Y on canvas UIs. Bbox-grounding prompt mitigates. See `project memory` and `internal/llm/gemini.go::actSystemPrompt`.
- Replay mode + self-heal not yet implemented (Phase 1).
- Snapshot/restore not implemented (Phase 2).
- Single-tenant only — control plane has API-key auth but no per-key resource limits.

## What NOT to do

- Don't change `Action` JSON field names without coordinating with all SDK consumers (they're a wire contract).
- Don't add a new third-party dep without checking it compiles cgo-free for the Docker image.
- Don't introduce stateful global vars in the control plane (sessions live in SQLite, runtime is local-only state).
- Don't suppress `cargo clippy` / `go vet` warnings — fix them.
