import http from 'node:http';
import https from 'node:https';
import assert from 'node:assert/strict';

// Never include request/response bodies or cookie values in assertion messages.
export function standClient({ origin, ca, email, password, record }) {
  const cookies = new Map();
  const privateValues = [email, password, '+79001234987', 'r11-private-token'];
  let csrf;
  const request = (path, { method = 'GET', body, headers = {}, base = origin } = {}) => new Promise((resolve, reject) => {
    const url = new URL(path, base);
    const data = body === undefined ? undefined : Buffer.from(typeof body === 'string' ? body : JSON.stringify(body));
    const transport = url.protocol === 'https:' ? https : http;
    const req = transport.request(url, {
      ca, family: 4, timeout: 8000,
      headers: { ...(cookies.size ? { Cookie: [...cookies].map(([k, v]) => `${k}=${v}`).join('; ') } : {}),
        ...(data ? { 'Content-Type': 'application/json', 'Content-Length': data.length } : {}), ...headers }, method,
    }, res => {
      const chunks = [];
      const protocol = res.socket.getProtocol?.();
      res.on('data', chunk => chunks.push(chunk));
      res.on('end', () => {
        for (const cookie of res.headers['set-cookie'] ?? []) {
          const [pair] = cookie.split(';');
          const index = pair.indexOf('=');
          const value = pair.slice(index + 1);
          cookies.set(pair.slice(0, index), value);
          if (value) privateValues.push(value);
        }
        const text = Buffer.concat(chunks).toString('utf8');
        let json;
        try { json = JSON.parse(text); } catch { /* Static HTML is expected too. */ }
        resolve({ status: res.statusCode, headers: res.headers, json, text, protocol });
      });
    });
    req.on('error', () => reject(new Error(`HTTP request failed for ${url.pathname}`)));
    req.on('timeout', () => req.destroy(new Error('request timeout')));
    req.end(data);
  });
  function error(res, status, code) {
    assert.equal(res.status, status, 'error status');
    assert.equal(res.json?.code, code, 'error code');
    assert.deepEqual(Object.keys(res.json ?? {}).sort(), ['code', 'field_errors', 'message', 'request_id']);
    assert.deepEqual(res.json.field_errors, {}, 'field_errors');
    assert.ok(res.json.message, 'error message');
    assert.match(res.json.request_id, /^[a-f0-9]{32}$/, 'request ID');
    assert.equal(res.headers['x-request-id'], res.json.request_id, 'request ID header');
  }
  function secureCookies(res, expected) {
    for (const name of expected) {
      const value = (res.headers['set-cookie'] ?? []).find(v => v.startsWith(`${name}=`));
      assert.ok(value, `missing ${name} cookie`);
      for (const flag of ['Secure', 'HttpOnly', 'SameSite=Strict', 'Path=/']) {
        assert.ok(value.split(';').map(v => v.trim()).includes(flag), `${name}: missing ${flag}`);
      }
      assert.ok(!/;\s*Domain=/i.test(value), 'cookie must be host-only');
    }
  }
  async function waitReady() {
    const until = Date.now() + 90_000;
    while (Date.now() < until) {
      try { if ((await request('/health/ready')).status === 200) return; } catch { /* startup */ }
      await new Promise(resolve => setTimeout(resolve, 500));
    }
    throw new Error('readiness did not recover');
  }
  async function healthy(httpOrigin) {
    await waitReady();
    const redirect = await request('/health/live', { base: httpOrigin });
    assert.equal(redirect.status, 308, 'HTTP redirect');
    assert.equal(redirect.headers.location, `${origin}/health/live`, 'HTTPS redirect target');
    record('http_redirect');
    const page = await request('/');
    assert.equal(page.status, 200, 'frontend status');
    assert.ok(page.text.includes('<div id="root">'), 'built frontend HTML');
    assert.ok(['TLSv1.2', 'TLSv1.3'].includes(page.protocol), 'TLS version');
    assert.ok(page.headers['strict-transport-security'], 'HSTS');
    record('https_and_frontend', { tls: page.protocol, certificateVerification: true });
    for (const path of ['/health/live', '/health/ready']) {
      const res = await request(path);
      assert.equal(res.status, 200, path);
      assert.equal(res.json?.status, 'ok', path);
    }
    record('health_live_ready');
    const anonymous = await request('/api/v1/auth/csrf');
    assert.equal(anonymous.status, 200, 'anonymous CSRF');
    secureCookies(anonymous, ['aiq_csrf']);
    csrf = anonymous.json?.csrf_token;
    assert.ok(csrf, 'CSRF token');
    privateValues.push(csrf);
    const credentials = { email, password };
    error(await request('/api/v1/auth/login', { method: 'POST', body: credentials, headers: { Origin: origin } }), 403, 'csrf_failed');
    error(await request('/api/v1/auth/login', { method: 'POST', body: credentials, headers: { Origin: 'https://untrusted.example', 'X-CSRF-Token': csrf } }), 403, 'csrf_failed');
    const login = await request('/api/v1/auth/login', { method: 'POST', body: credentials, headers: { Origin: origin, 'X-CSRF-Token': csrf } });
    assert.equal(login.status, 200, 'initial administrator login');
    secureCookies(login, ['aiq_session', 'aiq_csrf']);
    csrf = login.json?.csrf_token;
    privateValues.push(csrf);
    const me = await request('/api/v1/auth/me');
    assert.equal(me.status, 200, 'authenticated session');
    assert.ok(me.json?.roles?.includes('admin'), 'administrator role');
    record('login_secure_httponly_cookies_csrf');
    error(await request('/api/v1/auth/logout', { method: 'POST', headers: { Origin: origin } }), 403, 'csrf_failed');
    assert.equal((await request('/api/v1/auth/me')).status, 200, 'failed CSRF must preserve session');
    record('authenticated_csrf');
    const tooLarge = await request('/api/v1/users?email=r11-private@example.test', { method: 'POST', body: 'x'.repeat((1 << 20) + 1), headers: { Cookie: 'canary=r11-private-token', 'X-Contact': '+79001234987' } });
    privateValues.push('r11-private@example.test');
    error(tooLarge, 413, 'payload_too_large');
    record('nginx_body_limit_1m');
  }
  async function failures({ stopDB, startDB, stopBackend, startBackend }) {
    try {
      await stopDB();
      error(await request('/health/ready'), 503, 'not_ready');
      assert.equal((await request('/health/live')).status, 200, 'liveness with DB down');
      record('database_down_readiness_503_liveness_200');
    } finally { await startDB(); }
    await waitReady();
    record('database_recovery');
    try {
      await stopBackend();
      error(await request('/api/v1/users?email=r11-private@example.test', { headers: { Cookie: 'canary=r11-private-token' } }), 503, 'unavailable');
      error(await request('/health/ready'), 503, 'unavailable');
      record('backend_down_proxy_503');
    } finally { await startBackend(); }
    await waitReady();
    record('backend_recovery');
    assert.equal((await request('/api/v1/auth/me')).status, 200, 'session survives service restart');
    const logout = await request('/api/v1/auth/logout', { method: 'POST', headers: { Origin: origin, 'X-CSRF-Token': csrf } });
    assert.equal(logout.status, 204, 'logout');
    error(await request('/api/v1/auth/me'), 401, 'unauthorized');
    record('logout_session_revocation');
  }
  function logs(text) {
    for (const value of privateValues.filter(Boolean)) assert.ok(!text.includes(value), 'private data found in service logs');
    // Go logs route templates (r.Pattern), never the actual URL or query string.
    for (const line of text.split(/\r?\n/)) {
      let entry;
      try { entry = JSON.parse(line); } catch { continue; }
      for (const field of ['url', 'path', 'query', 'headers', 'cookie', 'body', 'email', 'phone', 'password', 'token']) {
        assert.ok(!Object.hasOwn(entry, field), 'unsafe field in structured logs');
      }
      assert.ok(!entry.route?.includes('?'), 'query in route log');
    }
    record('logs_exclude_contacts_passwords_tokens');
  }
  return { healthy, failures, logs, waitReady };
}
