import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test('late creation response does not navigate back after leaving the form', async ({ page }) => {
 let release!: () => void, finished!: () => void;
 const held = new Promise<void>(resolve => { release = resolve; });
 const done = new Promise<void>(resolve => { finished = resolve; });
 await page.route('**/api/v1/**', async route => {
  const p = new URL(route.request().url()).pathname.replace('/api/v1', '');
  if (p === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'csrf' } });
  if (p === '/auth/me') return route.fulfill({ json: { id: 2, name: 'Родитель', email: 'parent@example.test', roles: ['parent'], blocked: false, must_change_password: false } });
  if (p === '/family/requests' && route.request().method() === 'POST') { await held; try { await route.fulfill({ status: 201, json: { id: 4 } }); } finally { finished(); } return; }
  return route.fulfill({ json: { items: [], total: 0 } });
 });
 await page.goto('/#/my-requests');
 await page.getByLabel('Тема', { exact: true }).fill('Вопрос'); await page.getByLabel('Текст обращения').fill('Отправка с задержкой');
 const posted = page.waitForRequest(r => r.method() === 'POST' && r.url().endsWith('/family/requests'));
 await page.getByRole('button', { name: 'Отправить обращение' }).click(); await posted;
 await page.evaluate(() => { window.location.hash = '#/account'; });
 await expect(page.getByRole('heading', { name: 'Мой аккаунт', exact: true })).toBeVisible();
 release(); await done;
 await expect(page).toHaveURL(/#\/account$/);
});

test('staff assigns, starts, answers with attachment, closes and reopens a request', async ({ page }, info) => {
 let q: any = { id: 4, student_id: 7, student_name: 'Анна', author_name: 'Родитель', subject: 'Вопрос о занятии', body: 'Уточните материалы', reply: '', revision: 1, status: 'new', created_at: '2026-10-28T09:00:00Z', answered_at: null, closed_at: null, assignee_id: null, assignee_name: '', actions: ['assign', 'message'], messages: [{ id: 1, author_name: 'Родитель', body: 'Уточните материалы', kind: 'message', created_at: '2026-10-28T09:00:00Z', files: [] }], events: [] };
 await page.route('**/api/v1/**', route => {
  const p = new URL(route.request().url()).pathname.replace('/api/v1', '');
  if (p === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'csrf' } });
  if (p === '/auth/me') return route.fulfill({ json: { id: 1, name: 'Администратор', email: 'admin@example.test', roles: ['admin'], blocked: false, must_change_password: false } });
  if (p === '/family-requests/4') return route.fulfill({ json: q });
  if (p === '/family-requests/4/actions') {
   const b = route.request().postDataJSON(); expect(b.revision).toBe(q.revision); const from = q.status;
   if (b.action === 'assign') { expect(b.assignee_id).toBe(1); q.status = 'assigned'; q.assignee_name = 'Администратор'; q.actions = ['assign', 'start', 'message']; }
   if (b.action === 'start') { q.status = 'in_progress'; q.actions = ['assign', 'answer', 'message']; }
   if (b.action === 'answer') { expect(b.files[0].name).toBe('materials.txt'); expect(Buffer.from(b.files[0].data, 'base64').toString()).toBe('Materials'); q.status = 'answered'; q.answered_at = '2026-10-28T10:00:00Z'; q.actions = ['assign', 'close', 'message']; q.messages.push({ id: 2, author_name: 'Администратор', kind: 'answer', body: b.body, created_at: q.answered_at, files: [{ id: 3, name: 'materials.txt', url: '/api/v1/family-requests/4/files/3' }] }); }
   if (b.action === 'close') { q.status = 'closed'; q.closed_at = '2026-10-28T11:00:00Z'; q.actions = ['reopen']; }
   if (b.action === 'reopen') { expect(b.reason).toBe('Дополнительный вопрос'); q.status = 'assigned'; q.closed_at = null; q.actions = ['assign', 'start', 'message']; }
   q.events.push({ action: b.action, from_status: from, to_status: q.status, actor_name: 'Администратор', reason: b.reason, created_at: '2026-10-28T11:00:00Z' }); q.revision++; return route.fulfill({ json: q });
  }
  return route.fulfill({ status: 404, json: { message: 'Нет данных' } });
 });
 await page.goto('/#/family-requests/4');
 await page.getByLabel('ID ответственного').fill('1'); await page.getByRole('button', { name: 'Назначить ответственного' }).click();
 await page.getByRole('button', { name: 'Взять в работу' }).click();
 await page.getByLabel('Новое сообщение').fill('Прикладываю материалы занятия.');
 await page.getByLabel('Вложения — до 3 файлов, всего до 10 МиБ').setInputFiles({ name: 'materials.txt', mimeType: 'text/plain', buffer: Buffer.from('Materials') });
 await page.getByRole('button', { name: 'Ответить и отметить ответ' }).click();
 await expect(page.getByRole('link', { name: 'materials.txt' })).toBeVisible();
 await page.getByRole('button', { name: 'Закрыть обращение' }).click();
 await expect(page.getByLabel('Новое сообщение')).toHaveCount(0);
 await page.getByRole('button', { name: 'Открыть повторно' }).click();
 await expect(page.getByRole('alert')).toContainText('Укажите причину');
 await page.getByLabel('Причина повторного открытия').fill('Дополнительный вопрос');
 await page.getByRole('button', { name: 'Открыть повторно' }).click();
 await expect(page.getByRole('heading', { name: 'Назначено', exact: true })).toBeVisible();
 await expect(page.getByText('Дополнительный вопрос', { exact: true })).toBeVisible();
 expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
 await page.getByRole('heading', {level:1}).click();
 await page.screenshot({ path: info.outputPath('request-staff.png'), fullPage: true });
});

