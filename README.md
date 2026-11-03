# StreamForge

StreamForge is a production-minded video-streaming backend built as a modular Go monolith with independently scalable background workers. The project is intentionally being delivered in phases. **Phases 1 and 2 are complete:** the operational foundation now includes secure authentication and user profiles, without prematurely implementing video features.

## Current status

Implemented through Phase 2:

- Chi HTTP service with graceful startup and shutdown
- PostgreSQL connection pooling and dependency-aware readiness
- versioned, reversible SQL migrations
- JSON structured logs and request/correlation IDs
- Prometheus HTTP and Go runtime metrics
- provisioned local Grafana dashboard
- embedded OpenAPI contract and Swagger UI
- Docker image and one-command Docker Compose environment
- unit tests and a testcontainers-go PostgreSQL integration test
- CI checks for formatting, vetting, linting, tests, builds, and container builds
- user registration and case-insensitive unique identities
- Argon2id password hashing with timing-equalized login failures
- short-lived issuer-constrained JWT access tokens
- opaque, hashed refresh tokens with transactional one-time rotation
- refresh-family revocation when token reuse is detected
- logout, bearer authentication, and USER/ADMIN role middleware
- public and authenticated profile endpoints
- strict JSON decoding, allow-listed profile updates, and auth rate limits

Video metadata and upload routes remain intentionally deferred to Phase 3 and later.

## Architecture

The target remains a modular monolith plus worker. Module boundaries will separate HTTP transport, application services, repositories, and infrastructure adapters. The API and worker will share domain packages but will be separate binaries and deployment units.

```mermaid
flowchart LR
    Client --> API[Go API / Chi]
    API --> PG[(PostgreSQL users + sessions)]
    API -. later phases .-> Redis[(Redis)]
    API -. later phases .-> MQ[(RabbitMQ)]
    API -. presigned URLs .-> Object[(MinIO / S3)]
    MQ -. later phases .-> Worker[Go Worker + FFmpeg]
    Worker -. later phases .-> PG
    Worker -. later phases .-> Object
    Prometheus --> API
    Grafana --> Prometheus
```

Application code in the completed phases currently uses PostgreSQL. Redis, RabbitMQ, and MinIO are included in Compose so the local platform has stable endpoints as later phases arrive.

### Planned upload and transcoding flow

```mermaid
sequenceDiagram
    participant C as Client
    participant A as API
    participant D as PostgreSQL
    participant O as MinIO / S3
    participant Q as RabbitMQ
    participant W as Worker / FFmpeg
    C->>A: Create video metadata
    A->>D: Insert video (CREATED)
    C->>A: Request short-lived upload URL
    A-->>C: Presigned PUT URL
    C->>O: Upload source directly
    C->>A: Confirm upload completion
    A->>Q: Publish video.transcode
    Q->>W: Deliver retry-safe job
    W->>O: Read source and write HLS assets
    W->>D: Mark video READY
    C->>A: Request playback authorization
    A-->>C: Manifest/CDN URL
```

This sequence is a design contract for Phases 3–6, not a claim that the endpoints exist today.

## Technology stack

| Concern | Choice |
|---|---|
| Language and HTTP | Go, Chi |
| Authentication | Argon2id, JWT access tokens, opaque rotating refresh tokens |
| Primary data store | PostgreSQL, pgxpool |
| Migrations | golang-migrate |
| Cache and counters | Redis (planned application integration) |
| Jobs | RabbitMQ (planned application integration) |
| Object storage | MinIO locally; S3-compatible adapter planned |
| Video processing | FFmpeg and HLS (planned) |
| Observability | slog JSON logs, Prometheus, Grafana |
| Integration tests | testcontainers-go |
| Delivery | Docker Compose, GitHub Actions; Kubernetes in Phase 8 |

## Repository layout

```text
cmd/
  api/                 API composition root
  migrate/             explicit migration command
docs/                  embedded OpenAPI contract
internal/
  auth/                authentication service, crypto, sessions, middleware, HTTP
  user/                user domain, repository, profile service, HTTP
  config/              environment parsing and validation
  health/              liveness/readiness business logic
  platform/
    database/          PostgreSQL adapter
    httpserver/        router and transport middleware
    observability/     structured logger
    web/                JSON decoding and response envelopes
migrations/            ordered up/down SQL migrations
deployments/docker/    Prometheus and Grafana configuration
tests/integration/     container-backed integration tests
```

Remaining domain directories (`video`, `upload`, and others) will be created only when their phase begins, keeping architecture intentional rather than filling the repository with empty abstractions.

## Local setup

Requirements: Docker with Compose. A local Go toolchain is only needed when running commands outside containers.

```bash
cp .env.example .env
docker compose up --build
```

Once healthy:

- API: <http://localhost:8080>
- Swagger UI: <http://localhost:8080/docs/>
- raw OpenAPI: <http://localhost:8080/openapi.yaml>
- Prometheus: <http://localhost:9090>
- Grafana: <http://localhost:3000> (default local credentials: `admin` / `admin`)
- RabbitMQ management: <http://localhost:15672>
- MinIO console: <http://localhost:9001>

