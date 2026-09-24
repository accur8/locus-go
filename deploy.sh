#!/usr/bin/env bash
#
# deploy.sh — build a stamped linux/amd64 locus-go binary and deploy it to a
# server, then restart the systemd unit that runs it. Modeled on godev's
# bin/dev-deploy.
#
# Run this from inside the dev shell (so `go` is on PATH):
#   nix develop -c ./deploy.sh
# or with go otherwise available:
#   ./deploy.sh
#
# Usage:
#   ./deploy.sh [ssh-target] [job-uid] [--port N] [--remote-bin P] [--env E] [--no-restart]
#
# Defaults target locus2 on a8-apps, which runs as a continuum daemon job of
# the PROD job-runner (job dmnLocus2000000000000001, unit
# a8-jobrun-dmnLocus2000000000000001.service, User=dev):
#   ./deploy.sh                      # == ./deploy.sh dev@a8-apps dmnLocus2000000000000001
#   ./deploy.sh dev@a8-apps dmnLocus2000000000000001 --port 7001 --env continuum-prod
#
# It scp's the binary to <remote-bin> (default bin/locus -> /home/dev/bin/locus),
# staging+mv into place (mv works even if the running binary is busy), restarts
# the daemon job THROUGH ITS JOB-RUNNER (jobrunner.v1.RestartJob at the runner
# that lists the job; the unit is the runner's transient creation, so a
# `systemctl restart` on it stops the daemon and the definition vanishes with
# it), and probes http://localhost:<port>/ on the server. Needs `a8` on PATH
# with credentials for --env. Job control by domain name is
# FEATURE-20260924-job-control-by-domain-name; until then the runner is found
# by asking every registered one.

set -euo pipefail
cd "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

SSH_TARGET="dev@a8-apps"
SERVICE="dmnLocus2000000000000001"   # the daemon job uid
ENV="continuum-prod"
PORT=7001
REMOTE_BIN="bin/locus"   # relative to the remote $HOME (a8-apps: /home/dev/bin/locus)
RESTART=1

if [[ $# -gt 0 && "$1" != --* ]]; then SSH_TARGET="$1"; shift; fi
if [[ $# -gt 0 && "$1" != --* ]]; then SERVICE="$1"; shift; fi
while [[ $# -gt 0 ]]; do
  case "$1" in
    --port) PORT="$2"; shift 2 ;;
    --remote-bin) REMOTE_BIN="$2"; shift 2 ;;
    --env) ENV="$2"; shift 2 ;;
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
  echo "==> Restarting daemon job '${SERVICE}' through its job-runner (${ENV})"
  RUNNER=""
  for mb in $(a8 --env "${ENV}" registry list 2>/dev/null | grep -A5 '^\[[0-9]*\] job-runner' | awk '/Mailbox:/ {print $2}'); do
    if a8 --env "${ENV}" rpc request --mailbox "${mb}" --endpoint jobrunner.v1.ListJobs --body '{}' --compact 2>/dev/null | grep -q "\"jobUid\":\"${SERVICE}\""; then
      RUNNER="${mb}"; break
    fi
  done
  if [[ -z "${RUNNER}" ]]; then
    echo "ERROR: no registered ${ENV} job-runner lists job ${SERVICE}; the binary is deployed but NOT restarted." >&2
    exit 1
  fi
  echo "    runner mailbox: ${RUNNER}"
  a8 --env "${ENV}" rpc request --mailbox "${RUNNER}" --endpoint jobrunner.v1.RestartJob --body "{\"job_uid\":\"${SERVICE}\",\"graceful\":true}" --compact 2>/dev/null
  echo
  sleep 4
else
  echo "==> --no-restart: not restarting ${SERVICE}"
fi

echo "==> Probing http://localhost:${PORT}/ on ${SSH_TARGET}"
if ssh "${SSH_TARGET}" "curl -fsS -o /dev/null -w 'HTTP %{http_code}\n' http://localhost:${PORT}/" 2>/dev/null; then
  echo "==> Done. Deployed version ${VERSION}."
else
  echo "WARN: could not reach http://localhost:${PORT}/ (job still relaunching, or wrong port?)." >&2
fi
