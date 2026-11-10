"""Backup workflow tests with isolated fake Docker/storage; no working DB is touched."""
import concurrent.futures
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
SHELL = os.environ.get("AIQ_TEST_SHELL") or shutil.which("sh")

DOCKER = r'''#!/bin/sh
set -eu
while [ "$1" != exec ] && [ "$1" != cp ]; do shift; done
action=$1; shift
if [ "$action" = cp ]; then
  [ "${AIQ_TEST_FAIL_STAGE:-}" != copy ] || exit 1
  source=${1#db:/tmp/}; cp "$AIQ_TEST_REMOTE/$source" "$2"
  exit 0
fi
shift 2 # -T db
case "$1" in
  mktemp)
    path=$(mktemp "$AIQ_TEST_REMOTE/aiq-backup.XXXXXX")
    printf '/tmp/%s\n' "${path##*/}" ;;
  sh)
    [ "${AIQ_TEST_FAIL_STAGE:-}" != dump ] || exit 1
    for path do :; done
    printf 'verified-test-dump' > "$AIQ_TEST_REMOTE/${path##*/}" ;;
  pg_restore)
    [ "${AIQ_TEST_FAIL_STAGE:-}" != verify ] || exit 1 ;;
  rm)
    for path do :; done
    rm -f -- "$AIQ_TEST_REMOTE/${path##*/}" ;;
  *) exit 2 ;;
esac
'''

RESTIC = r'''#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$AIQ_TEST_RESTIC_LOG"
[ "${AIQ_TEST_FAIL_STAGE:-}" != "restic-$1" ]
'''

FLOCK = r'''#!/bin/sh
[ "${AIQ_TEST_FAIL_STAGE:-}" != lock ]
'''


@unittest.skipUnless(SHELL, "Set AIQ_TEST_SHELL to a POSIX shell")
class BackupWorkflowTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / "deploy").mkdir()
        (self.root / "bin").mkdir()
        (self.root / "remote").mkdir()
        for name in ("backup.sh", "backup-offsite.sh"):
            shutil.copyfile(ROOT / "deploy" / name, self.root / "deploy" / name)
        for name, content in (("docker", DOCKER), ("restic", RESTIC), ("flock", FLOCK)):
            path = self.root / "bin" / name
            path.write_text(content, encoding="utf-8", newline="\n")
            path.chmod(0o700)
        (self.root / "password").write_text("test-only", encoding="utf-8")
        self.env = dict(os.environ)
        self.env.update({
            "AIQ_TEST_REMOTE": str(self.root / "remote").replace("\\", "/"),
            "AIQ_TEST_RESTIC_LOG": str(self.root / "restic.log").replace("\\", "/"),
            "RESTIC_REPOSITORY": "s3:https://example.invalid/test-only",
            "RESTIC_PASSWORD_FILE": str(self.root / "password").replace("\\", "/"),
            "AIQ_TEST_FAIL_STAGE": "",
        })

    def run_backup(self, offsite=False, fail=""):
        env = dict(self.env, AIQ_TEST_FAIL_STAGE=fail)
        # Set PATH inside the shell so Windows and POSIX use the same path syntax.
        script = 'export PATH="$PWD/bin:$PATH"; sh deploy/'
        script += "backup-offsite.sh" if offsite else "backup.sh --print-path"
        return subprocess.run([SHELL, "-c", script], cwd=self.root, env=env,
                              capture_output=True, text=True, timeout=30)

    def dumps(self):
        return list((self.root / ".cache" / "backups").glob("*.dump"))

    def assert_no_temporary_files(self):
        self.assertEqual(list((self.root / "remote").iterdir()), [])
        self.assertEqual(list((self.root / ".cache" / "backups").glob("*.partial")), [])

    def test_success_publishes_complete_dump_and_cleans_temporary_files(self):
        result = self.run_backup()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(self.dumps()), 1)
        self.assertEqual(self.dumps()[0].read_bytes(), b"verified-test-dump")
        self.assertTrue(result.stdout.strip().endswith(".dump"))
        self.assert_no_temporary_files()

    def test_failed_dump_verification_or_copy_preserves_previous_backup(self):
        self.assertEqual(self.run_backup().returncode, 0)
        original = self.dumps()[0]
        for stage in ("dump", "verify", "copy"):
            with self.subTest(stage=stage):
                self.assertNotEqual(self.run_backup(fail=stage).returncode, 0)
                self.assertEqual(self.dumps(), [original])
                self.assertEqual(original.read_bytes(), b"verified-test-dump")
                self.assert_no_temporary_files()

    def test_parallel_backups_have_distinct_complete_files(self):
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as executor:
            results = list(executor.map(lambda _: self.run_backup(), range(2)))
        for result in results:
            self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(self.dumps()), 2)
        self.assert_no_temporary_files()

    def test_offsite_checks_new_backup_before_retention(self):
        result = self.run_backup(offsite=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        commands = (self.root / "restic.log").read_text().splitlines()
        self.assertEqual([line.split()[0] for line in commands], ["backup", "check", "forget"])
        self.assertIn("--keep-daily 7", commands[-1])

    def test_failed_delivery_or_check_never_prunes_previous_copies(self):
        for stage in ("restic-backup", "restic-check"):
            with self.subTest(stage=stage):
                log = self.root / "restic.log"
                log.unlink(missing_ok=True)
                result = self.run_backup(offsite=True, fail=stage)
                self.assertNotEqual(result.returncode, 0)
                self.assertNotIn("forget", log.read_text())
                self.assert_no_temporary_files()

    def test_busy_lock_fails_without_creating_backup(self):
        self.assertNotEqual(self.run_backup(offsite=True, fail="lock").returncode, 0)
        self.assertEqual(self.dumps(), [])
        self.assertFalse((self.root / "restic.log").exists())

    def test_invalid_retention_is_rejected(self):
        for value in ("0", "00", "-1", "invalid"):
            with self.subTest(value=value):
                self.env["AIQ_BACKUP_KEEP_DAILY"] = value
                self.assertNotEqual(self.run_backup(offsite=True).returncode, 0)
                self.assertEqual(self.dumps(), [])


if __name__ == "__main__":
    unittest.main()
