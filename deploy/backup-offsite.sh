#!/bin/sh
# Requires restic and flock on the deployment host. Credentials come from its environment.
set -eu
cd "$(dirname "$0")/.."
umask 077
: "${RESTIC_REPOSITORY:?Set the external restic repository}"
: "${RESTIC_PASSWORD_FILE:?Set the path to the repository password file}"
test -r "$RESTIC_PASSWORD_FILE"
command -v restic >/dev/null
command -v flock >/dev/null
keep_daily=${AIQ_BACKUP_KEEP_DAILY:-7}
case "$keep_daily" in
  ''|*[!0-9]*) printf 'AIQ_BACKUP_KEEP_DAILY must be a positive integer\n' >&2; exit 2 ;;
esac
if ! [ "$keep_daily" -gt 0 ] 2>/dev/null; then
  printf 'AIQ_BACKUP_KEEP_DAILY must be a positive integer\n' >&2
  exit 2
fi
mkdir -p .cache/backups
# Serialize delivery and retention; another run must fail visibly rather than report success.
exec 9>.cache/backups/offsite.lock
flock -n 9 || { printf 'Another offsite backup is running\n' >&2; exit 1; }
backup=$(sh deploy/backup.sh --print-path)
restic backup --tag aiq-mvp -- "$backup"
# Do not prune older snapshots until the new backup and metadata check have succeeded.
restic check
restic forget --tag aiq-mvp --group-by host,tags --keep-daily "$keep_daily" --prune
printf 'Offsite backup completed\n'