Verify the API:

```bash
curl -i http://localhost:8080/health
curl -i http://localhost:8080/ready
curl -s http://localhost:8080/metrics
```

Stop containers with `docker compose down`. Add `--volumes` deliberately if you also want to remove local persistent data.

## Configuration

Configuration is environment-only, making the same binary suitable for containers and future Kubernetes deployments.

| Variable | Default | Purpose |
|---|---:|---|
| `STREAMFORGE_ENV` | `development` | Runtime environment label |
| `STREAMFORGE_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `STREAMFORGE_HTTP_ADDR` | `:8080` | API listen address |
| `STREAMFORGE_HTTP_READ_HEADER_TIMEOUT` | `5s` | Header deadline for slow-client protection |
| `STREAMFORGE_HTTP_READ_TIMEOUT` | `10s` | HTTP read timeout |
| `STREAMFORGE_HTTP_WRITE_TIMEOUT` | `15s` | HTTP write timeout |
| `STREAMFORGE_HTTP_IDLE_TIMEOUT` | `60s` | Keep-alive idle timeout |
| `STREAMFORGE_SHUTDOWN_TIMEOUT` | `15s` | Graceful shutdown deadline |
| `STREAMFORGE_DATABASE_URL` | required | PostgreSQL URL; never logged |
| `STREAMFORGE_DATABASE_MAX_CONNS` | `20` | Pool maximum |
| `STREAMFORGE_DATABASE_MIN_CONNS` | `2` | Pool warm minimum |
| `STREAMFORGE_DATABASE_CONNECT_TIMEOUT` | `5s` | Startup/readiness DB timeout |
| `STREAMFORGE_AUTH_JWT_ISSUER` | `streamforge` | Required JWT issuer claim |
| `STREAMFORGE_AUTH_JWT_SECRET` | required | HMAC signing secret; minimum 32 bytes |
| `STREAMFORGE_AUTH_ACCESS_TOKEN_TTL` | `15m` | Access-token lifetime |
| `STREAMFORGE_AUTH_REFRESH_TOKEN_TTL` | `720h` | Refresh-token lifetime |
| `STREAMFORGE_AUTH_LOGIN_RATE_LIMIT` | `5` | Login attempts per local window/client |
| `STREAMFORGE_AUTH_LOGIN_RATE_WINDOW` | `1m` | Login rate-limit window |
| `STREAMFORGE_AUTH_REGISTER_RATE_LIMIT` | `3` | Registration attempts per local window/client |
| `STREAMFORGE_AUTH_REGISTER_RATE_WINDOW` | `1h` | Registration rate-limit window |

Compose-specific service credentials are listed in [.env.example](.env.example). Its values are development defaults only; replace every password outside local development and inject secrets through the deployment platform.

## API conventions

The service exposes `/health`, `/ready`, `/metrics`, `/docs/`, and `/openapi.yaml`. Product APIs live below `/api/v1`.

Authentication and user endpoints currently available:

```text
POST  /api/v1/auth/register
POST  /api/v1/auth/login
POST  /api/v1/auth/refresh
POST  /api/v1/auth/logout
GET   /api/v1/users/{username}
GET   /api/v1/users/me
PATCH /api/v1/users/me
```

Registration creates the account but does not automatically create a session. Login returns a short-lived access token and a refresh token. Send the access token as `Authorization: Bearer <token>`. Refresh tokens are single-use: clients must atomically replace the old value with the value returned by `/refresh`.

Example flow:

```bash
curl -sS http://localhost:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"forge_user","email":"forge@example.com","password":"correct horse battery staple"}'

