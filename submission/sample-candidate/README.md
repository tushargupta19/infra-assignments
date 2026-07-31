# Config Service — Sample Candidate Submission

A minimal Kubernetes configuration management service written in Go, backed
by PostgreSQL, deployed to a local Kubernetes cluster (`kind`) with
infrastructure provisioned via Terraform.

## Architecture

```
cmd/main.go                 — entrypoint: wires config, DB connection/retry, HTTP server, graceful shutdown
internal/handler/           — HTTP layer (thin, delegates to service, maps errors to status codes)
internal/service/           — business logic (validation, readiness)
internal/repository/        — storage interface + in-memory and PostgreSQL implementations
internal/domain/            — shared data types + validation
infra/terraform/            — IaC: namespace, PostgreSQL (StatefulSet + PVC), credentials/secrets
k8s/                        — Kubernetes manifests for the application (ConfigMap, Deployment, Service)
scripts/smoke-test.sh       — end-to-end validation script (used by `make smoke-test`)
Makefile                    — automation entrypoint for the whole local workflow
```

Handlers only parse requests, call the service, and translate errors to HTTP
status codes. The service layer owns validation and readiness checks.
Persistence is behind a `Repository` interface so the HTTP/service layers
never depend on `database/sql` or SQL directly.

## API

| Method | Path         | Description |
|--------|--------------|--------------|
| GET    | /ping        | Liveness check, always returns `pong`. Never touches the DB. |
| GET    | /readyz      | Readiness check. Returns 200 if the storage backend (e.g. PostgreSQL) is reachable, 503 otherwise. |
| GET    | /configs/:id | Retrieve config by ID |
| POST   | /configs     | Create or update a config |

### `:id` semantics

`:id` is an opaque, caller-supplied string identifier (e.g. `cfg_1`). It is
the primary key of the `configs` table — the client chooses it, the server
does not generate one. IDs are case-sensitive and must be non-empty.

### GET /configs/:id

- **200 OK** — returns the config as JSON (see shape below).
- **400 Bad Request** — `:id` is empty (not reachable via normal routing, kept as a defensive check).
- **404 Not Found** — no config exists with that ID. Body: `config not found`.
- **500 Internal Server Error** — unexpected storage error (e.g. DB connection dropped mid-request). The specific error is logged server-side (structured JSON log) but not leaked to the client.

Response shape:

```json
{
  "id": "cfg_1",
  "host": "localhost",
  "port": 8080,
  "app_name": "config-service",
  "log_level": "INFO",
  "created_at": "2026-07-31T10:00:00Z",
  "updated_at": "2026-07-31T10:00:00Z"
}
```

`created_at`/`updated_at` are populated by the PostgreSQL repository and
omitted entirely (not just null) when using the in-memory repository, since
that backend does not track them.

### POST /configs — upsert behavior

Example request body:

```json
{
  "id": "cfg_1",
  "host": "localhost",
  "port": 8080,
  "app_name": "config-service",
  "log_level": "INFO"
}
```

- If no row exists for `id`, one is created.
- If a row exists for `id`, **all fields are overwritten** (full replace, not
  a partial patch) and `updated_at` is refreshed; `created_at` is preserved
  from the original insert.
- `log_level` defaults to `INFO` if omitted or empty.
- Validation (400 Bad Request on failure):
  - `id` is required (non-empty)
  - `host` is required (non-empty)
  - `app_name` is required (non-empty)
  - `port` must be in `1..65535`
  - malformed JSON body → 400 with `invalid request body`
- **200 OK** on success, returning the persisted record as JSON.
- **500 Internal Server Error** on unexpected storage failures.

## Database

### Schema

```sql
CREATE TABLE IF NOT EXISTS configs (
    id         VARCHAR(255) PRIMARY KEY,
    host       VARCHAR(255) NOT NULL,
    port       INTEGER      NOT NULL CHECK (port > 0 AND port <= 65535),
    app_name   VARCHAR(255) NOT NULL,
    log_level  VARCHAR(50)  NOT NULL DEFAULT 'INFO',
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_configs_app_name ON configs (app_name);
```

**Why this shape:**
- `id` as the primary key mirrors the API contract directly — no surrogate
  key needed since the client already supplies a stable identifier.
- A `CHECK` constraint on `port` enforces the valid TCP port range at the
  database level as a second line of defense behind application validation.
- `created_at`/`updated_at` support basic auditability and are cheap to add.
- The `app_name` index supports a realistic secondary access pattern
  (filtering/browsing by application) even though the current API doesn't
  expose it yet.

### Schema creation / migration

The schema is embedded in the binary (`internal/repository/schema.sql` via
`go:embed`) and applied idempotently (`CREATE TABLE IF NOT EXISTS`,
`CREATE INDEX IF NOT EXISTS`) every time the app starts and successfully
connects to PostgreSQL. This means:

