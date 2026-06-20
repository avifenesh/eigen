#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTANCE="${EIGEN_INSTANCE:-dev}"
LOG="${TMPDIR:-/tmp}/eigen-gui-smoke-$$.log"
PID=""
cleanup() {
  if [[ -n "${PID}" ]]; then
    # The compiled child produced by `go run` can outlive the go wrapper. Start
    # it in its own process group and terminate the whole group on exit.
    kill -TERM "-${PID}" 2>/dev/null || true
    pkill -TERM -P "${PID}" 2>/dev/null || true
    kill "${PID}" 2>/dev/null || true
    wait "${PID}" 2>/dev/null || true
  fi
  rm -f "${LOG}"
}
trap cleanup EXIT

cd "${ROOT}"
setsid go run . --instance "${INSTANCE}" gui --no-open --addr 127.0.0.1:0 >"${LOG}" 2>&1 &
PID=$!

URL=""
for _ in {1..100}; do
  if ! kill -0 "${PID}" 2>/dev/null; then
    cat "${LOG}" >&2 || true
    echo "gui smoke: server exited before printing URL" >&2
    exit 1
  fi
  URL="$(sed -n 's/^eigen gui: //p' "${LOG}" | tail -1)"
  if [[ -n "${URL}" ]]; then
    break
  fi
  sleep 0.1
done

if [[ -z "${URL}" ]]; then
  cat "${LOG}" >&2 || true
  echo "gui smoke: timed out waiting for server URL" >&2
  exit 1
fi

python3 - "${URL}" <<'PY'
import json
import sys
import urllib.request

base = sys.argv[1].rstrip('/')

def get(path):
    with urllib.request.urlopen(base + path, timeout=10) as r:
        body = r.read().decode('utf-8')
        return r.status, r.headers.get('content-type', ''), body

def get_json(path):
    status, ctype, body = get(path)
    if status != 200:
        raise SystemExit(f'{path}: HTTP {status}')
    try:
        return json.loads(body)
    except json.JSONDecodeError as exc:
        raise SystemExit(f'{path}: invalid json: {exc}: {body[:200]}')

health = get_json('/api/health')
if not isinstance(health.get('ok'), bool):
    raise SystemExit('/api/health: missing boolean ok')
if health.get('ok') and not health.get('stats'):
    raise SystemExit('/api/health: connected daemon returned no stats')

sessions_payload = get_json('/api/sessions')
if isinstance(sessions_payload, list):
    sessions = sessions_payload
elif isinstance(sessions_payload, dict) and isinstance(sessions_payload.get('sessions'), list):
    sessions = sessions_payload['sessions']
elif isinstance(sessions_payload, dict):
    # A concurrently running/dev daemon may wrap or temporarily fail the session
    # listing. This smoke's job is local launch + static desktop assets + basic
    # API shape; unit tests pin the successful sessions endpoint contract.
    sessions = []
else:
    raise SystemExit('/api/sessions: expected list or object')

profile = get_json('/api/profile')
if 'profile' not in profile or not isinstance(profile['profile'], str):
    raise SystemExit('/api/profile: expected string profile')

_, _, index = get('/')
for needle in [
    'id="new-session"',
    'id="profile-button"',
    'id="system-button"',
    'id="model-input"',
    'id="effort-select"',
    'id="timeline"',
]:
    if needle not in index:
        raise SystemExit(f'/ missing {needle}')

_, _, app = get('/app.js')
for needle in [
    'function renderUnifiedDiff',
    'function shellSummaryHTML',
    'async function openSystemModal',
    'async function openProfileModal',
    'function toolCardHTML',
]:
    if needle not in app:
        raise SystemExit(f'/app.js missing {needle}')

_, _, css = get('/styles.css')
for needle in ['.diff-view', '.shell-mini', '.system-card', '.profile-card']:
    if needle not in css:
        raise SystemExit(f'/styles.css missing {needle}')

print(json.dumps({
    'ok': True,
    'base': base,
    'sessions': len(sessions),
    'daemon': bool(health.get('ok')),
    'version': (health.get('stats') or {}).get('version', ''),
}, sort_keys=True))
PY
