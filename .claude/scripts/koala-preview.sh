#!/usr/bin/env bash
# Deploy Koala to blink, then SSH-tunnel the live UI back to localhost:9081
# so Claude Preview can inspect it at http://localhost:9081
#
# A placeholder HTTP server holds port 9081 open immediately so the preview
# tool doesn't time out during the Docker build (which takes several minutes).

set -euo pipefail

PORT="${BLINK_KOALA_LIVE_UI_PORT:-9081}"
SSH_HOST="${BLINK_SSH_HOST:-blink}"

cleanup() {
  [ -n "${PLACEHOLDER_PID:-}" ] && kill "$PLACEHOLDER_PID" 2>/dev/null || true
}
trap cleanup EXIT

# Hold port open immediately so the preview tool doesn't time out
python3 -m http.server "$PORT" --directory /tmp &>/dev/null &
PLACEHOLDER_PID=$!
echo "==> Placeholder server holding port ${PORT} (PID: ${PLACEHOLDER_PID})"

echo "==> Deploying Koala to ${SSH_HOST}..."
/usr/local/bin/blink deploy koala --local

# Hand off: kill placeholder, open the SSH tunnel
kill "$PLACEHOLDER_PID" 2>/dev/null || true
PLACEHOLDER_PID=""

echo "==> Opening SSH tunnel: localhost:${PORT} -> ${SSH_HOST}:${PORT}"
echo "==> Live UI available at http://localhost:${PORT}"
exec ssh -N -L "${PORT}:localhost:${PORT}" "${SSH_HOST}"
