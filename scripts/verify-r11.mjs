// Runs only against a new, isolated Compose project. Never targets a pilot DB.
import { spawn } from 'node:child_process';
import { mkdir, writeFile, readFile, unlink } from 'node:fs/promises';
import { randomBytes } from 'node:crypto';
import { createServer } from 'node:net';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import assert from 'node:assert/strict';
import { standClient } from './r11-client.mjs';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const project = `aiq-r11-${Date.now()}-${randomBytes(3).toString('hex')}`;
const output = resolve(root, '.cache', project);
const tlsDir = resolve(output, 'tls');
const report = { startedAt: new Date().toISOString(), kind: 'isolated-docker-https', project, publishedPilot: false, checks: [], commands: [] };
const secrets = [randomBytes(32).toString('hex'), randomBytes(24).toString('hex')];
const env = { ...process.env, POSTGRES_PASSWORD: secrets[0], ADMIN_EMAIL: `bootstrap-${project}@example.test`,
  ADMIN_NAME: 'R11 fixture administrator', ADMIN_PASSWORD: secrets[1], PILOT_HOST: 'localhost',
  TLS_CERTS_DIR: tlsDir, PILOT_BIND_ADDRESS: '127.0.0.1', COMPOSE_PROJECT_NAME: project };
let ownsProject = false;
let maintenanceLogs = '';
await mkdir(output, { recursive: true });
await writeFile(resolve(output, 'empty.env'), '');
function record(name, detail = {}) { report.checks.push({ name, status: 'passed', ...detail }); console.log(`PASS ${name}`); }
function run(args, { log = true, allowFailure = false, timeout = 900_000 } = {}) {
  if (log) report.commands.push(['docker', ...args]);
  return new Promise((resolveRun, reject) => {
    const proc = spawn('docker', args, { cwd: root, env, windowsHide: true, stdio: ['ignore', 'pipe', 'pipe'] });
    let stdout = '', stderr = '';
    const timer = setTimeout(() => proc.kill(), timeout);
    proc.stdout.on('data', data => { stdout += data; });
    proc.stderr.on('data', data => { stderr += data; });
    proc.on('error', error => { clearTimeout(timer); reject(new Error(error.code === 'ENOENT' ? 'Docker executable is not installed' : 'Docker process could not start')); });
    proc.on('close', code => {
      clearTimeout(timer);
      if (code !== 0 && !allowFailure) {
        // docker config/inspect/logs can contain credentials: retain no raw diagnostics.
        reject(new Error(`Docker command failed (exit ${code}); see commands in report`));
      } else resolveRun({ code, stdout, stderr });
    });
  });
}
const base = ['compose', '--env-file', resolve(output, 'empty.env'), '-p', project, '-f', 'compose.pilot.yaml'];
const compose = args => run([...base, ...args]);
const port = () => new Promise((resolvePort, reject) => {
  const server = createServer(); server.once('error', reject);
  server.listen(0, '127.0.0.1', () => { const value = server.address().port; server.close(() => resolvePort(value)); });
});
try {
  report.dockerVersion = (await run(['version', '--format', '{{.Server.Version}}'])).stdout.trim();
  report.composeVersion = (await run(['compose', 'version', '--short'])).stdout.trim();
  record('docker_engine_and_compose');
  const existing = await run(['ps', '-aq', '--filter', `label=com.docker.compose.project=${project}`]);
  const volumes = await run(['volume', 'ls', '-q', '--filter', `label=com.docker.compose.project=${project}`]);
  assert.ok(!existing.stdout.trim() && !volumes.stdout.trim(), 'fixture project must be empty');
  ownsProject = true;
  const httpPort = await port();
  let httpsPort = await port();
  while (httpsPort === httpPort) httpsPort = await port();
  env.PILOT_HTTP_PORT = String(httpPort); env.PILOT_HTTPS_PORT = String(httpsPort);
  env.APP_ORIGIN = `https://localhost:${httpsPort}`;
  report.origin = env.APP_ORIGIN;
  // Certificate and key are generated inside the already pinned build image.
  const certificateUser = process.getuid ? ['--user', `${process.getuid()}:${process.getgid()}`,
    '-e', 'GOCACHE=/tmp/go-build', '-e', 'GOPATH=/tmp/go'] : [];
  await run(['run', '--rm', ...certificateUser, '-v', `${root}:/work`, '-w', '/work', 'golang:1.26.1-alpine',
    'go', 'run', 'scripts/local-cert.go', `/work/.cache/${project}/tls`]);
  await compose(['config', '--quiet']);
  record('compose_config');
  console.log('Building backend and frontend images...');
  await compose(['build', '--pull', 'backend', 'frontend']);
  record('compose_build');
  await compose(['up', '-d', '--wait', 'db']);
  const sql = async query => (await compose(['exec', '-T', 'db', 'psql', '-U', 'aiq', '-d', 'aiq', '-At', '-v', 'ON_ERROR_STOP=1', '-c', query])).stdout.trim();
  assert.equal(await sql("SELECT count(*) FROM pg_tables WHERE schemaname='public'"), '0', 'database must start empty');
  record('empty_database');
  const firstMigration = await compose(['run', '--rm', '-T', 'migrate']);
  maintenanceLogs += firstMigration.stdout + firstMigration.stderr;
  const migrationState = await sql('SELECT name,applied_at FROM schema_migrations ORDER BY name');
  assert.ok(migrationState.includes('016_lesson_participants.sql'), 'current migration');
  const repeatedMigration = await compose(['run', '--rm', '-T', 'migrate']);
  maintenanceLogs += repeatedMigration.stdout + repeatedMigration.stderr;
  assert.equal(await sql('SELECT name,applied_at FROM schema_migrations ORDER BY name'), migrationState, 'migration replay changed history');
  record('migrations_and_safe_repeat', { migrationCount: migrationState.split('\n').length });
  const bootstrap = await compose(['run', '--rm', '-T', '-e', 'ADMIN_EMAIL', '-e', 'ADMIN_NAME', '-e', 'ADMIN_PASSWORD', 'admin']);
  maintenanceLogs += bootstrap.stdout + bootstrap.stderr;
  record('first_administrator_bootstrap');
  await compose(['up', '-d', '--wait', 'backend', 'frontend']);
  await compose(['exec', '-T', 'frontend', 'nginx', '-t']);
  record('nginx_t');
  for (const service of ['db', 'backend']) {
    const id = (await compose(['ps', '-q', service])).stdout.trim();
    const exposed = (await run(['inspect', '--format', '{{json .HostConfig.PortBindings}}', id])).stdout.trim();
    assert.ok(exposed === '{}' || exposed === 'null', `${service} must not publish ports`);
  }
  record('database_and_backend_ports_not_published');
  report.images = [];
  for (const service of ['db', 'backend', 'frontend']) {
    const id = (await compose(['ps', '-q', service])).stdout.trim();
    const imageID = (await run(['inspect', '--format', '{{.Image}}', id])).stdout.trim();
    const metadata = await run(['image', 'inspect', '--format', '{{json .RepoTags}}\n{{json .RepoDigests}}', imageID]);
    const [tags, repoDigests] = metadata.stdout.trim().split('\n').map(v => JSON.parse(v));
    report.images.push({ service, imageID, tags, repoDigests });
  }
  report.buildBases = ['golang:1.26.1-alpine', 'alpine:3.23.3', 'node:22.23.3-alpine', 'nginx:1.28.2-alpine'];
  const client = standClient({ origin: env.APP_ORIGIN, ca: await readFile(resolve(tlsDir, 'fullchain.pem')),
    email: env.ADMIN_EMAIL, password: env.ADMIN_PASSWORD, record });
  await client.healthy(`http://localhost:${httpPort}`);
  await client.failures({
    stopDB: () => compose(['stop', '-t', '5', 'db']), startDB: () => compose(['start', '--wait', 'db']),
    stopBackend: () => compose(['stop', '-t', '5', 'backend']), startBackend: () => compose(['start', '--wait', 'backend']),
  });
  const logs = await compose(['logs', '--no-color', '--no-log-prefix', 'db', 'backend', 'frontend']);
  client.logs(maintenanceLogs + logs.stdout + logs.stderr);
  for (const secret of secrets) assert.ok(!(maintenanceLogs + logs.stdout + logs.stderr).includes(secret), 'environment secret leaked to logs');
  record('environment_secrets_excluded_from_logs');
  report.status = 'passed';
} catch (error) {
  report.status = report.checks.length ? 'failed' : 'blocked';
  report.failure = error.message;
  process.exitCode = 1;
} finally {
  if (ownsProject) {
    try { await compose(['down', '--volumes', '--remove-orphans']); record('isolated_project_cleanup'); }
    catch { report.cleanupFailed = true; report.status = 'failed'; process.exitCode = 1; }
  }
  try { await unlink(resolve(tlsDir, 'privkey.pem')); }
  catch (error) {
    if (error.code !== 'ENOENT') { report.privateKeyCleanupFailed = true; report.status = 'failed'; process.exitCode = 1; }
  }
  report.finishedAt = new Date().toISOString();
  await writeFile(resolve(output, 'report.json'), `${JSON.stringify(report, null, 2)}\n`);
  console.log(`R11 ${report.status}: ${resolve(output, 'report.json')}`);
}
