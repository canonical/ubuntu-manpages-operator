# Codex repository instructions

## Scope

These instructions apply to the entire repository. This repository contains two related
deliverables:

- a Go application that ingests Ubuntu packages, converts their manpages to HTML, and serves the
  generated site;
- a Python Juju charm that deploys the application and manages its server and ingest processes
  with Pebble.

Keep changes focused on the requested task. Preserve unrelated work in the tree and do not edit
generated build artifacts.

## Repository map

- `cmd/server`, `cmd/ingest`, `cmd/ingest-pkg`: Go binary entry points.
- `internal/`: Go application packages. Tests live beside the code as `*_test.go`.
- `internal/web/templates/`: Go HTML templates.
- `internal/web/static/`: browser CSS and JavaScript.
- `src/`: Python charm implementation.
- `tests/unit/`: charm unit tests.
- `tests/integration/`: tests that require a real Juju model; run them through Spread as documented
  in `CONTRIBUTING.md`.
- `tests/spread/`: Spread orchestration and fixtures.
- `lib/charms/`: vendored charm libraries; avoid editing them as part of ordinary feature work.
- `charmcraft.yaml`, `rockcraft.yaml`, `spread.yaml`: packaging and integration-test definitions.

Use `README.md` and `CONTRIBUTING.md` for user-facing behavior and detailed development setup.
Use `.env.example` as the source of truth for Go application environment variables.

## Design constraints

- The generated HTML tree is the application's data store. Do not introduce a database or external
  search service without an explicit architecture change.
- `server` is long-running; `ingest` runs to completion. The Pebble layer relies on
  `on-success: ignore` for the ingest service.
- Keep configuration environment-driven for `server` and `ingest`. `ingest-pkg` may use its
  existing command-line flags for selecting a release and package.
- Manpage conversion depends on `mandoc`; tests that exercise conversion may require it.
- Preserve the two web layouts: `base-landing.html` for the home page and `base.html` for
  documentation pages. Shared page elements belong in the existing partial templates.
- Treat files such as `.charm`, `.rock`, coverage output, caches, and `bin/` contents as generated
  artifacts, not source.

## Making changes

1. Read the affected implementation and its nearby tests before editing.
2. Add or update tests for behavior changes. Prefer the narrowest test that demonstrates the new
   behavior or regression.
3. Keep documentation synchronized when changing commands, environment variables, charm options,
   routes, or other user-visible behavior.
4. Run targeted checks while iterating, then the relevant broader checks before handing off.
5. Report which checks ran and any that could not run. Suggest a Conventional Commits-style commit
   message in the final handoff.

Do not paper over failures by weakening assertions, skipping tests, or broadly suppressing linters.

## Verification commands

Choose checks in proportion to the files changed:

```bash
# Targeted Go package tests
go test ./internal/<package>

# All Go tests
go test ./...

# Charm unit tests (also runs Go tests and reports Python coverage)
make unit

# Python-only unit test iteration
uv run --all-extras pytest tests/unit -v --tb native

# Go vet plus Python lint, format check, and type check
make lint

# Apply Go and Python formatting/lint fixes
make format
```

Run `make format` only when source formatting is relevant, and review its diff because it can touch
multiple Go and Python files. Integration tests require additional infrastructure; use the Spread
commands in `CONTRIBUTING.md` when the change warrants them. Packaging checks (`charmcraft pack` or
`rockcraft pack`) are appropriate for packaging changes but are not a substitute for tests.

## Language and area conventions

### Go

- Use standard Go style and run `gofmt` (normally through `make format`) on changed Go files.
- Keep package boundaries under `internal/`; put executable wiring in `cmd/` rather than business
  logic.
- Wrap errors with useful context and preserve causes with `%w` when callers may inspect them.
- Use the repository's structured `slog` logging rather than ad hoc printing in application code.
- Prefer table-driven tests where several cases share the same behavior.

### Python charm

- Follow the Ruff and ty configuration in `pyproject.toml` (Python 3.12+, 99-character line length).
- Keep event handling and charm wiring in `src/charm.py`; keep Pebble/service behavior in
  `src/manpages.py` unless a clearer abstraction is needed.
- Use `ops.testing` for isolated charm behavior. Reserve `tests/integration/` for behavior that
  genuinely needs Juju or related applications.

### Templates and browser code

- Preserve automatic escaping in Go templates; do not introduce trusted HTML bypasses for
  user-controlled or archive-derived content.
- Maintain progressive enhancement: navigation and search should retain useful non-JavaScript
  behavior where the existing markup supports it.
- In JavaScript, use `const` by default, `let` only for reassignment, arrow functions for callbacks,
  template literals for interpolation, and the existing `;(() => { ... })()` scoped-module style.
- When changing a static asset, verify its serving and cache/ETag behavior in the web package tests.

