# locus-go

A Go reimplementation of `a8.locus.LocusMain` (the Scala `locus` project) — the
Accur8 Maven/sbt **artifact repository proxy server**. It serves artifacts out of multiple backing
repositories (HTTP/HTTPS upstreams, S3 buckets, local directories), caches them
on disk, validates and generates checksums, and generates directory listings and
`maven-metadata.xml` on the fly.

It reads the **same `config.hocon`** as the Scala version (no conversion), and
its HTTP responses are **byte-for-byte identical** to the production server for
the artifact-serving endpoints (verified against `locus.accur8.net`).

## What it does

- `GET /repos/<repo>/<path>` — resolve content: local cache → backing repo
  download (with checksum validation), serving the file, a redirect, or
  generated content.
- Generated content: directory `index.html` listings, `maven-metadata.xml`, and
  `.md5` / `.sha1` / `.sha256` checksums.
- `HEAD` (existence check) and `PUT` (upload to S3 or a local repo; multiplexers
  route writes to `repoForWrites`).
- `?action=clearcache` and `?action=debug` query actions.
- Repo types: `multiplexer` (fan-out, first-with-content wins), `url`
  (`http`/`https` proxy, or `s3://`), and `local` (filesystem).
- Auth: HTTP basic-auth users with Read/Write/Admin privileges, plus anonymous
  access granted by source subnet (honouring `X-Forwarded-For` from trusted
  proxy subnets).
- `GET /` and `GET /repos` navigation pages.

## What is intentionally **not** ported

The three `POST /api/*` endpoints from the Scala server are **omitted** by
design (they depend on the JVM `coursier` resolver and the `a8-versions` /
`neodeploy` stack):

- `POST /api/resolveDependencyTree`
- `POST /api/nixBuildDescription`
- `POST /api/javaLauncherInstallerDotNix`

Unknown paths return `404` exactly as the Scala server does for unmatched routes.

## Dev shell

A Nix flake provides the Go toolchain (`frags.go` from nix-pins: go, gopls,
delve, golangci-lint, gomod2nix). With direnv, `cd` into the repo (an `.envrc`
runs `use flake`); otherwise:

```bash
nix develop            # interactive shell with go on PATH
nix develop -c go test ./...
```

## Build & run

```bash
nix develop -c go build -o locus ./cmd/locus

# run against a config (the locus2 production config.hocon lives in
# server-app-configs/.../locus2/config/; point -config at it)
TZ=America/New_York ./locus -config /path/to/config.hocon
```

Flags:

| flag | default | meaning |
|------|---------|---------|
| `-config` | `config/config.hocon` | path to the HOCON config |
| `-app-path` | `locus.server.app` | HOCON object path holding the config |
| `-s3-region` | `us-east-1` | AWS region for `s3://` repos |
| `-debug` | `false` | debug logging |
| `-check` | `false` | load + validate config, print summary, exit |
| `-version` | `false` | print build metadata and exit |

## Deploy

`deploy.sh` builds a stamped `linux/amd64` binary, scp's it to the server, and
restarts the supervisor service. Run it from the dev shell (needs `go` + ssh
access to the target):

```bash
nix develop -c ./deploy.sh              # == ./deploy.sh dev@a8-apps locus2 --port 7001
```

It deploys the binary to `dev@a8-apps:bin/locus` (`/home/dev/bin/locus`) and
restarts `locus2`. The one-time supervisor change (JVM launcher → this Go binary)
is in `deploy/locus2.supervisor.conf`; the live source-of-truth is
`proxmox-hosts/nixgen/a8-apps/supervisor/managed/locus2.conf` (see
`deploy/README.md`).

### Timezone

`maven-metadata.xml`'s `<lastUpdated>` and directory-listing timestamps are
formatted in the **process-local timezone**, exactly like the JVM
(`ZoneId.systemDefault()`). For byte-identical output with production, run with
the same zone, e.g. `TZ=America/New_York ./locus ...`.

## Configuration

Reads the existing `config.hocon` unchanged. Secrets (S3 keys, user passwords)
live only in that file — nothing is hardcoded. A small dependency-free HOCON
parser handles the subset used by these configs (nested objects, arrays,
quoted/unquoted scalars, dotted keys, `//`/`#` comments). See
`example-config.hocon` for the shape (placeholder credentials).

## Architecture

```
cmd/locus            entrypoint (LocusMain)
internal/
  config             LocusConfig + HOCON load + SubnetManager
  hocon              dependency-free HOCON-subset parser
  uri                URI model (http/https/s3, path join, trailing-slash)
  cpath              ContentPath (scrubbed, traversal-safe)
  javatime           Java LocalDateTime.toString + maven lastUpdated formatting
  mavenversion       a8-versions ParsedVersion grammar + ordering
  checksum           md5/sha1/sha256 digest, validate, scrub
  model              DirectoryEntry, RepoContent, ResolvedRepo interface, RepoLog
  upstream           HTTP client, S3 client, maven index.html scraper
  repo               resolution core + backends (mux/http/s3/local) + generators
  server             router, handlers, basic-auth/anonymous, responses
```

## Tests & verification

```bash
nix-shell -p go --run 'go test ./...'
```

Unit tests cover the HOCON parser (incl. the real prod config), the version
grammar/ordering, the maven index scraper, time formatting (incl. the faithful
`lastUpdated` minute-as-hour quirk), and checksums. The server output was
additionally diffed byte-for-byte against `locus.accur8.net` for `index.html`,
`maven-metadata.xml`, checksums, response headers/content-types, and 404s across
HTTP, S3, and multiplexer repos.
