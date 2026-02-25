# Project Instructions — Ubuntu Manpages Operator

This repository contains two distinct projects that work together:

1. **Go application** — Downloads Ubuntu `.deb` packages, extracts manpages, converts them to HTML, and serves them via HTTP.
2. **Juju charm (Python)** — A Kubernetes operator that deploys and manages the Go application as a workload using Pebble.

The Go app is packaged into an OCI image via `rockcraft`, and the charm drives it using the Juju/Pebble model.

---

## Workflow Requirements

**Before finishing any task**, always:

1. Run `make format; make lint`
1. Consider if the test coverage needs updating
1. Update `README.md` if the change adds/removes/renames CLI commands, env vars, or user-facing features
1. Update `.github/copilot-instructions.md` if the change affects structure, convention, workflow, etc.
1. Provide a **draft commit message** using Conventional Commits format

---

## Repository Layout

```
cmd/                  # Go entry points (3 binaries)
  ingest/             #   Bulk manpage ingestion
  ingest-pkg/         #   Single-package ingestion (dev/debug)
  server/             #   HTTP server
internal/             # Go internal packages
  config/             #   Env-var based configuration (+ .env file support)
  fetcher/            #   Downloads Packages.gz indexes and .deb files from the Ubuntu archive
  launchpad/          #   Resolves release codenames → version numbers via Launchpad API
  logging/            #   Structured slog logger setup
  pipeline/           #   Orchestrates extraction, conversion, transformation, and storage
  search/             #   In-memory indexed manpage search (with fuzzy matching via Damerau-Levenshtein)
  sitemap/            #   XML sitemap generation
  storage/            #   Filesystem-based HTML/gzip storage with per-package SHA1 cache
  transform/          #   8-stage HTML transformation pipeline (mandoc output → web-ready HTML)
  web/                #   HTTP server, routes, templates, static assets
    templates/        #   Go html/template files (base, base-landing, head, nav, footer, search-form, index, manpage, browse, search, 404)
    static/           #   CSS and JS served with ETag caching

src/                  # Python charm source
  charm.py            #   ManpagesCharm — main operator class
  manpages.py         #   Manpages helper — builds Pebble layers, triggers updates, purges
lib/                  # Vendored charm libraries (traefik_k8s ingress)

tests/                # All tests
  unit/               #   Python unit tests for the charm (ops.testing/scenario)
  integration/        #   Python integration tests (jubilant, real Juju model)
  spread/             #   Spread test orchestration (LXD VMs or GitHub CI)

charmcraft.yaml       # Charm build definition (base ubuntu@24.04, Pebble containers, storage, actions)
rockcraft.yaml        # OCI image build definition (Go binaries + mandoc)
pyproject.toml        # Python project config (dependencies, linting, testing)
go.mod                # Go module definition
Makefile              # Python lint/format/test targets
.env.example          # Example environment variables for local Go development
spread.yaml           # Spread test runner configuration
```

---

## Go Application

### Architecture

The app is a manpage pipeline + web server. There are **three binaries**:

| Binary           | Purpose                                                                                       |
| ---------------- | --------------------------------------------------------------------------------------------- |
| `cmd/server`     | HTTP server — serves manpages, search, sitemaps, browse, health checks                        |
| `cmd/ingest`     | Bulk ingestion — fetches all packages for configured releases, converts manpages, writes HTML |
| `cmd/ingest-pkg` | Single-package ingestion — for development/debugging a specific package                       |

All three read configuration from environment variables (see `.env.example`), optionally loading a `.env` file from the working directory. Each binary creates a structured logger via `logging.BuildLogger()` and immediately calls `slog.SetDefault(logger)` so that any `slog` package-level calls throughout the codebase use the same `TextHandler` format.

The `server` and `ingest` binaries have no CLI flags — all configuration comes from environment variables. The `ingest-pkg` binary accepts two required CLI flags (`-release` and `-package`) to select a single package for debugging, with remaining configuration from the environment.

