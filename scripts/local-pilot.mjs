// Local-only, synthetic pilot. All business requests reach the actual Go server.
import { spawn, execFileSync } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { createServer as createTLS } from 'node:tls';
import { createServer as createHTTP } from 'node:http';
import { createServer as createTCP } from 'node:net';
import { mkdirSync, readFileSync, writeFileSync, existsSync, openSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
const checking = process.argv.includes('--check');
const dir = resolve(root, '.cache/local-pilot', checking ? `check-${Date.now()}` : 'workspace');
mkdirSync(dir, { recursive: true });
const baseURL = process.env.TEST_DATABASE_URL;
if (!baseURL) throw Error('Set TEST_DATABASE_URL to a local aiq_mvp_validation_* database.');
const exe = process.platform === 'win32' ? '.exe' : '';
const fixtureTool = resolve(dir, `fixture${exe}`), serverTool = resolve(dir, `server${exe}`);
const env = { ...process.env };
const ports = { app: 5174, backend: 18081, smtp: 1465, mailbox: 18025 };
const appURL = `http://localhost:${ports.app}`;
for (const port of Object.values(ports)) await new Promise((done, reject) => {
 const probe = createTCP(); probe.once('error', reject); probe.listen(port, '127.0.0.1', () => probe.close(done));
});
function build(args) { execFileSync('go', args, { cwd: resolve(root, 'backend'), env, stdio: 'inherit', windowsHide: true }); }
build(['build', '-o', fixtureTool, './cmd/mvp-fixture']);
build(['build', '-o', serverTool, './cmd/server']);
const statePath = resolve(dir, 'access.json');
let state;
if (existsSync(statePath)) state = JSON.parse(readFileSync(statePath, 'utf8'));
else {
 const password = randomBytes(20).toString('hex');
 const fixture = JSON.parse(execFileSync(fixtureTool, ['init'], { env: { ...env, MVP_TEST_PASSWORD: password }, encoding: 'utf8', windowsHide: true }));
 state = { ...fixture, password, mail_key: randomBytes(24).toString('hex'), app_url: appURL, mailbox_url: `http://127.0.0.1:${ports.mailbox}` };
 writeFileSync(statePath, JSON.stringify(state, null, 2), { mode: 0o600 });
}
const certDir = resolve(dir, `tls-${Date.now()}`);
build(['run', '../scripts/local-cert.go', certDir]);
const children = [], messages = [];
const sockets = new Set();
const smtp = createTLS({ key: readFileSync(resolve(certDir, 'privkey.pem')), cert: readFileSync(resolve(certDir, 'fullchain.pem')), minVersion: 'TLSv1.2' }, socket => {
 sockets.add(socket); socket.once('close', () => sockets.delete(socket)); socket.on('error', () => {});
 socket.setTimeout(15000, () => socket.destroy()); socket.setEncoding('utf8');
 let buffer = '', recipient = '', data = null;
 socket.write('220 localhost ESMTP local pilot\r\n');
 socket.on('data', chunk => {
  buffer += chunk;
  if (buffer.length > 1024 * 1024) return socket.destroy();
  let index;
  while ((index = buffer.indexOf('\r\n')) >= 0) {
   const line = buffer.slice(0, index); buffer = buffer.slice(index + 2);
   if (data !== null) {
    if (line !== '.') { data.push(line.replace(/^\.\./, '.')); continue; }
    messages.push({ id: messages.length + 1, to: recipient, body: data.join('\n'), at: new Date().toISOString() });
    if (messages.length > 500) messages.shift();
    data = null; socket.write('250 Captured locally\r\n'); continue;
   }
   if (/^(EHLO|HELO) /i.test(line)) socket.write('250-localhost\r\n250 8BITMIME\r\n');
   else if (/^MAIL FROM:/i.test(line)) { recipient = ''; socket.write('250 OK\r\n'); }
   else if (/^RCPT TO:/i.test(line)) {
    const value = line.match(/<([^>]+)>/)?.[1] || '';
    if (!/^[^\r\n<>]+@example\.test$/i.test(value)) socket.write('550 Synthetic example.test recipients only\r\n');
    else { recipient = value; socket.write('250 OK\r\n'); }
   } else if (line === 'DATA' && recipient) { data = []; socket.write('354 End with dot\r\n'); }
   else if (line === 'QUIT') socket.end('221 Bye\r\n');
   else if (line === 'RSET') { recipient = ''; socket.write('250 OK\r\n'); }
   else socket.write('502 Unsupported command\r\n');
  }
 });
});
const escape = text => String(text).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const inbox = createHTTP((req, res) => {
 const basic = `Basic ${Buffer.from(`pilot:${state.mail_key}`).toString('base64')}`;
 if (req.headers.authorization !== basic && req.headers['x-pilot-key'] !== state.mail_key) {
  res.writeHead(401, { 'WWW-Authenticate': 'Basic realm="Local pilot mailbox"' }); return res.end('Authentication required');
 }
 res.setHeader('Cache-Control', 'no-store');
 res.setHeader('X-Content-Type-Options', 'nosniff');
 if (req.url === '/messages') { res.setHeader('Content-Type', 'application/json'); return res.end(JSON.stringify(messages)); }
 res.setHeader('Content-Type', 'text/html; charset=utf-8');
 res.setHeader('Content-Security-Policy', "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'");
 res.end(`<!doctype html><html lang="ru"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Почта локального пилота</title><style>body{font:18px system-ui;max-width:850px;margin:32px auto;padding:16px}article{border:1px solid #ccc;padding:20px;margin:20px 0}pre{white-space:pre-wrap;overflow-wrap:anywhere}a{color:#05652f}</style><h1>Почта локального пилота</h1><p>Письма остаются на этом компьютере. Внешняя отправка отсутствует.</p><a href="/">Обновить</a>${messages.slice().reverse().map(m => `<article><h2>${escape(m.to)}</h2><p>${escape(m.at)}</p><pre>${escape(m.body)}</pre>${(m.body.match(/http:\/\/localhost:5174\/#[^\s]+/g) || []).map(link => `<p><a href="${escape(link)}">Открыть ссылку из письма</a></p>`).join('')}</article>`).join('') || '<p>Писем пока нет.</p>'}</html>`);
});
function launch(command, args, name, extra = {}) {
 const child = spawn(command, args, { cwd: resolve(root, 'frontend'), env: { ...env, ...extra }, windowsHide: true, stdio: ['ignore', openSync(resolve(dir, `${name}.stdout.log`), 'w'), openSync(resolve(dir, `${name}.stderr.log`), 'w')] });
 children.push(child); return child;
}
async function ready(url) {
 for (let i = 0; i < 100; i++) { try { if ((await fetch(url)).ok) return; } catch {} await new Promise(r => setTimeout(r, 100)); }
 throw Error(`Not ready: ${url}`);
}
let stopping = false;
async function stop() {
 if (stopping) return; stopping = true;
 for (const child of children.reverse()) if (child.exitCode === null) { child.kill(); await new Promise(r => { child.once('exit', r); setTimeout(r, 2000); }); }
 for (const socket of sockets) socket.destroy();
 smtp.close(); inbox.close(); inbox.closeAllConnections();
 if (checking) execFileSync(fixtureTool, ['cleanup'], { env: { ...env, MVP_SCHEMA: state.schema }, stdio: 'inherit', windowsHide: true });
}
try {
 await new Promise(r => smtp.listen(ports.smtp, '127.0.0.1', r));
 await new Promise(r => inbox.listen(ports.mailbox, '127.0.0.1', r));
 launch(serverTool, [], 'backend', { DATABASE_URL: state.database_url, HTTP_ADDR: `127.0.0.1:${ports.backend}`, APP_ORIGIN: appURL, TRUST_PROXY: 'false', SMTP_HOST: '127.0.0.1', SMTP_PORT: String(ports.smtp), SMTP_TLS_MODE: 'tls', SMTP_FROM: 'pilot@example.test', SMTP_USERNAME: '', SMTP_PASSWORD: '', SMTP_CA_FILE: resolve(certDir, 'fullchain.pem') });
 const viteConfig = resolve(dir, 'vite.config.mjs');
 writeFileSync(viteConfig, `export default ${JSON.stringify({ root: resolve(root, 'frontend'), preview: { host: '127.0.0.1', port: ports.app, strictPort: true, proxy: { '/api': `http://127.0.0.1:${ports.backend}`, '/health': `http://127.0.0.1:${ports.backend}` } } })};`);
 launch(process.execPath, ['node_modules/vite/bin/vite.js', 'preview', '--config', viteConfig], 'frontend');
 await Promise.all([ready(`${appURL}/health/ready`), ready(appURL)]);
 if (checking) {
  const config = resolve(dir, 'playwright.config.mjs');
  writeFileSync(config, `export default ${JSON.stringify({ testDir: resolve(root, 'frontend/e2e'), testMatch: 'mvp.spec.ts', workers: 1, timeout: 180000, reporter: [['list'], ['json', { outputFile: resolve(root, '.cache/local-pilot/e2e-report.json') }]], outputDir: resolve(dir, 'screenshots'), use: { baseURL: appURL, trace: 'off', launchOptions: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH } : {} }, projects: [{ name: 'desktop', use: { viewport: { width: 1440, height: 1000 } } }, { name: 'mobile-360', use: { viewport: { width: 360, height: 800 } } }] })};`);
  const code = await new Promise((done, reject) => {
   const test = spawn(process.execPath, ['node_modules/@playwright/test/cli.js', 'test', '--config', config], { cwd: resolve(root, 'frontend'), env: { ...env, MVP_ACCESS_FILE: statePath }, windowsHide: true, stdio: 'inherit' });
   test.once('error', reject); test.once('exit', done);
  });
  process.exitCode = code || 0; await stop();
 } else {
  writeFileSync(resolve(dir, 'access.txt'), `Локальный пилот: ${appURL}\nТолько вымышленные данные.\n\nСотрудники: admin@example.test, teacher@example.test, curator@example.test, education_leader@example.test, methodologist@example.test\nПароль: ${state.password}\n\nПочта: ${state.mailbox_url}\nЛогин почты: pilot\nПароль почты: ${state.mail_key}\n`, { mode: 0o600 });
  console.log(`Local pilot ready: ${appURL}\nMailbox: ${state.mailbox_url}\nCredentials: ${resolve(dir, 'access.txt')}`);
  process.on('SIGINT', () => stop().then(() => process.exit())); process.on('SIGTERM', () => stop().then(() => process.exit()));
 }
} catch (err) { await stop(); throw err; }
