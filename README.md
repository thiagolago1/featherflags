# 🪶 featherflags

Lightweight, self-hosted feature flags for React Native / Expo apps. Single Go binary + Postgres.

- **3 environments** out of the box: `development`, `staging`, `production` — independent state per flag, one API key each
- **Deterministic percentage rollouts** — the same user always lands in the same bucket; raising the percent only ever adds users
- **Attribute targeting** — `eq` / `neq` / `in` conditions over user attributes (plan, app version, …)
- **Fail-safe by design** — unknown operators and malformed rules evaluate to `false`; the public key can only evaluate, never read rules

## Quick start

```bash
docker compose up -d
```

Create a project (returns one API key per environment):

```bash
curl -X POST localhost:8080/admin/projects \
  -H "Authorization: Bearer change-me-admin-token" \
  -H "Content-Type: application/json" \
  -d '{"name":"my-app"}'
```

Create a flag and enable it in development at 50% for premium users:

```bash
curl -X POST localhost:8080/admin/projects/<projectId>/flags \
  -H "Authorization: Bearer change-me-admin-token" \
  -H "Content-Type: application/json" \
  -d '{"key":"new-checkout"}'

curl -X PATCH localhost:8080/admin/flags/<flagId>/rules/development \
  -H "Authorization: Bearer change-me-admin-token" \
  -H "Content-Type: application/json" \
  -d '{"enabled":true,"rolloutPercent":50,"conditions":[{"attr":"plan","op":"eq","value":"premium"}]}'
```

Evaluate from the client (this is all the app ever calls):

```bash
curl -X POST localhost:8080/v1/evaluate \
  -H "X-API-Key: ff_dev_..." \
  -H "Content-Type: application/json" \
  -d '{"userId":"user-123","attributes":{"plan":"premium"}}'
# → {"flags":{"new-checkout":true}}
```

## React / React Native SDK

```tsx
import AsyncStorage from "@react-native-async-storage/async-storage";
import { FlagsProvider, useFlag } from "@featherflags/react";

<FlagsProvider
  baseUrl="https://flags.example.com"
  apiKey="ff_prod_..."
  user={{ id: user.id, attributes: { plan: user.plan } }}
  storage={AsyncStorage}
>
  <App />
</FlagsProvider>;

// anywhere below the provider:
const showNewCheckout = useFlag("new-checkout");
```

Fail-safe contract: the SDK never throws. Cold start hydrates from the storage
cache before the network round-trip; if the server is unreachable it serves the
last cached values, and with no cache every flag is `false`.

## API

| Method | Path | Auth |
|---|---|---|
| `POST` | `/v1/evaluate` | `X-API-Key` (per environment) |
| `GET` | `/health` | — |
| `POST` | `/admin/projects` | Bearer `ADMIN_TOKEN` |
| `GET` | `/admin/projects` | Bearer |
| `POST` | `/admin/projects/{id}/flags` | Bearer |
| `GET` | `/admin/projects/{id}/flags` | Bearer |
| `PATCH` | `/admin/flags/{id}/rules/{env}` | Bearer |
| `POST` | `/admin/flags/{id}/archive` · `/unarchive` | Bearer |

## Dashboard

`apps/dashboard` — dark, dense admin UI (React + Vite, zero UI libs). Sidebar of
projects, one row per flag with independent toggles for development / staging /
production, rollout slider and JSON conditions editor per environment, one-click
API-key copy. Toggling production asks for confirmation; the other environments
just flip.

```bash
cd apps/dashboard
npm install
npm run dev   # proxies /admin to the API on :8080
```

Authentication is the server's `ADMIN_TOKEN`, entered once per session. For a
deployed dashboard set `VITE_API_URL` at build time.

## Deploy (Render)

One-click via [Blueprint](https://render.com/docs/infrastructure-as-code): the
included `render.yaml` provisions the API (Docker), Postgres, Redis and the
static dashboard. After the first deploy:

1. Copy the generated `ADMIN_TOKEN` from the API service's Environment tab.
2. Set the dashboard's `VITE_API_URL` to the API's public URL and redeploy it.

## Development

```bash
cd api
docker compose -f ../docker-compose.yml up -d db
DATABASE_URL='postgres://featherflags:featherflags@localhost:5433/featherflags?sslmode=disable' \
  ADMIN_TOKEN=dev go run ./cmd/server
go test ./...
```

Stack: Go 1.26, [chi](https://github.com/go-chi/chi), [pgx](https://github.com/jackc/pgx). Migrations are embedded in the binary and applied on boot.

## Roadmap

- [x] Redis cache for rule sets (optional `REDIS_URL`; invalidated on admin writes, TTL safety net)
- [x] `@featherflags/react` SDK (`packages/sdk-react`) — pluggable storage cache, fail-safe defaults
- [x] React dashboard (`apps/dashboard`) — per-environment toggles, rollout slider, conditions editor
- [ ] SSE stream for real-time flag updates
- [ ] `semver_gte` condition operator for app versions

## License

[MIT](LICENSE)