### Configuration (environment variables)

| Variable                   | Default                                                  | Purpose                                                |
| -------------------------- | -------------------------------------------------------- | ------------------------------------------------------ |
| `MANPAGES_SITE`            | `https://manpages.ubuntu.com`                            | Public-facing site URL (used in sitemaps, links)       |
| `MANPAGES_ARCHIVE`         | `https://archive.ubuntu.com/ubuntu`                      | Ubuntu package archive URL                             |
| `MANPAGES_PUBLIC_HTML_DIR` | `/app/www`                                               | Root output directory for HTML and gzip files          |
| `MANPAGES_RELEASES`        | `trusty, xenial, bionic, jammy, noble, plucky, questing` | Comma-separated release codenames                      |
| `MANPAGES_REPOS`           | `main, restricted, universe, multiverse`                 | Ubuntu repos to scan                                   |
| `MANPAGES_ARCH`            | `amd64`                                                  | Architecture to fetch packages for                     |
| `MANPAGES_ADDR`            | `:8080`                                                  | HTTP bind address (server only)                        |
| `MANPAGES_LOG_LEVEL`       | `info`                                                   | Log level (debug, info, warn, error)                   |
| `MANPAGES_FORCE`           | `false`                                                  | Force reprocessing of all packages (ignore SHA1 cache) |

### Ingest Pipeline

For each release (processed concurrently):

1. **Fetch** `Packages.gz` index files from the archive (across pockets: base, `-updates`, `-security`), deduplicate by highest version.
2. **Per package**: check SHA1 cache → download `.deb` → extract manpages → for each manpage:
   - Parse the path to determine output location.
   - Handle symlinks and `.so` references.
   - Convert roff → HTML using `mandoc`.
   - Run 8-stage HTML transform pipeline (rewrite links, extract title, structure headings, generate TOC, inject metadata).
   - Write HTML and gzip outputs to the filesystem.
   - Update SHA1 cache so unchanged packages are skipped on the next run.
3. **Generate sitemaps** per release/section.

Failures are non-fatal per manpage — errors are logged and counted. A summary is printed at the end.

### Web Server Routes

| Route                                              | Description                                     |
| -------------------------------------------------- | ----------------------------------------------- |
| `GET /`                                            | Homepage with release grid                      |
| `GET /healthz`                                     | Health check endpoint (`{"status":"ok"}`)       |
| `GET /manpages/{release}/{section}/{page}.html`    | Rendered manpage with TOC, breadcrumbs, JSON-LD |
| `GET /manpages/{release}/{section}/{page}.txt`     | Plain text version (HTML tags stripped)         |
| `GET /manpages/latest/...`                         | Redirects to the latest release                 |
| `GET /manpages/lts/...`                            | Redirects to the latest LTS release             |
| `GET /manpages/{release}/`                         | Directory browse with sections                  |
| `GET /manpages.gz/...`                             | Raw gzipped manpage source files                |
| `GET /api/search?q=&release=&lang=&limit=&offset=` | JSON search API                                 |
| `GET /search`                                      | Server-rendered search page                     |
| `GET /sitemaps/...`                                | XML sitemaps                                    |
| `GET /robots.txt`                                  | Robots file                                     |
| `GET /llms.txt`, `/llms-full.txt`                  | LLM-friendly documentation                      |
| `GET /static/...`                                  | CSS/JS with content-hash ETag                   |

