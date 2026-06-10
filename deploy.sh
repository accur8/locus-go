#!/usr/bin/env bash
#
# deploy.sh — build a stamped linux/amd64 locus-go binary and deploy it to a
# server, then restart its supervisor service. Modeled on godev's bin/dev-deploy.
#
# Run this from inside the dev shell (so `go` is on PATH):
#   nix develop -c ./deploy.sh
# or with go otherwise available:
#   ./deploy.sh
#
# Usage:
#   ./deploy.sh [ssh-target] [supervisor-service] [--port N] [--remote-bin P] [--no-restart]
#
# Defaults target the locus2 service on a8-apps:
#   ./deploy.sh                      # == ./deploy.sh dev@a8-apps locus2
#   ./deploy.sh dev@a8-apps locus2 --port 7001
#
# It scp's the binary to <remote-bin> (default bin/locus -> /home/dev/bin/locus),
# staging+mv into place (mv works even if the running binary is busy), restarts
# the supervisor service, and probes http://localhost:<port>/ on the server.
#
# NOTE: the supervisor program must already invoke the deployed binary. See
#   deploy/locus2.supervisor.conf for the one-time config change (java -> Go).

set -euo pipefail
cd "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

SSH_TARGET="dev@a8-apps"
SERVICE="locus2"
PORT=7001
REMOTE_BIN="bin/locus"   # relative to the remote $HOME (a8-apps: /home/dev/bin/locus)
RESTART=1

if [[ $# -gt 0 && "$1" != --* ]]; then SSH_TARGET="$1"; shift; fi
if [[ $# -gt 0 && "$1" != --* ]]; then SERVICE="$1"; shift; fi
while [[ $# -gt 0 ]]; do
  case "$1" in
    --port) PORT="$2"; shift 2 ;;
    --remote-bin) REMOTE_BIN="$2"; shift 2 ;;
    --no-restart) RESTART=0; shift ;;
    *) echo "unknown option: $1" >&2; exit 1 ;;
  esac
done

# --- build metadata (injected via ldflags into internal/buildinfo) ---
BASE_VERSION="$(cat VERSION 2>/dev/null || echo 0.0.0)"
GIT_COMMIT="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
GIT_BRANCH="$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)"
SHORT_SHA="$(git rev-parse --short HEAD 2>/dev/null || echo nogit)"
if git diff --quiet 2>/dev/null && git diff --cached --quiet 2>/dev/null; then
  GIT_DIRTY=false
else
  GIT_DIRTY=true
fi
TS="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
VERSION_TS="$(date -u +%Y%m%d_%H%M)"
GO_VERSION="$(go version | awk '{print $3}')"
VERSION="${BASE_VERSION}-${VERSION_TS}_${GIT_BRANCH}-${SHORT_SHA}"
[[ "$GIT_DIRTY" == "true" ]] && VERSION="${VERSION}-dirty"

BI=github.com/accur8/locus-go/internal/buildinfo
LDFLAGS="-s -w \
  -X ${BI}.Version=${VERSION} \
  -X ${BI}.GitCommit=${GIT_COMMIT} \
  -X ${BI}.GitBranch=${GIT_BRANCH} \
  -X ${BI}.GitDirty=${GIT_DIRTY} \
  -X ${BI}.BuildTimestamp=${TS} \
  -X ${BI}.BuildUser=$(whoami) \
  -X ${BI}.BuildMachine=$(hostname) \
  -X ${BI}.GoVersion=${GO_VERSION}"

OUT="dist/locus-linux-amd64"
mkdir -p dist

echo "==> Building stamped linux/amd64 locus-go"
echo "    version: ${VERSION}  (commit ${SHORT_SHA}, dirty=${GIT_DIRTY})"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "${LDFLAGS}" -o "${OUT}" ./cmd/locus
echo "    output:  ${OUT} ($(du -h "${OUT}" | cut -f1))"

echo "==> Deploying to ${SSH_TARGET}:${REMOTE_BIN}"
scp -q "${OUT}" "${SSH_TARGET}:${REMOTE_BIN}.staged"
ssh "${SSH_TARGET}" "mkdir -p \"\$(dirname '${REMOTE_BIN}')\" && mv '${REMOTE_BIN}.staged' '${REMOTE_BIN}' && chmod +x '${REMOTE_BIN}'"
echo "    deployed"

if [[ "$RESTART" == "1" ]]; then
  echo "==> Restarting supervisor service '${SERVICE}'"
  ssh "${SSH_TARGET}" "supervisorctl restart '${SERVICE}'"
  ssh "${SSH_TARGET}" "supervisorctl status '${SERVICE}'" || true
else
  echo "==> --no-restart: not restarting ${SERVICE}"
fi

echo "==> Probing http://localhost:${PORT}/ on ${SSH_TARGET}"
if ssh "${SSH_TARGET}" "curl -fsS -o /dev/null -w 'HTTP %{http_code}\n' http://localhost:${PORT}/" 2>/dev/null; then
  echo "==> Done. Deployed version ${VERSION}."
else
  echo "WARN: could not reach http://localhost:${PORT}/ (service starting, wrong port, or supervisor still points at java?)." >&2
fi