- No separate migration tool or step is required to bootstrap a fresh
  database — running the app is enough.
- It intentionally **does not** support incremental schema evolution
  (adding a column to an existing table, for instance). For a longer-lived
  system, this would be replaced with a proper migration tool
  (`golang-migrate`, `atlas`, etc.) run as a Job/init container **before**
  the app starts. See "Known limitations" below.

### Provisioning automation

PostgreSQL itself (the server, not the schema) is provisioned by Terraform
as a Kubernetes `StatefulSet` with a `PersistentVolumeClaim`
(`infra/terraform/postgres.tf`), not by the app or by static YAML. See
"Infrastructure" below for the full rationale.

## Infrastructure

### Cluster choice: kind

`kind` (Kubernetes-in-Docker) was chosen over Minikube because it starts
faster, has minimal host footprint (just needs a container runtime), and
maps cleanly onto CI runners if this were ever automated — Docker is the
only real host dependency. Any of Minikube/k3d would work equally well for
this assignment; this is a preference, not a requirement.

### Split of responsibilities: Terraform vs. Kubernetes manifests

This submission deliberately splits infrastructure into two layers with a
clear boundary:

| Concern | Owner | Why |
|---|---|---|
| Namespace | Terraform (`namespace.tf`) | Needed before any secret/DB resource can be created; Terraform is the source of truth for "environment setup." |
| PostgreSQL (StatefulSet, PVC, Service) | Terraform (`postgres.tf`) | A stateful, provisioned *dependency* of the app — conceptually infrastructure, not application deployment. Using Terraform's `kubernetes` provider means the DB lifecycle (create/destroy/recreate) is managed the same way as any other provisioned resource, with plan/apply visibility. |
| DB credentials (`postgres-credentials`, `config-service-db` Secrets) | Terraform | Generated (`random_password`) rather than hand-typed, and wired directly into the DSN Secret the app consumes — no credential ever needs to be copy-pasted between systems. |
| Application (ConfigMap, Deployment, Service) | Kubernetes manifests (`k8s/`, via `kubectl apply`) | This is the actual workload being iterated on; plain manifests keep the edit-apply-test loop fast without a Terraform plan/apply round-trip for every code change. |

This means **Terraform must be applied before `kubectl apply -f k8s/`** (the
namespace and the `config-service-db` Secret the Deployment references don't
exist otherwise). `make up` encodes this ordering; see "Local setup" below.

Networking: both the app and PostgreSQL use `ClusterIP` Services. The app is
reached from the host via `kubectl port-forward` (or `make port-forward` /
`make smoke-test`) — sufficient for local validation and consistent with
"local setup" scope. PostgreSQL is only reachable in-cluster (by design; it
has no need to be exposed to the host).

### Kubernetes deployment

Plain manifests (no Helm/Kustomize) were used for the application layer.
For a single Deployment + Service + ConfigMap, a templating layer would add
indirection without real benefit at this scope; Helm/Kustomize would be the
right call if this grew multiple environments or services.

- **Liveness probe** (`/ping`) never touches the database — a slow or down
  DB should not cause Kubernetes to kill and restart an otherwise-healthy
  process (that would create a pointless restart loop).
- **Readiness probe** (`/readyz`) checks DB connectivity via `Repository.Ping`.
  If the DB is unreachable, the pod is removed from the Service's endpoints
  (stops receiving traffic) without being killed — it will rejoin
  automatically once the DB recovers.
- `securityContext` runs as non-root, disables privilege escalation, and
  uses a read-only root filesystem — sensible defaults even for a local
  assignment.
- Resource `requests`/`limits` are set to realistic-but-small values
  appropriate for a local single-replica deployment.

## Configuration and Secrets

| Variable       | Source                                         | Description |
|----------------|-------------------------------------------------|--------------|
| `APP_PORT`     | ConfigMap (`k8s/configmap.yaml`)                 | HTTP listen port (default `8080`) |
| `LOG_LEVEL`    | ConfigMap                                        | Reserved for future log-level wiring (currently `slog` always logs at Info/Warn/Error as appropriate) |
| `DATABASE_URL` | Secret `config-service-db` (Terraform-managed)   | PostgreSQL connection string |

- **Config vs. secrets**: non-sensitive values (`APP_PORT`, `LOG_LEVEL`) live
  in a ConfigMap; the only sensitive value (`DATABASE_URL`, which embeds the
  DB password) lives in a Secret. They are never mixed in the same object.
- **Where the DSN comes from**: Terraform generates a random password
  (`random_password.db_password`, unless `TF_VAR_db_password` is supplied),
  builds the full `postgres://` DSN, and writes it directly into the
  `config-service-db` Secret. The app never sees or handles the raw
  username/password separately — it only reads one connection string.
