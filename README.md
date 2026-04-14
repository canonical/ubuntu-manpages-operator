# Ubuntu Manpages Operator

<a href="https://charmhub.io/ubuntu-manpages"><img alt="" src="https://charmhub.io/ubuntu-manpages/badge.svg" /></a>
<a href="https://github.com/canonical/ubuntu-manpages-operator/actions/workflows/release.yaml"><img src="https://github.com/canonical/ubuntu-manpages-operator/actions/workflows/release.yaml/badge.svg"></a>

**Ubuntu Manpages Operator** is a [charm](https://juju.is/charms-architecture) for deploying [https://manpages.ubuntu.com](https://manpages.ubuntu.com), a site which contains thousands of dynamically generated manuals, extracted from every supported version of Ubuntu and updated on a regular basis.

This repository contains both the [application code](./cmd) and the code for the [charm](./src).

## Go application

The Go application downloads Ubuntu `.deb` packages, extracts manpages, converts them to HTML, and serves them via HTTP. It is composed of three binaries:

| Binary           | Purpose                                                                                       |
| ---------------- | --------------------------------------------------------------------------------------------- |
| `cmd/server`     | HTTP server — serves manpages, search, sitemaps, browse, health checks                        |
| `cmd/ingest`     | Bulk ingestion — fetches all packages for configured releases, converts manpages, writes HTML |
| `cmd/ingest-pkg` | Single-package ingestion — for development/debugging a specific package                       |

The `server` and `ingest` binaries have no CLI flags — all configuration comes from environment variables. The `ingest-pkg` binary accepts two required CLI flags (`-release` and `-package`) to select a single package for debugging, with remaining configuration from the environment.

### Configuration

All three binaries read configuration from environment variables, optionally loading a `.env` file from the working directory. See [`.env.example`](.env.example) for a template.

| Variable                   | Default                                                  | Purpose                                                |
| -------------------------- | -------------------------------------------------------- | ------------------------------------------------------ |
| `MANPAGES_SITE`            | `https://manpages.ubuntu.com`                            | Public-facing site URL (used in sitemaps, links)       |
| `MANPAGES_ARCHIVE`         | `https://archive.ubuntu.com/ubuntu`                      | Ubuntu package archive URL                             |
| `MANPAGES_PUBLIC_HTML_DIR` | `/app/www`                                               | Root output directory for HTML and gzip files          |
| `MANPAGES_RELEASES`        | `trusty, xenial, bionic, jammy, noble, plucky, questing` | Comma-separated release codenames                      |
| `MANPAGES_REPOS`           | `main, restricted, universe, multiverse`                 | Ubuntu repos to scan                                   |
| `MANPAGES_ARCH`            | `amd64`                                                  | Architecture to fetch packages for                     |
| `MANPAGES_ADDR`            | `:8080`                                                  | HTTP bind address (server only)                        |
| `MANPAGES_ADMIN_ADDR`      | `127.0.0.1:9090`                                         | Admin listener address for internal endpoints; must be loopback-only (server only) |
| `MANPAGES_LOG_LEVEL`       | `info`                                                   | Log level (debug, info, warn, error)                   |
| `MANPAGES_FORCE`           | `false`                                                  | Force reprocessing of all packages (ignore SHA1 cache) |

### Ingest pipeline

For each configured release (processed concurrently), the ingest binary fetches `Packages.gz` index files from the Ubuntu archive, deduplicates packages by highest version, and downloads each `.deb` that has changed since the last run (based on a SHA1 cache). Manpages are extracted from each package, converted from roff to HTML using `mandoc`, and run through an 8-stage HTML transform pipeline that rewrites links, extracts titles, generates a table of contents, and injects metadata. Finally, sitemaps are generated per release and section.

### Web server

The server binary serves the generated HTML manpages along with search, browse, sitemaps, health checks, and static assets. It supports virtual release aliases (`latest`, `lts`) resolved via the Launchpad API, as well as plain-text and gzipped manpage variants. See the [routes table](.github/copilot-instructions.md#web-server-routes) for the full list of endpoints.

### Key design decisions

- **No database** — Both storage and search are filesystem-based. The generated HTML tree _is_ the data store, with SHA1 files as the package cache.
- **Fuzzy search** — Search matches in four tiers: exact, prefix, substring (contains), and fuzzy (Damerau-Levenshtein distance). Fuzzy results are shown in a separate "Similar matches" section so typos like `grpe` still find `grep`. The JSON API exposes the `match_type` field on each result.
- **mandoc for conversion** — The `mandoc` utility converts roff to HTML. It is installed as a stage package in the OCI image.
- **Metadata in HTML comments** — Each generated manpage embeds a `<!--META:{...}-->` JSON comment containing title, description, package info, and TOC. The server parses this at serve time for rendering and search enrichment.

## Deploying with Juju

Assuming you have access to a bootstrapped [Juju](https://juju.is) controller, you can deploy the charm with:

```bash
❯ juju deploy ubuntu-manpages
```

Once the charm is deployed, you can check the status with Juju status:

```bash
❯ juju status
Model     Controller     Cloud/Region  Version  SLA          Timestamp
manpages  concierge-k8s  k8s           3.6.14   unsupported  19:05:33Z

App              Version  Status       Scale  Charm            Channel  Rev  Address        Exposed  Message
ubuntu-manpages           maintenance      1  ubuntu-manpages             1  10.152.183.84  no       Updating manpages

Unit                Workload     Agent  Address     Ports  Message
ubuntu-manpages/0*  maintenance  idle   10.1.0.163         Updating manpages
```

You can see from the status that the application has been assigned an IP address, and is listening on port 8080. Using the example above, browsing to `http://10.245.163.53:8080` would display the homepage for the application.

On first start up, the charm will install the application, ensuring that any packages and configuration files are in place, and will begin downloading and processing manpages for the configured releases.

The charm accepts only one configuration option: `releases`, which is a comma-separated list of Ubuntu releases to include in the manpages (default: `questing, plucky, oracular, noble, jammy`). For example, to adjust the list:

```bash
❯ juju config ubuntu-manpages releases="questing, plucky, oracular, noble, jammy"
```

When a new configuration is applied, the charm will automatically update the manpages to include the new releases, and purge any releases that are present on disk from a previous configuration, but no longer specified.

To update the manpages, you can use the provided Juju [Action](https://documentation.ubuntu.com/juju/3.6/howto/manage-actions/):

```bash
❯ juju run ubuntu-manpages/0 update-manpages
```

### Integrating with an ingress / proxy

The charm supports integrations with ingress/proxy services using the `ingress` relation. To test this:

```bash
# Deploy the charms
❯ juju deploy ubuntu-manpages
❯ juju deploy traefik-k8s --trust --config external-hostname=manpages.internal

# Create integrations
❯ juju integrate ubuntu-manpages traefik-k8s

# Test the proxy integration
❯ curl -k -H "Host: manpages.internal" https://<traefik-ip>/<model-name>-ubuntu-manpages
```

The scenario described above is demonstrated [in the integration tests](./tests/integration/test_ingress.py).

### Deployment requirements

As of 2025-07-30, the deployment requirements have been observed to be the following:

- Configured releases: Jammy, Noble, Plucky, Questing
- Disk space used in the `/app/www` folder: `9.4GiB`
- Size of a single release in that folder: `~1.7GiB` (HTML) + `~750MiB` (.gz manpages)
- Stats from the systemd service, on a 4 cores VM: `update-manpages.service: Consumed 1d 2h 50min 47.755s CPU time, 4.7G memory peak, 0B memory swap peak.`

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for build instructions, development workflows, and testing procedures for both the Go application and the Python charm.

## Contribute to Ubuntu Manpages Operator

Ubuntu Manpages Operator is open source and part of the Canonical family. We would love your help.

If you're interested, start with the [contribution guide](CONTRIBUTING.md).

## License and copyright

Ubuntu Manpages Operator is released under the [GPL-3.0 license](LICENSE).