test('parent without children creates a general request and keeps input on conflict; revoked card clears', async ({ page }, info) => {
 let q: any = null, conflict = true, revoked = false;
 await page.route('**/api/v1/**', route => {
  const p = new URL(route.request().url()).pathname.replace('/api/v1', '');
  if (p === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'csrf' } });
  if (p === '/auth/me') return route.fulfill({ json: { id: 2, name: 'Родитель', email: 'parent@example.test', roles: ['parent'], blocked: false, must_change_password: false } });
  if (p === '/family/students') return route.fulfill({ json: { items: [], total: 0 } });
  if (p === '/family/requests' && route.request().method() === 'GET') return route.fulfill({ json: { items: q ? [q] : [], total: q ? 1 : 0 } });
  if (p === '/family/requests') { const b = route.request().postDataJSON(); expect(b.student_id).toBeNull(); expect(b.files[0].name).toBe('question.txt'); q = { id: 4, ...b, student_name: '', author_name: 'Родитель', status: 'new', revision: 1, actions: ['message'], created_at: '2026-10-28T09:00:00Z', messages: [{ id: 1, author_name: 'Родитель', kind: 'message', body: b.body, created_at: '2026-10-28T09:00:00Z', files: [{ id: 1, name: 'question.txt', url: '/api/v1/family/requests/4/files/1' }] }], events: [] }; return route.fulfill({ status: 201, json: q }); }
  if (p === '/family/requests/4') return revoked ? route.fulfill({ status: 404, json: { message: 'Доступ отозван.' } }) : route.fulfill({ json: q });
  if (p === '/family/requests/4/actions') { const b = route.request().postDataJSON(); if (conflict) { conflict = false; return route.fulfill({ status: 409, json: { message: 'Запись изменилась. Обновите обращение.' } }); } q.revision++; q.messages.push({ id: 2, author_name: 'Родитель', kind: 'message', body: b.body, created_at: '2026-10-28T10:00:00Z', files: [] }); return route.fulfill({ json: q }); }
  return route.fulfill({ status: 403, json: { message: 'Нет доступа' } });
 });
 await page.goto('/#/my-requests');
 await page.getByLabel('Тема', { exact: true }).fill('Общий вопрос'); await page.getByLabel('Текст обращения').fill('Вопрос о работе центра');
 await page.getByLabel('Вложения — до 3 файлов, всего до 10 МиБ').setInputFiles({ name: 'question.txt', mimeType: 'text/plain', buffer: Buffer.from('Question') });
 await page.getByRole('button', { name: 'Отправить обращение' }).click();
 await expect(page.getByRole('heading', { name: 'Общий вопрос', exact: true })).toBeVisible();
 await expect(page.getByRole('status').filter({ hasText: 'Обращение отправлено.' })).toBeVisible();
 await expect(page.getByLabel('ID ответственного')).toHaveCount(0);
 await page.getByLabel('Новое сообщение').fill('Уточняю вопрос'); await page.getByRole('button', { name: 'Добавить сообщение' }).click();
 await expect(page.getByRole('alert')).toContainText('Запись изменилась'); await expect(page.getByLabel('Новое сообщение')).toHaveValue('Уточняю вопрос');
 await page.getByRole('button', { name: 'Обновить обращение', exact: true }).click();
 await expect(page.getByLabel('Новое сообщение')).toHaveValue('Уточняю вопрос');
 await page.getByRole('button', { name: 'Добавить сообщение' }).click();
 await expect(page.getByText('Уточняю вопрос', { exact: true })).toBeVisible();
 await expect(page.getByRole('status').filter({ hasText: 'Сообщение отправлено.' })).toBeVisible();
 expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
 await page.getByRole('heading', {level:1}).click();
 await page.screenshot({ path: info.outputPath('request-parent.png'), fullPage: true });
 revoked = true; await page.getByRole('button', { name: 'Обновить обращение', exact: true }).click();
 await expect(page.getByRole('alert')).toContainText('Доступ отозван');
 await expect(page.getByText('Вопрос о работе центра', { exact: true })).toHaveCount(0); await expect(page.getByRole('link', { name: 'question.txt' })).toHaveCount(0);
});
