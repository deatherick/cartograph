# Cartograph web UI

React + Vite + TypeScript + Tailwind. Served by `ctxd` (see the root
[README](../README.md#web-ui) and
[ADR-0015](../docs/adr/0015-react-web-ui.md)) — this directory is only the
frontend's source; the built output is copied into
`../internal/httpserver/web/` (which `internal/httpserver`'s `go:embed`
directive reads) by `make web` at the repo root.

Some UI code here (design tokens, primitives, layout pattern) is adapted
from Grafel's `webui-v2` (MIT License) — see `../NOTICE.md` for the full,
file-by-file attribution.

## Development

```bash
npm install
npm run dev
```

The dev server proxies `/api/*` to a real `ctxd` instance — start one
separately first:

```bash
# from the repo root
./bin/ctxd --web 127.0.0.1:7420 <path/to/some/indexed/project>
```

## Building

```bash
npm run build          # -> dist/
```

Normally you don't run this directly — `make web` (or `make build`) at the
repo root does this and copies the result into `internal/httpserver/web/`
for you.
