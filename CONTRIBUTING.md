# Contributing

This documents explains the processes and practices recommended for contributing enhancements to
this operator.

- Generally, before developing enhancements to this charm, you should consider [opening an issue
  ](https://github.com/canonical/ubuntu-manpages-operator/issues) explaining your use case.
- If you would like to chat with us about your use-cases or proposed implementation, you can reach
  us [on Matrix](https://ubuntu.com/community/communications/matrix) or [Discourse](https://discourse.charmhub.io/).
- Familiarising yourself with the [Operator Framework](https://ops.readthedocs.io/en/latest/) library
  will help you a lot when working on new features or bug fixes.
- All enhancements require review before being merged. Code review typically examines
  - code quality
  - test coverage
  - user experience for Juju administrators this charm.
- Please help us out in ensuring easy to review branches by rebasing your pull request branch onto
  the `main` branch. This also avoids merge commits and creates a linear Git commit history.

## Before submitting

Before opening a pull request, please ensure:

1. **Format and lint**: `make format; make lint`
2. **Go tests pass**: `go test ./...`
3. **Charm unit tests pass**: `make unit`
4. **Test coverage** is updated if you've added or changed functionality
5. **`README.md` is updated** if the change adds, removes, or renames CLI commands, environment variables, or user-facing features
6. Use [Conventional Commits](https://www.conventionalcommits.org/) format for commit messages

## Go development

### Prerequisites

- Go 1.24+ (`go version`)
- `mandoc` — install with `apt install mandoc`

### Building

```bash
go build -o bin/server ./cmd/server
go build -o bin/ingest ./cmd/ingest
go build -o bin/ingest-pkg ./cmd/ingest-pkg
```

### Running locally

```bash
cp .env.example .env   # edit as needed
./bin/server            # requires manpages to be ingested first
```

### Ingesting manpages

```bash
# Ingest all configured releases
./bin/ingest

# Ingest a single release
MANPAGES_RELEASES=noble ./bin/ingest

# Force reprocessing (ignore SHA1 cache)
MANPAGES_FORCE=true ./bin/ingest

# Ingest a single package (for debugging)
./bin/ingest-pkg -release noble -package coreutils
```

### Tests

```bash
go test ./...
```

### Building the OCI image

```bash
rockcraft pack
```

## Charm development (Python)

This project uses [`uv`](https://github.com/astral-sh/uv) for managing dependencies and virtual
environments.

You can create a virtual environment manually should you wish, though most of that is taken
care of automatically if you use the `Makefile` provided:

```bash
❯ make format        # update your code according to linting rules
❯ make lint          # code style
❯ make unit          # run unit tests

# The following exist, but should not be run directly without `spread`.
# See 'Running Tests' below.
❯ make integration   # run integration tests (run with `spread`)
```

To create the environment manually:

```bash
❯ uv venv
❯ source .venv/bin/activate
❯ uv sync --all-extras
```

## Workshop (alternative dev environment)

[Workshop](https://ubuntu.com/workshop/) provides a reproducible LXD-backed
dev environment for this repo. Both the Go app and the Python charm build inside
one workshop, with no need for `mandoc` on the host.

```bash
sudo snap install workshop --classic
workshop launch                       # first run only; takes a few minutes
workshop run lint                     # or: format, unit, go-test, pytest, static-check, build
workshop run serve                    # run cmd/server; site at http://localhost:8080
workshop run ingest-pkg -- -release noble -package coreutils  # single-package debug
workshop shell                        # interactive shell inside the workshop
```

Charm packing (`charmcraft pack`), rock building (`rockcraft pack`), and the
jubilant integration tests are deliberately not wired up as actions — they need
nested LXD or a real Juju model. Run those on the host as usual.

### Sharing build caches with the host (optional)

By default the Go module cache and uv cache live inside the workshop, isolated
from the host's `~/go/pkg/mod` and `~/.cache/uv`. To share them (useful if you
also build on the host, or run multiple workshops) bind the SDK mount plugs to
host paths once per workshop:

```bash
workshop remount dev/go:mod-cache ~/go/pkg/mod
workshop remount dev/uv:cache     ~/.cache/uv
```

These connections persist across `workshop refresh` and `workshop stop`/`start`.

## Running tests

Unit tests can be run locally with no additional tools by running `make unit`. All of the project's unit tests are designed to run agnostic of machine and network, and shouldn't require any additional dependencies other than those injected by `uv run` and the `Make` target.

Integration tests also use `spread`. Currently, there are two supported backends -
tests can either be run in LXD virtual machines, or on a pre-provisioned server (such as a Github
Actions runner or development VM).

To show the available integration tests, you can:

```bash
❯ spread -list lxd:
```

From there, you can either run all of the tests, or a selection:

```bash
# Run all of the tests
❯ spread -v lxd:

# Run a particular test
❯ spread -v lxd:ubuntu-24.04:tests/spread/integration/deploy-charm:juju_3_6
```

To run any of the tests on a locally provisioned machine, use the `github-ci` backend, e.g.

```bash
# List available tests
❯ spread -list github-ci:

# Run all of the tests
❯ spread -v github-ci:

# Run a particular test
❯ spread -v github-ci:ubuntu-24.04:tests/spread/integration/deploy-charm:juju_3_6
```

### Troubleshooting Failing Tests

If you're working on a test that's failing, you can use `spread` to get an interactive shell inside the test environment on failure, which allows for easier debugging. To enable this, add `--debug` to your `spread` commands. For example:

```bash
❯ spread -v --debug lxd:ubuntu-24.04:tests/spread/integration/
```

If the above tests were to fail, you'd be dropped into an interactive shell before the machine is reaped.

## Building artefacts

Build the charm:

```bash
charmcraft pack
```

Build the OCI image:

```bash
rockcraft pack
```

### Deploy

```bash
# Create a model
❯ juju add-model dev

# Enable DEBUG logging
❯ juju model-config logging-config="<root>=INFO;unit=DEBUG"

# Deploy the charm
❯ juju deploy ./ubuntu-manpages_amd64.charm
```
