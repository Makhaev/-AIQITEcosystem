#!/bin/sh
set -eu
# Restrict substitution to these two values; preserve nginx's $host, $request_id, etc.
if ! printf '%s' "${PILOT_HOST:-}" | grep -Eq '^([A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?\.)*[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$'; then
    echo 'PILOT_HOST must be a DNS hostname without scheme or port' >&2
    exit 1
fi
case "${APP_ORIGIN:-}" in
    "https://$PILOT_HOST") ;;
    "https://$PILOT_HOST":*)
        pilot_port=${APP_ORIGIN##*:}
        case "$pilot_port" in ''|*[!0-9]*) echo 'Invalid HTTPS port' >&2; exit 1 ;; esac
        if [ "$pilot_port" -lt 1 ] || [ "$pilot_port" -gt 65535 ]; then exit 1; fi ;;
    *) echo 'APP_ORIGIN must be the HTTPS origin of PILOT_HOST' >&2; exit 1 ;;
esac
test -s /etc/nginx/tls/fullchain.pem
test -s /etc/nginx/tls/privkey.pem
envsubst '${PILOT_HOST} ${APP_ORIGIN}' < /etc/nginx/aiq/pilot.conf.template > /etc/nginx/conf.d/default.conf
nginx -t
exec nginx -g 'daemon off;'
