# qdesk

AI-native testing platform — describe tests in natural language, AI agents execute them in cloud sandboxes, tests self-heal when UIs change.

**Status:** Phase 0 (MVP). See `docs/superpowers/specs/` for design and `docs/superpowers/plans/` for implementation plans.

## Quickstart (Phase 0 sandbox)

```bash
docker build -t qdesk/ubuntu-chrome:dev images/ubuntu-chrome
docker run -d -p 7878:7878 --name qdesk-sbx qdesk/ubuntu-chrome:dev
curl http://localhost:7878/health
curl http://localhost:7878/screenshot --output /tmp/screen.png
```

## License

Apache 2.0 — see `LICENSE`.
