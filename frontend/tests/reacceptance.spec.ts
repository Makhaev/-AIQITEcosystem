import { expect, test } from '@playwright/test';

test('F01 branches can be created, renamed and archived without deletion', async ({ page }) => {
  const branches: { id: number; name: string; status: string }[] = [];
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    const json = (body: unknown, status = 200) => route.fulfill({ json: body, status });
    if (path === '/auth/csrf') return json({ csrf_token: 'test' });
    if (path === '/auth/me') return json({ id: 1, name: 'Администратор', roles: ['admin'], email: 'admin@example.test' });
    if (path === '/branches' && route.request().method() === 'GET') return json({ items: branches, total: branches.length, page: 1, page_size: 100 });
    if (path === '/branches') {
      const item = { id: 1, ...route.request().postDataJSON() };
      branches.push(item); return json(item, 201);
    }
    if (path === '/branches/1' && route.request().method() === 'PUT') {
      Object.assign(branches[0], route.request().postDataJSON()); return json(branches[0]);
    }
    return json({ message: 'Недоступно' }, 404);
  });
  await page.goto('/#/branches');
  await page.getByLabel('Название филиала').fill('Центральный');
  await page.getByRole('button', { name: 'Сохранить филиал', exact: true }).click();
  const table = page.getByRole('table', { name: 'Филиалы', exact: true });
  await expect(table).toContainText('Центральный');
  await table.getByRole('button', { name: 'Изменить филиал', exact: true }).click();
  await page.getByLabel('Название филиала').fill('Центральный архив');
  await page.getByLabel('Статус филиала').selectOption('archived');
  await page.getByRole('button', { name: 'Сохранить филиал', exact: true }).click();
  await expect(table).toContainText('Центральный архив');
  await expect(table).toContainText('В архиве');
  expect(branches).toEqual([{ id: 1, name: 'Центральный архив', status: 'archived' }]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('F03 internal notes retain revisions after reopening and disappear on a denied pupil', async ({ page }) => {
  type Revision = { version: number; body: string; editor_name: string; created_at: string };
  type Note = { id: number; author_name: string; body: string; version: number; created_at: string; can_edit: boolean; history: Revision[] };
  const notes: Note[] = [];
  const readNotes: string[] = [];
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    const method = route.request().method();
    const json = (body: unknown, status = 200) => route.fulfill({ json: body, status });
    if (path === '/auth/csrf') return json({ csrf_token: 'test' });
    if (path === '/auth/me') return json({ id: 2, name: 'Куратор', roles: ['curator'], email: 'curator@example.test' });
    if (path === '/students/10/overview') return json({ id: 10, name: 'Ученик', status: 'active', full_access: true, curator_id: 99, curator_ids: [2], parents: [] });
    if (path === '/students/10/files') return json({ items: [], total: 0, page: 1, page_size: 20 });
    if (path === '/students/10/notes' && method === 'GET') {
      readNotes.push(path); return json({ items: notes, total: notes.length, page: 1, page_size: 20 });
    }
    if (path === '/students/10/notes' || path === '/students/10/notes/1') {
      const input = route.request().postDataJSON();
      if (method === 'POST') notes.push({ id: 1, author_name: 'Куратор', body: input.body, version: 1, created_at: '2026-11-04T10:00:00Z', can_edit: true, history: [] });
      else { expect(input.version).toBe(notes[0].version); notes[0].version++; notes[0].body = input.body; }
      notes[0].history.unshift({ version: notes[0].version, body: input.body, editor_name: 'Куратор', created_at: '2026-11-04T10:00:00Z' });
      return json({ id: 1, version: notes[0].version }, method === 'POST' ? 201 : 200);
    }
    return json({ message: 'Ученик недоступен' }, 404);
  });
  await page.goto('/#/students/10');
  await page.getByLabel('Новая заметка', { exact: true }).fill('Первая внутренняя заметка');
  await page.getByRole('button', { name: 'Добавить заметку', exact: true }).click();
  await expect(page.getByText('Первая внутренняя заметка', { exact: true }).first()).toBeVisible();
  await page.getByRole('button', { name: 'Исправить заметку', exact: true }).click();
  await page.getByLabel('Исправление заметки', { exact: true }).fill('Уточнённая внутренняя заметка');
  await page.getByRole('button', { name: 'Сохранить исправление', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Добавить заметку', exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByText('Уточнённая внутренняя заметка', { exact: true }).first()).toBeVisible();
  await page.getByText('История изменения', { exact: true }).click();
  await expect(page.getByText('Первая внутренняя заметка', { exact: true })).toBeVisible();
  expect(readNotes.length).toBeGreaterThan(1);
  await page.goto('/#/students/11');
  await expect(page.getByRole('alert')).toContainText('Ученик недоступен');
  await expect(page.getByRole('heading', { name: 'Заметки куратора', exact: true })).toHaveCount(0);
  await expect(page.getByText('Уточнённая внутренняя заметка', { exact: true })).toHaveCount(0);
});