curl -sS http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"forge@example.com","password":"correct horse battery staple"}'
```

Every response carries `X-Request-ID`. A syntactically safe caller-provided ID is preserved; otherwise the API creates one. Access logs include method, path, response status, duration, and request ID without logging bodies, credentials, or tokens.

Errors use a stable envelope:

```json
{
  "error": {
    "code": "ROUTE_NOT_FOUND",
    "message": "The requested route was not found"
  }
}
```

Liveness checks only process health. Readiness checks PostgreSQL and returns `503` when the API should be removed from traffic.

## Database architecture and migrations

PostgreSQL is the source of truth. The Phase 1 migration enables `pgcrypto` for database-side cryptographic/UUID utilities and `pg_trgm` for later indexed search support. Phase 2 adds `citext`, `users`, and `refresh_tokens`. Database constraints enforce roles, lengths, username format, unique identities, token-hash size, and referential integrity.

Only Argon2id password hashes and SHA-256 hashes of 256-bit random refresh tokens are persisted. A refresh row records its family, expiry, consumption, revocation, and replacement. Rotation locks the presented row, inserts its replacement, and consumes the old token in one transaction. Presenting a consumed or revoked token revokes every token in that family before returning an authentication failure.

Migrations are explicit and run in a one-shot Compose service before the API starts. This avoids race conditions from every API replica attempting schema changes on startup.

```bash
export STREAMFORGE_DATABASE_URL='postgres://streamforge:streamforge@localhost:5432/streamforge?sslmode=disable'
make migrate-up
make migrate-down  # reverts exactly one migration
```

Production rollbacks must consider whether a down migration is data-destructive before it is run.

## Video-processing pipeline

Planned processing is asynchronous. Upload bytes will go directly from clients to object storage through a short-lived presigned URL. After object metadata is validated, the API will publish an idempotent `video.transcode` job. A separately scaled worker will use FFmpeg to validate media, create thumbnails, and generate 360p/720p HLS renditions. Storage/CDN—not the Go API—will serve manifests and segments.

RabbitMQ retry exchanges, exponential backoff, a dead-letter queue, job leases, and durable idempotency records are Phase 5 concerns. They are not represented by placeholder code in the completed phases.

## Observability

The API emits JSON logs to stdout. Prometheus currently receives:

- `http_requests_total{method,route,status}`
- `http_request_duration_seconds{method,route,status}`
- standard Go runtime and process metrics

Route templates—not raw paths—are metric labels, preventing user/video IDs from causing unbounded cardinality. Later phases will add upload, processing, queue, and cache metrics at the services that own them.

Grafana provisions its Prometheus data source and the StreamForge Overview dashboard automatically.

## Testing

```bash
make fmt
make lint
make test
make test-integration
make build
```

Unit tests cover configuration, Argon2id, token parsing, authentication services, rate limiting, health behavior, request IDs, strict JSON parsing, status codes, and error envelopes. Integration tests use testcontainers-go to apply the real migrations and exercise registration, duplicate identity protection, login failures, bearer authorization, profile access, refresh rotation, replay detection, family revocation, and logout. Docker must be running for integration tests.

The CI workflow runs formatting, `go vet`, golangci-lint, race-enabled unit tests, PostgreSQL integration tests, binary builds, and a Docker image build.

## Deployment

The completed phases are deployed locally with Docker Compose. The final image is a small non-root distroless runtime containing separate `api` and `migrate` binaries. The migration process is selected explicitly by overriding the image entrypoint.

Kubernetes resources are deliberately deferred until the Compose workflow and application boundaries are established. Phase 8 will add separate API/worker Deployments, ConfigMaps, Secrets, Services, Ingress, and HPAs so request handling and compute-heavy transcoding scale independently.

## Design decisions

- **Modular monolith first:** one domain codebase limits distributed-system overhead; the worker is split only at the process boundary needed for asynchronous processing.
- **Explicit composition:** constructors and small infrastructure interfaces make dependencies visible without a DI framework.
- **Separate migrations:** schema changes are a deployment action, not a side effect of replica startup.
- **Fail-fast startup:** invalid configuration or unavailable PostgreSQL prevents the API from advertising service.
- **Thin transport:** HTTP code translates requests and responses; business rules belong in module services as phases are implemented.
- **S3 boundary:** future upload and playback code will depend on a narrow object-storage interface, allowing MinIO, S3, and CDN URL implementations.
- **Short access, rotating refresh:** access tokens remain stateless and short-lived; durable refresh families support logout, rotation, replay detection, and session revocation.
- **No credential oracle:** unknown emails and incorrect passwords use the same error and an Argon2 verification path.
- **Allow-listed updates:** profile handlers decode dedicated DTOs and cannot bind role, email, password hash, or identifiers.

## Known limitations

- There are no videos, uploads, background workers, password-reset flow, email verification, or MFA yet.
- Logout revokes refresh tokens immediately; an already-issued access token remains valid until its short expiry.
- Auth rate limits are bounded in-memory per API replica. Phase 8 will move distributed limits to Redis and add trusted-proxy client-IP handling.
- Expired refresh-token cleanup is not yet scheduled; a later maintenance job should delete old rows after the audit window.
- JWT signing currently uses one HMAC key without key IDs; production key rotation is a future hardening task.
- Readiness currently checks only PostgreSQL because it is the only required application dependency.
- The local Swagger page loads its UI assets from a public CDN; the OpenAPI document itself is embedded in the binary.
- Local credentials are intentionally convenient and are not suitable for shared or production environments.
- Compose is a single-node development topology and does not provide database backups or high availability.

## Roadmap

1. ✅ Phase 1 — operational foundation.
2. ✅ Phase 2 — authentication and user profiles.
3. **Phase 3 — video metadata, ownership, visibility, and processing state.**
4. Phase 4 — S3-compatible storage and direct uploads.
5. Phase 5 — RabbitMQ worker, retries, DLQ, FFmpeg, and thumbnails.
6. Phase 6 — HLS output and playback authorization.
7. Phase 7 — likes, comments, playlists, tags, PostgreSQL search, views, and analytics.
8. Phase 8 — Redis policies, broader observability, k6, hardened CI/CD, Kubernetes, and autoscaling.

## Future improvements

After the requested phases: CDN signing, malware scanning, resumable multipart uploads, additional HLS profiles, content moderation, captions, richer recommendations, managed PostgreSQL/Redis/RabbitMQ, multi-region object replication, and progressive delivery.
