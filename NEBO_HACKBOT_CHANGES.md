# NEBO-HACKBOT Changes Log

Tracks every change from the PentAGI v2.1.0 base, with files affected, rationale, and runtime-testing notes.

---

## Rename: PentAGI → NEBO-HACKBOT (user-facing branding)

**Decision:** Keep Go module path (`github.com/vxcontrol/pentagi`) and internal import paths unchanged to avoid a sprawling rename that risks breaking the build. Only user-facing display names, container names, env var prefixes, and branding strings are changed. Auth salt prefixes in `session.go` and `auth_middleware.go` are kept as-is to preserve session/token compatibility.

### Commit 1: Backend Go display strings (14 files)
- `backend/cmd/pentagi/main.go` — startup log
- `backend/pkg/version/version.go` — binary name default
- `backend/pkg/server/router.go` — Swagger title, description, logger name
- `backend/pkg/server/docs/{docs.go,swagger.json,swagger.yaml}` — API docs
- `backend/pkg/providers/performer.go` — Graphiti source descriptions
- `backend/pkg/providers/helpers.go` — error message
- `backend/pkg/tools/searchers/searxng.go` — User-Agent header
- `backend/pkg/tools/terminal.go` — container name prefix
- `backend/pkg/docker/client.go` — warning message
- `backend/pkg/config/config.go` — default DB connection string
- `backend/pkg/config/tenant.go` — Docker label key
- `backend/pkg/server/services/graphql.go` — logger names

**Runtime test:** Start the stack, verify UI title shows "NEBO-HACKBOT", containers are named `nebo-hackbot-terminal-*`, Swagger UI shows correct title.

### Commit 2: Frontend UI strings (16 files)
- Page title, web manifest, sidebar, login heading, flow placeholders
- API tokens page, resources page, route titles, storage keys
- All test assertions updated

**Runtime test:** Load the web UI; verify branding on login page, sidebar, flow creation, and settings pages.

### Commit 3: Compose, Dockerfile, scripts, env (11 files)
- docker-compose*.yml — service/container/volume/network names, env var prefixes, image defaults
- Dockerfile — binary name, system user/group, /opt paths, labels
- scripts — SSL cert org/CN, build version strings
- .env.example — all PENTAGI_ prefixes renamed

**Runtime test:** `docker compose up -d` works with new service names; containers named correctly.

### Commit 4: Installer, locale, docs (12 files)
- Installer Go files — banner, checker paths/volumes, processor constants, locale (~100+ strings)
- README.md — all prose, env vars, container names (~243 references)
- CLAUDE.md, CONTRIBUTING.md — project description, paths

**Runtime test:** Run the installer wizard; verify all screens show NEBO-HACKBOT branding.

### NOT changed (by design)
- Go module path and all import paths
- Auth salt prefixes (pentagi.cookie.auth, pentagi.jwt.signing, pentagi.automation)
- DB advisory lock names and GORM logger names
- GitHub URLs (github.com/vxcontrol/pentagi)
- pentagi.com and update.pentagi.com domain URLs
- Go identifier names (PentagiRunning, ProductStackPentagi, etc.)
