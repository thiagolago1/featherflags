# 🪶 featherflags

Lightweight, self-hosted feature flags for React Native / Expo apps. Single Go binary + Postgres.

- **3 environments** out of the box: `development`, `staging`, `production` — independent state per flag, one API key each
- **Deterministic percentage rollouts** — the same user always lands in the same bucket; raising the percent only ever adds users
- **Attribute targeting** — `eq` / `neq` / `in` / `semver_gte` conditions over user attributes (plan, app version, …)
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

**Real-time**: pass `realtime` to the provider and flag changes land in the app
the instant they're saved in the dashboard (SSE with automatic reconnection;
polling stays on as a fallback). Measured admin-write → client-update latency:
~30ms on a local stack.

## API

| Method | Path | Auth |
|---|---|---|
| `POST` | `/v1/evaluate` | `X-API-Key` (per environment) |
| `GET` | `/v1/stream` | `X-API-Key` or `?apiKey=` (SSE) |
| `GET` | `/health` | — |
| `POST` | `/admin/projects` | Bearer `ADMIN_TOKEN` |
| `GET` | `/admin/projects` | Bearer |
| `POST` | `/admin/projects/{id}/flags` | Bearer |
| `GET` | `/admin/projects/{id}/flags` | Bearer |
| `PATCH` | `/admin/flags/{id}/rules/{env}` | Bearer |
| `POST` | `/admin/flags/{id}/archive` · `/unarchive` | Bearer |

## Dashboard

`apps/dashboard` — dark, dense admin UI (Next.js, zero UI libs). Sidebar of
projects, one row per flag with independent toggles for development / staging /
production, rollout slider and JSON conditions editor per environment, one-click
API-key copy. Toggling production asks for confirmation; the other environments
just flip.

The dashboard is a **BFF (backend-for-frontend)**, not a static SPA: it's the
only thing that knows the Go API's address and service token, it authenticates
users itself (email/password or company SSO via [next-auth](https://authjs.dev)),
and it stores the session in an httpOnly cookie — never in `localStorage` /
`sessionStorage`, and the API's URL/token never reach the browser. All admin
calls from the browser go to same-origin `/api/backend/*`, which the server
proxies to the internal API after checking the session.

```bash
cd apps/dashboard
cp .env.example .env       # fill in DATABASE_URL, NEXTAUTH_SECRET, INTERNAL_API_URL/TOKEN
npm install
npx prisma migrate dev     # creates the auth schema (users/accounts)
ADMIN_EMAIL=you@company.com ADMIN_PASSWORD=... npm run db:seed  # first admin user
npm run dev
```

## Deploy (Kubernetes)

`deploy/k8s/` has the manifests for a real deployment: the Go API runs as a
`ClusterIP`-only `Deployment` with no `Ingress` (only reachable from the
dashboard pod, enforced by `networkpolicy.yaml`); the dashboard is the single
public entry point via `dashboard-ingress.yaml`; secrets are pulled from the
company's secret manager through `external-secrets.yaml` (adjust
`secretStoreRef`/`remoteRef` to match). See the manifests' comments for what
to fill in (registry/image, hostnames, OIDC issuer).

## Development

```bash
cd api
docker compose -f ../docker-compose.yml up -d db
DATABASE_URL='postgres://featherflags:featherflags@localhost:5433/featherflags?sslmode=disable' \
  ADMIN_TOKEN=dev-only-admin-token-please-rotate-me go run ./cmd/server
go test ./...
```

Integration tests (full HTTP lifecycle + SSE) run against a real Postgres and
skip automatically when `TEST_DATABASE_URL` is unset:

```bash
docker compose up -d db
TEST_DATABASE_URL='postgres://featherflags:featherflags@localhost:5433/featherflags?sslmode=disable' \
  go test -race ./...
```

Stack: Go 1.26, [chi](https://github.com/go-chi/chi), [pgx](https://github.com/jackc/pgx). Migrations are embedded in the binary and applied on boot.

## Roadmap

- [x] Redis cache for rule sets (optional `REDIS_URL`; invalidated on admin writes, TTL safety net)
- [x] `@featherflags/react` SDK (`packages/sdk-react`) — pluggable storage cache, fail-safe defaults
- [x] React dashboard (`apps/dashboard`) — per-environment toggles, rollout slider, conditions editor
- [x] SSE stream for real-time flag updates (`GET /v1/stream` + `realtime` prop in the SDK)
- [x] `semver_gte` condition operator for app versions (e.g. `{"attr":"appVersion","op":"semver_gte","value":"2.1.0"}`)

## License

[MIT](LICENSE)
