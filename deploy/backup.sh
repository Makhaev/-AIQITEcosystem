#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
case "${1:-}" in
  ''|--print-path) ;;
  *) printf 'Usage: sh deploy/backup.sh [--print-path]\n' >&2; exit 2 ;;
esac
umask 077
mkdir -p .cache/backups
compose() {
  docker compose --env-file "${AIQ_ENV_FILE:-.env.production}" -f deploy/compose.production.yaml "$@"
}
partial=''
remote=''
cleanup() {
  if [ -n "$partial" ]; then rm -f -- "$partial"; fi
  if [ -n "$remote" ]; then compose exec -T db rm -f -- "$remote" >/dev/null 2>&1 || :; fi
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# Unique files on both sides allow manual and scheduled backups to overlap safely.
partial=$(mktemp "$PWD/.cache/backups/aiq-$(date -u +%Y%m%dT%H%M%SZ)-XXXXXX.partial")
remote=$(compose exec -T db mktemp /tmp/aiq-backup.XXXXXX)
case "$remote" in
  /tmp/aiq-backup.*) ;;
  *) remote=''; printf 'Invalid backup temporary path\n' >&2; exit 1 ;;
esac
compose exec -T db sh -eu -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc -f "$1"' sh "$remote"
compose exec -T db pg_restore --list "$remote" >/dev/null
compose cp "db:$remote" "$partial" >&2
test -s "$partial"
chmod 600 "$partial"
backup="${partial%.partial}.dump"
mv -- "$partial" "$backup"
partial=''
if [ "${1:-}" = --print-path ]; then
  printf '%s\n' "$backup"
else
  printf 'Backup saved: %s\n' "$backup"
fi
