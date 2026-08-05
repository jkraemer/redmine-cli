#!/usr/bin/env bash
# Run the live e2e suite against a containerized Redmine.
# Usage: scripts/live/run.sh <redmine-version>   (e.g. 6.1)
set -euo pipefail

VERSION="${1:?usage: run.sh <redmine-version>}"
RUNTIME="${RUNTIME:-$(command -v docker || command -v podman || true)}"
if [ -z "${RUNTIME:-}" ]; then
  echo "error: neither docker nor podman found on PATH" >&2
  exit 1
fi
PORT="${PORT:-3000}"
NAME="redmine-e2e-${VERSION//./-}"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

cleanup() {
  if [ -z "${KEEP:-}" ]; then
    "$RUNTIME" rm -f "$NAME" >/dev/null 2>&1 || true
  else
    echo "KEEP set — container $NAME left running on port $PORT"
  fi
}
trap cleanup EXIT

"$RUNTIME" rm -f "$NAME" >/dev/null 2>&1 || true
"$RUNTIME" run -d --name "$NAME" -p "$PORT:3000" "docker.io/library/redmine:$VERSION" >/dev/null
echo "waiting for redmine:$VERSION on port $PORT (first boot runs migrations)..."
for i in $(seq 1 90); do
  if curl -fsS "http://127.0.0.1:$PORT/" >/dev/null 2>&1; then
    break
  fi
  if [ "$i" = 90 ]; then
    echo "redmine did not become ready; container logs:" >&2
    "$RUNTIME" logs --tail 50 "$NAME" >&2
    exit 1
  fi
  sleep 2
done

"$RUNTIME" cp "$REPO_ROOT/scripts/live/bootstrap.rb" "$NAME:/bootstrap.rb"
BOOT_OUT="$("$RUNTIME" exec "$NAME" bin/rails runner /bootstrap.rb)"
API_KEY="$(printf '%s\n' "$BOOT_OUT" | sed -n 's/^e2e_token=//p' | tail -1)"
PROJECT="$(printf '%s\n' "$BOOT_OUT" | sed -n 's/^e2e_project=//p' | tail -1)"
if [ -z "$API_KEY" ] || [ -z "$PROJECT" ]; then
  echo "bootstrap failed; output was:" >&2
  echo "$BOOT_OUT" >&2
  exit 1
fi

echo "bootstrap ok (project=$PROJECT); running live e2e suite"
cd "$REPO_ROOT"
REDMINE_URL="http://127.0.0.1:$PORT" \
REDMINE_API_KEY="$API_KEY" \
E2E_PROJECT="$PROJECT" \
  go test -tags live ./e2e -v -count=1