Search uses an in-memory filename index (no database). At startup, `FSSearcher` scans `manpages/{release}/man{1-9}/` directories once and builds an index of lowercased command names, then logs the index size and build duration. Searches match against this index in four tiers: exact (case-insensitive) → prefix → substring (contains) → fuzzy (Damerau-Levenshtein distance). The DL function has a bounded variant (`damerauLevenshteinBounded`) with length pre-filtering and early row termination for fast rejection of dissimilar strings. Fuzzy matching uses an adaptive distance threshold based on query length (≤2 → disabled, 3-4 → max distance 1, ≥5 → max distance 2), plus fuzzy prefix matching for command names ≥3 characters. Fuzzy results are capped at 10 to limit noise. The `Result` struct carries a `MatchType` field (`exact`, `prefix`, `contains`, `fuzzy`) exposed in the JSON API. The search page is server-rendered on initial load (one release, defaulting to the newest), but release tab switching is handled client-side via `search.js` — clicking a tab fetches results from `/api/search` and swaps them into the DOM without a page reload (progressive enhancement: tabs are still regular `<a>` links if JS is unavailable). `pushState` keeps the URL in sync so back/forward navigation works between tabs. Fuzzy results appear in a separate "Similar matches" section. The index can be refreshed with `Rebuild()`. Language-filtered searches fall back to filesystem scanning.

### Template Layouts

The server uses two distinct HTML layouts with shared partials:

- **`base-landing.html`** — Brochure-style layout for the homepage (`/`). Uses `{{ template "header" . }}` for the top nav and no sidebar. The index template fills blocks in this layout.
- **`base.html`** — Documentation layout (`l-docs`) for all other pages (search, browse, manpage, 404). Uses the same shared header and includes a sidebar with navigation and release links.
- **`head.html`** — Shared partial defining `{{ define "head" }}` with the full `<head>` element (meta tags, CSS, favicons). Used by both layouts via `{{ template "head" . }}`.
- **`nav.html`** — Shared partial defining `{{ define "header" }}` with the full `<header>` element (logo, mobile toggles, navigation links, Releases dropdown). Used by both layouts via `{{ template "header" . }}`.
- **`footer.html`** — Shared partial defining `{{ define "footer" }}` with the site footer (project links, copyright, build revision). Used by both layouts.
- **`search-form.html`** — Shared partial defining `{{ define "search-inputs" }}` with the search box form elements (input, reset, submit). Used by `base.html` (docs header) and `index.html` (landing page).

The `base.html` layout provides a `{{ block "scripts" . }}` hook just after `app.js` for page-specific scripts. The search template uses this to load `search.js`.

### Building and Running Locally

```bash
# Build all binaries
go build -o bin/server ./cmd/server
go build -o bin/ingest ./cmd/ingest
go build -o bin/ingest-pkg ./cmd/ingest-pkg

# Run the server (requires manpages to be ingested first)
cp .env.example .env   # edit as needed
./bin/server

# Ingest manpages for all configured releases
./bin/ingest

# Ingest a subset of releases
MANPAGES_RELEASES=noble ./bin/ingest

# Force reprocessing (ignore SHA1 cache)
MANPAGES_FORCE=true ./bin/ingest

# Ingest a single package (for debugging)
./bin/ingest-pkg -release noble -package coreutils

# Run Go tests
go test ./...
```

The `mandoc` system package is required for conversion. Install with `apt install mandoc`.

### Building the OCI Image

```bash
rockcraft pack    # produces ubuntu-manpages_0.1.0_amd64.rock
```

The image ships two binaries (`/usr/bin/server`, `/usr/bin/ingest`) plus `mandoc` and CA certificates.

---

## Juju Charm (Python)

### Architecture

The charm is a Kubernetes sidecar charm using Pebble to manage the Go application container. Key files:

- `src/charm.py` — `ManpagesCharm` class. Observes `pebble-ready`, `config-changed`, `update-status`, and the `update-manpages` action.
- `src/manpages.py` — `Manpages` helper. Builds the Pebble layer (defines `server` and `ingest` services), triggers manpage updates by restarting the `ingest` service, and purges releases removed from config.

### Charm Lifecycle

1. **`pebble-ready`** — Adds the Pebble layer and starts both the `server` and `ingest` services.
2. **`config-changed`** / **`update-manpages` action** — Replans the workload with updated config, restarts `ingest`, purges stale releases.
3. **`update-status`** — Checks if `ingest` is still running; reports `MaintenanceStatus` or `ActiveStatus`.

### Pebble Services

