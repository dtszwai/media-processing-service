# Media Processing Service Web Console

This app is the local web console for the media-processing service. It is a Svelte 5 test harness for submitting generation jobs, inspecting traces, draining queues, browsing local S3/DDB state, and checking logs against the compose stack; it is not a production product surface.

The console is a playground for exercising backend-owned use cases. Do not use UI convenience flows as architectural input, and do not move service invariants into this app just because a local testing button needs to trigger them.

## Run

From the repository root:

```bash
make web
```

From this package:

```bash
pnpm dev
```

The Vite server listens on `http://localhost:3001`.

## Environment

```bash
VITE_API_URL=http://localhost:9000
VITE_GRAFANA_URL=http://localhost:3000
VITE_LOKI_URL=http://localhost:3000/api/datasources/proxy/uid/loki/loki
```

The defaults match `make up`. The backend uses `LOCAL_ONLY=true` only for unauthenticated local generation calls; inspection and mutation helpers are served by the Vite-local adapter.

## Tabs

| Tab | Purpose |
| --- | --- |
| `me` | Local tenant/user snapshot, recent media, recent jobs |
| `submit` | Text-to-image and audio-overview generation submitter |
| `library` | Local generated media browser |
| `trace` | Job list plus per-job waterfall, span detail, output preview, and operator actions |
| `queues` | Queue/DLQ inspection and queue mutations |
| `ddb` | DynamoDB key lookup and table inspection |
| `s3` | LocalStack S3 object browser |
| `logs` | Loki/Grafana-linked log search |

Routes are hash-based (`#/submit`, `#/trace/<jobId>`, `#/trace/<jobId>/<spanId>`) so the console can be served as a static Vite app without a router server.

## Commands

```bash
pnpm check
pnpm test
pnpm test:watch
pnpm test:e2e
pnpm test:e2e:ui
pnpm build
```

`pnpm test` runs Vitest unit coverage such as trace waterfall logic. `pnpm test:e2e` runs Playwright against the Vite dev server and currently keeps a smoke check for the local console shell.

## Structure

```text
src/
├── App.svelte
├── app.css
├── features/
│   ├── ddb/
│   ├── jobs/
│   ├── library/
│   ├── logs/
│   ├── me/
│   ├── queues/
│   ├── s3/
│   ├── submit/
│   └── trace/
├── lib/
└── shared/
```

Feature folders own their panel components and local helpers. `shared/api.ts` creates product Connect clients, `shared/local-ops` owns browser calls to the Vite-local adapter, `shared/route.svelte.ts` owns hash routing, and generated protobuf clients come from `frontend/packages/api-client`.
