# qdesk

AI-native testing platform — describe tests in natural language, AI agents execute them in cloud sandboxes, tests self-heal when UIs change.

**Status:** Phase 0 (MVP). See `docs/superpowers/specs/` for design and `docs/superpowers/plans/` for implementation plans.

## Quickstart (Phase 0 sandbox)

Build and run a single sandbox container:

```bash
docker build -t qdesk/ubuntu-chrome:dev -f images/ubuntu-chrome/Dockerfile .
docker run -d --rm --name qdesk-sbx -p 7878:7878 qdesk/ubuntu-chrome:dev

# Health
curl http://localhost:7878/health
# => {"status":"ok","version":"0.1.0"}

# Take a screenshot
curl http://localhost:7878/screenshot --output /tmp/screen.png
file /tmp/screen.png  # PNG image data, 1920 x 1080, 8-bit/color RGB

# Click at (100, 200)
curl -X POST http://localhost:7878/actions \
    -H 'content-type: application/json' \
    -d '{"type":"click","x":100,"y":200}'

# Type text
curl -X POST http://localhost:7878/actions \
    -H 'content-type: application/json' \
    -d '{"type":"type","text":"hello world"}'

# Press Ctrl+S
curl -X POST http://localhost:7878/actions \
    -H 'content-type: application/json' \
    -d '{"type":"key","keys":["ctrl","s"]}'

docker stop qdesk-sbx
```

Or run the smoke test:

```bash
./scripts/smoke-sandbox.sh
```

## Layout

- `crates/qdesk-protocol/` — wire types shared across host & sandbox
- `crates/qdesk-agentd/`   — in-sandbox HTTP daemon (binary)
- `images/ubuntu-chrome/`  — Dockerfile + entrypoint for default sandbox image
- `scripts/`               — operator scripts (smoke test, etc.)
- `docs/superpowers/specs/` — design documents
- `docs/superpowers/plans/` — implementation plans

## License

Apache 2.0 — see `LICENSE`.
