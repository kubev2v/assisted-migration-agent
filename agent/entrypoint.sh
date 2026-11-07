#!/bin/sh
# Managed startup and offline commands must not copy extensions before the host gate.
probation=false
[ -z "${AGENT_UPGRADE_ATTEMPT_ID:-}" ] || probation=true
for arg in "$@"; do
    case "$arg" in --upgrade-attempt-id|--upgrade-attempt-id=*) probation=true ;; esac
done
if [ "${1:-}" = run ] && [ "$probation" = false ] && [ ! -e /var/lib/agent/updates/authorization.json ] && [ -d /app/extensions ] && [ -d /var/lib/agent ]; then
    cp -n /app/extensions/*.duckdb_extension /var/lib/agent/ 2>/dev/null || true
fi
exec /app/agent "$@"
