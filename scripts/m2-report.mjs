// Summarize actual local test reports; do not promote local checks to acceptance.
import { readFileSync, writeFileSync, readdirSync, existsSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { resolve, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
function read(path) {
  const bytes = readFileSync(resolve(root, path));
  return bytes.toString(bytes[0] === 0xff && bytes[1] === 0xfe ? 'utf16le' : 'utf8').replace(/^\uFEFF/, '');
}
function json(path) {
  const text = read(path);
  return JSON.parse(text.slice(text.indexOf('{')));
}
function browserTests(report) {
  const tests = [];
  function visit(suite) {
    for (const spec of suite.specs ?? []) for (const test of spec.tests ?? []) {
      tests.push({ file: spec.file, title: spec.title, project: test.projectName, status: test.status });
    }
    for (const child of suite.suites ?? []) visit(child);
  }
  for (const suite of report.suites) visit(suite);
  return tests;
}
const events = read('.cache/m2/final-go.jsonl').trim().split(/\r?\n/).map(line => JSON.parse(line));
const goTests = events.filter(e => e.Test && !e.Test.includes('/') && ['pass', 'fail', 'skip'].includes(e.Action))
  .map(e => ({ package: e.Package, test: e.Test, status: e.Action }));
if (!goTests.length || goTests.some(t => t.status !== 'pass') || events.some(e => e.Action === 'fail')) {
  throw Error('Go report contains failures/skips or is empty');
}
const initialUI = json('.cache/m2/final-ui.json');
const retryUI = json('.cache/m2/groups-ui.json');
const ui = new Map(browserTests(initialUI).map(t => [`${t.project}:${t.file}:${t.title}`, t]));
for (const test of browserTests(retryUI)) ui.set(`${test.project}:${test.file}:${test.title}`, test);
const m2 = json('.cache/m2/e2e-report.json');
const core = json('.cache/r13/browser-report.json');
const browserResults = [...ui.values(), ...browserTests(m2), ...browserTests(core)];
if (browserResults.some(t => t.status !== 'expected') || [initialUI, retryUI, m2, core].some(r => r.errors.length)) {
  throw Error('Unresolved browser failures');
}
let followup;
if (existsSync(resolve(root, '.cache/m2/content-go.jsonl'))) {
  const events = read('.cache/m2/content-go.jsonl').trim().split(/\r?\n/).map(line => JSON.parse(line));
  const tests = events.filter(e => e.Test && !e.Test.includes('/') && ['pass', 'fail', 'skip'].includes(e.Action))
    .map(e => ({ package: e.Package, test: e.Test, status: e.Action }));
  const browser = json('.cache/m2/content-ui.json');
  if (!tests.length || tests.some(t => t.status !== 'pass') || events.some(e => e.Action === 'fail') ||
      browser.errors.length || browserTests(browser).some(t => t.status !== 'expected')) {
    throw Error('Follow-up checks failed');
  }
  followup = {
    scope: 'Detailed document size; M2 snapshot module correction; cross-program mapping; conflict recovery and draft navigation.',
    go: { command: 'go test -count=1 -json ./internal/app -run TestM2|TestLessonSnapshot', passed: tests.length, tests },
    browser: { stats: browser.stats, tests: browserTests(browser) },
  };
}
const hashes = {};
function hash(path) {
  hashes[relative(root, path).replaceAll('\\', '/')] = createHash('sha256').update(readFileSync(path)).digest('hex');
}
function walk(path, extensions) {
  for (const entry of readdirSync(path, { withFileTypes: true })) {
    const child = resolve(path, entry.name);
    if (entry.isDirectory()) walk(child, extensions);
    else if (extensions.some(ext => entry.name.endsWith(ext))) hash(child);
  }
}
for (const [dir, ext] of [
  ['backend/internal', ['.go']], ['backend/cmd', ['.go']], ['backend/migrations', ['.sql']],
  ['frontend/src', ['.tsx', '.ts', '.css']], ['frontend/e2e', ['.ts']], ['frontend/tests', ['.ts']],
]) walk(resolve(root, dir), ext);
for (const path of ['docs/openapi.yaml', 'docs/openapi-m2.yaml', '.github/workflows/ci.yml', 'scripts/m2-e2e.mjs', 'frontend/tests/groups.spec.ts']) hash(resolve(root, path));
const report = {
  date: '2026-10-20',
  recorded_at: new Date().toISOString(),
  status: 'local_checks_passed',
  acceptance: false,
  working_tree: 'uncommitted; not a release SHA',
  environment: 'Windows; isolated PostgreSQL on loopback 55435; installed Chrome; desktop and 360px',
  go: {
    command: 'go test -count=1 -json ./...',
    database: 'TEST_DATABASE_URL and R12_TEST_DATABASE_URL explicitly set; PG_BIN configured',
    top_level_tests: goTests.length, passed: goTests.length, failed: 0, skipped: 0,
    tests: goTests,
  },
  browser: {
    initial: initialUI.stats,
    reason_for_retry: 'GroupEditor lost the selected teacher while async options loaded; changed to controlled selection.',
    retry: retryUI.stats,
    resolved_tests: [...ui.values()],
    m2: { stats: m2.stats, tests: browserTests(m2) },
    core_real: { stats: core.stats, tests: browserTests(core) },
  },
  load_fixture: {
    note: 'Seed and inventory only; performance load was not rerun.',
    inventory: json('.cache/m2/load-inventory.json'),
  },
  followup,
  open_conditions: [
    'R11 container HTTPS stand acceptance',
    'R12 production backup storage, schedule and notifications',
    'R13 agreed resource limits and final release SHA',
    'R14 remote CI, release and customer acceptance',
  ],
  source_sha256_recorded_after_validation: hashes,
};
writeFileSync(resolve(root, 'docs/reports/m2-local-2026-10-20.json'), JSON.stringify(report, null, 2) + '\n');
console.log(`M2 report: ${goTests.length} Go tests; ${ui.size} resolved UI cases; ${browserTests(m2).length + browserTests(core).length} real browser scenarios.`);
if (followup) console.log(`Follow-up: ${followup.go.passed} targeted Go tests; ${followup.browser.stats.expected} browser cases.`);