| Service    | Command           | Startup | Behavior                                    |
| ---------- | ----------------- | ------- | ------------------------------------------- |
| `manpages` | `/usr/bin/server` | enabled | Long-running HTTP server                    |
| `ingest`   | `/usr/bin/ingest` | enabled | Runs once then exits (`on-success: ignore`) |

### Configuration

Single config option: `releases` — comma-separated list of Ubuntu codenames (default: `questing, plucky, oracular, noble, jammy`).

### Storage

Two Juju storage volumes mounted into the container:

| Storage       | Mount Point            | Purpose                      |
| ------------- | ---------------------- | ---------------------------- |
| `manpages`    | `/app/www/manpages`    | Generated HTML manpages      |
| `manpages-gz` | `/app/www/manpages.gz` | Gzipped manpage source files |

### Ingress Integration

The charm integrates with `traefik-k8s` via the `ingress` relation (using `traefik_k8s.v2.ingress`). When related, Traefik proxies traffic to the app on port 8080 with prefix stripping.

### Dependencies

- Python: `ops`, `launchpadlib`, `pydantic`, `httplib2`, `jinja2`
- Dev: `ops[testing]`, `coverage`, `pytest`, `ruff`, `jubilant`, `requests`, `ty`

---

## Development Workflow

See [CONTRIBUTING.md](../CONTRIBUTING.md) for development workflow, build instructions, and test commands.

---

## JavaScript Conventions

All JavaScript in `internal/web/static/` uses modern ES6/7 syntax:

- **`const` / `let`** — never `var`. Use `const` by default; `let` only when reassignment is needed.
- **Arrow functions** — use `() => {}` for all callbacks and inner functions. Use `e.currentTarget` instead of `this` in event handlers.
- **Template literals** — use backtick strings with `${}` interpolation instead of string concatenation.
- **Modern iterators** — prefer `for...of`, `.map()`, `.filter()`, `.forEach()` over traditional `for (var i = 0; ...)` loops.
- **IIFE pattern** — use `;(() => { ... })()` for scoped blocks.

---

## Key Design Decisions

- **No database**: Storage is filesystem-based — the generated HTML tree _is_ the data store, with SHA1 files as the package cache. Search uses an in-memory filename index built at startup by scanning the HTML tree; no external search engine or database is needed.
- **Two-service model**: The server runs continuously; the ingestion process runs once and exits. Pebble's `on-success: ignore` prevents the charm from restarting it after completion.
- **mandoc for conversion**: The `mandoc` utility converts roff to HTML. It's installed as a stage package in the rock.
- **8-stage HTML pipeline**: Raw `mandoc` output is transformed through multiple stages to produce web-ready HTML with proper links, TOC, metadata, and structure.
- **Metadata in HTML comments**: Each generated manpage embeds a `<!--META:{...}-->` JSON comment containing title, description, package info, and TOC. The server parses this at serve time for rendering and search enrichment.
- **Launchpad API for versions**: Release codenames are resolved to version numbers at startup via the Launchpad REST API. This enables `latest` and `lts` URL aliases.
- **Two template layouts**: The homepage uses a brochure-style layout (`base-landing.html`) with Vanilla Framework grid classes and no sidebar; all other pages use a documentation layout (`base.html`) with `l-docs` classes and a sidebar.

---

## Common Tasks

| Task                | Command                                                     |
| ------------------- | ----------------------------------------------------------- |
| Run unit tests      | `make unit`                                                 |
| Lint                | `make lint`                                                 |
| Format              | `make format`                                               |
| Build charm         | `charmcraft pack`                                           |
| Build OCI image     | `rockcraft pack`                                            |
| Local server        | `go run ./cmd/server`                                       |
| Ingest all releases | `go run ./cmd/ingest`                                       |
| Ingest one release  | `MANPAGES_RELEASES=noble go run ./cmd/ingest`               |
| Ingest one package  | `go run ./cmd/ingest-pkg -release noble -package coreutils` |