- **`optional: true`** on the `DATABASE_URL` secretKeyRef lets the app pod
  start even if Terraform hasn't been applied yet (it falls back to the
  in-memory repository — see "Reliability" below), which keeps local
  iteration forgiving instead of crash-looping.
- **What would change for production**: use a managed secret store (Azure
  Key Vault / AWS Secrets Manager / Vault) with the CSI Secrets Store driver
  instead of plain Kubernetes Secrets (which are only base64-encoded, not
  encrypted, unless encryption-at-rest is separately configured); rotate
  credentials; require TLS (`sslmode=require`/`verify-full`) instead of
  `sslmode=disable`; and never auto-generate a production DB password
  through a local Terraform run.

## Reliability and Operational Readiness

- **How the app gets DB connection details**: a single `DATABASE_URL` env
  var, sourced from a Secret. No connection details are hardcoded.
- **What happens when the DB is unavailable**:
  - **At startup**: the app retries connecting with exponential backoff
    (500ms → 10s cap, 10 attempts) before giving up and exiting non-zero.
    This tolerates the normal case where the app container starts before
    PostgreSQL is fully ready.
  - **At runtime** (DB was up, then goes away): requests that need the DB
    return `500 Internal Server Error` with a generic body (the real error
    is logged, not leaked to the client); `/readyz` starts failing so
    Kubernetes stops routing traffic to the pod; `/ping` keeps succeeding
    so the pod is not killed and restarts automatically once the DB
    recovers (no restart-loop).
  - **If `DATABASE_URL` is never set**: the app falls back to an in-memory
    repository so `go run ./cmd` and unit tests work without any
    infrastructure. This is a deliberate local-development convenience, not
    a production posture (see "Known limitations").
- **Liveness vs. readiness**: see the Kubernetes Deployment section above —
  `/ping` = process alive, `/readyz` = safe to receive traffic.
- **Repeatable deployment**: schema migration is idempotent, Terraform is
  declarative (`terraform apply` is safe to re-run), and Kubernetes
  manifests are declarative (`kubectl apply` is safe to re-run). `make up`
  can be run repeatedly from a clean or partially-applied state.
- **Verifying health after deployment**: `kubectl rollout status` (built
  into `make deploy`), `make smoke-test` (end-to-end HTTP validation), and
  `make logs` (structured JSON logs) — see "Testing" below.

## Observability

- **Structured logs**: `log/slog` with a JSON handler, so every log line is
  machine-parseable (`{"time":..., "level":"INFO", "msg":"...", ...}`).
- **Startup logs** clearly report which repository backend is active
  (`"DATABASE_URL not set, using in-memory repository"` vs. `"connected to
  postgres and applied schema"`), each DB connection retry attempt, and the
  listening port — so a failed deployment is diagnosable from `kubectl logs`
  alone.
- **Graceful shutdown**: SIGTERM/SIGINT triggers `http.Server.Shutdown`
  with a 10s drain window and a corresponding log line, so pod
  termination (rollouts, scale-down) doesn't abruptly drop in-flight
  requests.
- **Troubleshooting**:
  ```bash
  kubectl -n config-service get pods
  kubectl -n config-service describe pod <pod>
  kubectl -n config-service logs -l app=config-service --tail=100
  kubectl -n config-service logs -l app=postgres --tail=100
  kubectl -n config-service get events --sort-by=.lastTimestamp
  ```
