# cms-backend

Go (Gin) API for the SBI Securities CMS. It is the only process that talks to
PostgreSQL, and it does so **only by calling stored routines** — there is no
`SELECT`/`INSERT`/`UPDATE`/`DELETE` text anywhere in this codebase.

```
browser ──► Next.js (cms-website)  ──/api/*──►  cms-backend (Gin)  ──►  PostgreSQL
            UI + thin proxy                      auth, validation,       cms.* routines
                                                 uploads, public API     (SECURITY DEFINER)
```

## How the database is reached

`internal/db` builds exactly one shape of statement,
`SELECT * FROM cms.<routine>($1, …)`, with every value bound. The routine name
is checked against `^cms\.[a-z_]+$` and comes from code or from
`internal/cms/registry.json`, never from a request.

The routines already existed (`cms-website/db/migrations`). Migration
`016_api_routine_boundary.sql` makes them the *only* door:

- every routine in `cms` is `SECURITY DEFINER` with a pinned `search_path`;
- `EXECUTE` is revoked from `PUBLIC` and granted to the `sbi_cms_api` role;
- that role has `USAGE` on the schema and **no table privileges**, so even a
  bug or SQL injection in this service cannot read or write a table directly.

Reads stay functions rather than `CREATE PROCEDURE`: a PostgreSQL procedure
cannot return a result set, and the dashboard needs rows back.

## Run it

```sh
# 1. Database (once, as a superuser) — creates sbi_cms and sbi_cms_api
psql -U postgres -h localhost -v app_password=… -v api_password=… -f ../cms-website/db/setup.sql
# 2. Schema + routines (as the owner), then the API role's grants
cd ../cms-website && npm run db:migrate && npm run db:seed
psql -U sbi_cms -d sbi_cms -f db/grant-api-role.sql   # only if the role was created after migrating

# 3. This service
cd ../cms-backend
cp .env.example .env     # fill in DATABASE_URL, CMS_SESSION_SECRET, CMS_PUBLIC_API_TOKEN
go run ./cmd/server      # listens on 127.0.0.1:8080

# 4. The dashboard (proxies /api/* here; see CMS_BACKEND_URL)
cd ../cms-website && npm run dev
```

`GET /healthz` answers 200 when the database does.

## Layout

| Path | What it holds |
| --- | --- |
| `cmd/server` | entrypoint, graceful shutdown |
| `internal/config` | environment + `.env` loading, refuses to start without a secret |
| `internal/db` | the routine caller; converts rows to the JSON shapes the dashboard expects |
| `internal/apierr` | the `{ok, data}` / `{ok:false, error}` envelope and the SQLSTATE → HTTP mapping |
| `internal/auth` | scrypt passwords, HMAC-hashed sessions, login throttle, guards |
| `internal/cms` | `registry.json` (what each fold contains), field validation, argument building |
| `internal/media` | upload storage (generated names, type + content check) |
| `internal/handlers`, `internal/server` | endpoints and routing |

## Compatibility

Routes, JSON envelope, cookie name, password hashing and session hashing are
identical to what the Next.js app used, so existing admin passwords and live
sessions keep working. `internal/auth/auth_test.go` checks a scrypt vector
produced by the Node implementation.

`internal/cms/registry.json` was generated from
`cms-website/src/server/cms/registry.ts`. When a collection or field changes,
update both.

## Notes and limits

- The login throttle is in-process (per instance), as it was before.
- Bind address defaults to loopback; put it behind the Next.js proxy, not on a
  public interface. `X-Forwarded-For` is taken from the proxy.
- `go test ./...` covers password compatibility and the throttle; the handlers
  were exercised against a live database but have no automated suite yet.
