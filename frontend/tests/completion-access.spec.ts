import { expect, test, type Page } from '@playwright/test';

async function fixture(page: Page, mixed: boolean) {
	const requests: string[] = [];
  const identity = (id: number) => ({ id, name: id === 10 ? 'Ученик куратора' : 'Ученик педагога', status: 'active' });
  const full = { birth_date: '2013-05-10', curator_id: 2, curator_name: 'Куратор примера', created_at: '2026-10-12T08:00:00Z', updated_at: '2026-10-12T08:00:00Z' };
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
	requests.push(path);
    const json = (body: unknown, status = 200) => route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });
    if (path === '/auth/csrf') return json({ csrf_token: 'test-only-csrf' });
    if (path === '/auth/me') return json({ id: 2, name: 'Педагог примера', email: 'teacher@example.test', roles: mixed ? ['teacher', 'curator'] : ['teacher'], blocked: false, must_change_password: false });
    if (path === '/students') return json({ items: [10, 11].map(id => ({ ...identity(id), ...(mixed && id === 10 ? full : {}) })), total: 2, page: 1, page_size: 20 });
    const card = path.match(/^\/students\/(10|11)\/(?:360|overview)$/);
    if (card) {
      const id = Number(card[1]); const allowed = mixed && id === 10;
      return json({ ...identity(id), ...(allowed ? { ...full, age: 13, parents: [], history: [] } : {}), full_access: allowed, enrollments: [], upcoming_lessons: [], attendance: { from: '2026-10-01', to: '2026-10-12', present: 0, late: 0, absent: 0, excused: 0, unmarked: 0, percent: null, items: [] } });
    }
    return json({ code: 'not_found', message: 'Запись не найдена.', field_errors: {}, request_id: 'example' }, 404);
  });
	return requests;
}

test('teacher list and overview omit private pupil sections', async ({ page }) => {
  const requests = await fixture(page, false);
  await page.goto('/#/students');
  await expect(page.getByRole('table', { name: 'Ученики AIQ' })).toBeVisible();
  await expect(page.getByRole('columnheader', { name: 'Дата рождения и куратор' })).toHaveCount(0);
  await page.getByRole('link', { name: 'Ученик педагога', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Основные сведения' })).toBeVisible();
  for (const name of ['Дата рождения', 'Возраст (полных лет)', 'Куратор', 'Родители и представители', 'История критических изменений']) {
    await expect(page.getByText(name, { exact: true })).toHaveCount(0);
  }
  await expect(page.getByRole('heading', { name: 'Посещаемость за период' })).toBeVisible();
  expect(requests).toContain('/students/11/overview');
  expect(requests.filter(path => path.startsWith('/parents') || path.startsWith('/audit-logs') || /^\/students\/\d+$/.test(path))).toEqual([]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('combined teacher and curator gets private fields only for assigned pupil', async ({ page }) => {
  await fixture(page, true);
  await page.goto('/#/students/10');
  await expect(page.getByText('Дата рождения', { exact: true })).toBeVisible();
  await expect(page.getByText('Куратор примера', { exact: true })).toBeVisible();
  await expect(page.getByText('Возраст (полных лет)', { exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'История критических изменений' })).toBeVisible();
  await page.goto('/#/students/11');
  await expect(page.getByRole('heading', { name: 'Ученик педагога' })).toBeVisible();
  await expect(page.getByText('Дата рождения', { exact: true })).toHaveCount(0);
  await expect(page.getByText('Возраст (полных лет)', { exact: true })).toHaveCount(0);
  await expect(page.getByText('Куратор примера', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'История критических изменений' })).toHaveCount(0);
});

test('foreign pupil ID shows denial and never loads contacts or audit', async ({ page }) => {
  const requests = await fixture(page, false);
  await page.goto('/#/students/12');
  await expect(page.getByRole('alert')).toContainText('Запись не найдена');
  await expect(page.getByRole('heading', { name: 'Основные сведения' })).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'Родители и представители' })).toHaveCount(0);
  expect(requests.filter(path => path.startsWith('/parents') || path.startsWith('/audit-logs'))).toEqual([]);
});