- **What would be added for production**: a Prometheus `/metrics` endpoint
  (request counts/latencies, DB pool stats), trace propagation between the
  HTTP handler and repository calls (OpenTelemetry), and log-level
  configurability (the `LOG_LEVEL` ConfigMap key is reserved for this but
  not yet wired to `slog`'s level).

## Local setup

### Prerequisites

- Go 1.22+
- Docker (container runtime for `kind`)
- `kind`
- `kubectl`
- Terraform >= 1.8

### One-command bootstrap

```bash
cd submission/sample-candidate
make up
```

This runs, in order:

1. `cluster-up` — creates the `kind` cluster (`config-service`), no-op if it already exists.
2. `tf-apply` — `terraform init` + `terraform apply`: creates the `config-service` namespace, PostgreSQL (StatefulSet + PVC + Service), and both Secrets.
3. `deploy` — builds the app image, loads it into `kind` (no registry needed locally), applies `k8s/` manifests, and waits for the rollout to complete.

Then validate end-to-end:

```bash
make smoke-test
```

This port-forwards the service and exercises `/ping`, `/readyz`,
`POST /configs`, `GET /configs/:id` (hit and miss). See
`scripts/smoke-test.sh` for the exact checks.

### Individual steps (equivalent to `make up`, if you want to run them by hand)

```bash
# 1. Cluster
kind create cluster --name config-service

# 2. Infrastructure (namespace, PostgreSQL, secrets)
cd infra/terraform
terraform init
terraform apply -var="kube_context=kind-config-service"
cd ../..

# 3. Build & load the image
docker build -t config-service:latest .
kind load docker-image config-service:latest --name config-service

# 4. Deploy the app
kubectl --context kind-config-service apply -f k8s/
kubectl --context kind-config-service -n config-service rollout status deploy/config-service

# 5. Validate
kubectl --context kind-config-service -n config-service port-forward svc/config-service 8080:8080 &
curl http://localhost:8080/ping
curl http://localhost:8080/readyz
curl -X POST http://localhost:8080/configs -H 'Content-Type: application/json' \
  -d '{"id":"cfg_1","host":"localhost","port":8080,"app_name":"config-service","log_level":"INFO"}'
curl http://localhost:8080/configs/cfg_1
```

### Tear down

```bash
make down
```

Runs `terraform destroy` followed by `kind delete cluster`.

### Run tests

```bash
make test     # go test ./... -count=1 -race
make lint     # gofmt -l . && go vet ./...
```

Unit tests use the in-memory repository (no external dependency required),
so they run identically in CI and locally.

## Testing

- **`internal/handler/handler_test.go`**: exercises every endpoint through
  real `net/http/httptest` requests against the in-memory repository —
  `/ping`, upsert success, get after upsert (round-trip correctness),
  get-not-found (404), upsert with missing ID (400), and malformed JSON
  body (400). This is the fast, dependency-free test suite that runs in CI.
- **`scripts/smoke-test.sh`** (`make smoke-test`): the deployment-level
  validation — proves the *actually deployed* system (real Postgres, real
  Kubernetes networking, real Terraform-provisioned secrets) behaves
  correctly, not just the Go code in isolation.
- **What's intentionally not covered, and why**: there is no automated test
  against a real PostgreSQL instance in the unit test suite (e.g. via
  `testcontainers-go`) — the smoke test covers that path instead, to avoid
  requiring Docker-in-Docker for `go test` in constrained CI environments.
  If this service grew significantly, the next testing investment would be
  a `-tags=integration` test suite against a real Postgres container.

## Repository structure

- **App code** (`cmd/`, `internal/`) is a standard Go layout: entrypoint,
  then `handler` → `service` → `repository` → `domain`, each importable
  independently and unit-testable in isolation.
- **Infra code** (`infra/terraform/`) is split by concern (`main.tf`
  providers, `variables.tf`, `namespace.tf`, `postgres.tf`, `outputs.tf`)
  rather than one large file, so a reviewer can find "how is Postgres
  configured" without reading unrelated provider boilerplate.
- **Deployment config** (`k8s/`) is one manifest per Kubernetes object
  (`configmap.yaml`, `deployment.yaml`, `service.yaml`), which keeps diffs
  small and readable when only one resource changes.
- **Automation** (`Makefile`, `scripts/`) is the single entrypoint a
  reviewer or new contributor needs — no tribal knowledge of the "right"
  command order is required.

This layout is maintainable because each layer (app / infra / deployment /
automation) can be reasoned about, and changed, independently.

## Known limitations

- **No incremental migrations**: schema changes beyond the current
  `CREATE TABLE IF NOT EXISTS` would require adding a real migration tool
  (see "Database" above).
- **Single PostgreSQL replica, no backups**: acceptable for a local
  assignment; production would need at least automated backups and
  ideally a managed database service.
- **In-memory fallback repository**: convenient for `go run ./cmd` without
  infrastructure, but means the app *can* silently run without persistence
  if `DATABASE_URL` is missing in a real deployment. In production, this
  fallback would be removed in favor of failing startup outright when a
  database is required.
- **No automated integration test against real Postgres** in `go test`
  (covered instead by the smoke test) — see "Testing" above.
- **No Prometheus metrics endpoint** yet — logs and probes are the only
  observability signal today.
- **`sslmode=disable`** between the app and PostgreSQL — acceptable for
  same-cluster local traffic, not acceptable for production.

## Responsible AI usage

- AI assistance was used to help design and implement this solution
  (repository/service/handler layering, the Postgres repository and
  migration approach, the Terraform module split, Kubernetes probe
  configuration, the Makefile/smoke-test automation, and this README).
- What was personally verified: the Go code compiles and passes
  `go vet`/`gofmt`/`go test ./... -race`; `terraform fmt -check` and
  `terraform validate` pass against the Terraform configuration; the
  Kubernetes manifests were reviewed for internal consistency (namespace,
  label selectors, Secret/ConfigMap names referenced by the Deployment);
  and the full `make up` → `make smoke-test` flow was run end-to-end
  against a real local `kind` cluster.
- Engineering judgment was applied throughout — e.g. choosing which schema
  constraints matter, where to draw the Terraform/kubectl boundary, and
  which production concerns were explicitly out of scope for a local
  assignment (documented above rather than silently ignored).
