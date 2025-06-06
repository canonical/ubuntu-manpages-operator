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

## Developing

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
❯ make functional    # run functional tests (run with `spread`)
❯ make integration   # run integration tests (run with `spread`)
```

To create the environment manually:

```bash
❯ uv venv
❯ source .venv/bin/activate
❯ uv sync --all-extras
```

## Running tests

Unit tests can be run locally with no additional tools by running `make unit`. All of the project's unit tests are designed to run agnostic of machine and network, and shouldn't require any additional dependencies other than those injected by `uv run` and the `Make` target.

The project's [functional tests](./tests/functional) are designed to be run in an ephemeral environment. They require an internet connection, and will mutate the system they run on. This is taken care of automatically by using [`spread`](https://github.com/canonical/spread), which you can install like so:

```bash
❯ go install github.com/canonical/spread/cmd/spread@latest
```

Once set up, you can run the functional tests with:

```bash
❯ charmcraft test -v lxd:ubuntu-24.04:tests/spread/functional/
```

Integration tests also use `spread`. Currently, there are two supported backends -
tests can either be run in LXD virtual machines, or on a pre-provisioned server (such as a Github
Actions runner or development VM).

To show the available integration tests, you can:

```bash
❯ charmcraft test --list lxd:
lxd:ubuntu-24.04:tests/spread/functional/launchpad
lxd:ubuntu-24.04:tests/spread/functional/manpages
lxd:ubuntu-24.04:tests/spread/integration/deploy-charm:juju_3_6
```

From there, you can either run all of the tests, or a selection:

```bash
# Run all of the tests
❯ charmcraft test -v lxd:

# Run a particular test
❯ charmcraft test -v lxd:ubuntu-24.04:tests/spread/integration/deploy-charm:juju_3_6
```

To run any of the tests on a locally provisioned machine, use the `github-ci` backend, e.g.

```bash
# List available tests
❯ charmcraft test --list github-ci:

# Run all of the tests
❯ charmcraft test -v github-ci:

# Run a particular test
❯ charmcraft test -v github-ci:ubuntu-24.04:tests/spread/integration/deploy-charm:juju_3_6
```

### Troubleshooting Failing Tests

If you're working on a test that's failing, you can use `spread` to get an interactive shell inside the test environment on failure, which allows for easier debugging. To enable this, add `--debug` to your `charmcraft test` commands. For example:

```bash
❯ charmcraft test -v --debug lxd:ubuntu-24.04:tests/spread/functional/
```

If the above tests were to fail, you'd be dropped into an interactive shell before the machine is reaped.

## Build charm

Build the charm in this git repository using:

```bash
charmcraft pack
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
