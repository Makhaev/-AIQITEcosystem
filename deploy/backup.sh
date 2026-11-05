#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
mkdir -p .cache/backups
backup=".cache/backups/aiq-$(date -u +%Y%m%dT%H%M%SZ).dump"
docker compose --env-file .env.production -f deploy/compose.production.yaml exec -T db pg_dump -U aiq -d aiq -Fc -f /tmp/aiq-backup.dump
docker compose --env-file .env.production -f deploy/compose.production.yaml exec -T db pg_restore --list /tmp/aiq-backup.dump >/dev/null
docker compose --env-file .env.production -f deploy/compose.production.yaml cp db:/tmp/aiq-backup.dump "$backup"
chmod 600 "$backup"
printf 'Backup saved: %s\n' "$backup"
