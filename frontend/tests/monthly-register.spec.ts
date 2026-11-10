import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test('monthly register follows student creation and archiving and exports Excel', async ({ page }) => {
  let active = true;
  let created = false;
  const first = { id: 10, name: 'Иванов Иван', status: 'active', birth_date: null, registered_on: '2026-01-05', groups: 'Группа AIQ', curators: 'Куратор AIQ' };
  const added = { ...first, id: 11, name: 'Новый ученик' };
  const currentMonth = new Intl.DateTimeFormat('sv-SE', { timeZone: 'Europe/Moscow', year: 'numeric', month: '2-digit' }).format(new Date());
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()); const path = url.pathname.replace('/api/v1', '');
    const json = (body: unknown, status = 200) => route.fulfill({ json: body, status });
    if (path === '/auth/me') return json({ id: 1, name: 'Администратор', email: 'admin@example.test', roles: ['admin'], must_change_password: false });
    if (path === '/auth/csrf') return json({ csrf_token: 'fixture' });
    if (path === '/features') return json({});
    if (path === '/students/monthly-register') {
      if (url.searchParams.get('format') === 'xlsx') return route.fulfill({ body: 'PK-test-download', contentType: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' });
      const historical = url.searchParams.get('month') !== currentMonth;
      const items = historical ? [first] : [...(active ? [first] : []), ...(created ? [added] : [])];
      return json({ items, total: items.length, month: url.searchParams.get('month'), as_of: '2026-11-08' });
    }
    if (path === '/students' && route.request().method() === 'POST') { created = true; return json(added, 201); }
    if (path === '/students/10' && route.request().method() === 'PATCH') { active = false; return json({ ...first, status: 'archived' }); }
    if (path === '/students') return json({ items: [...(active ? [first] : []), ...(created ? [added] : [])].map(s => ({ ...s, curators: [] })), total: Number(active) + Number(created) });
    return json({ items: [], total: 0 });
  });
  await page.goto('/#/students');
  const register = page.locator('.monthly-register');
  await expect(register.getByRole('heading', { name: 'Таблица учеников по месяцам', exact: true })).toBeVisible();
  await expect(register.getByRole('link', { name: 'Иванов Иван', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Добавить ученика', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Имя и фамилия', { exact: true }).fill('Новый ученик');
  await dialog.getByRole('button', { name: 'Сохранить ученика', exact: true }).click();
  await expect(register.getByRole('link', { name: 'Новый ученик', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Архивировать ученика Иванов Иван', exact: true }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Перенести в архив', exact: true }).click();
  await expect(register.getByRole('link', { name: 'Иванов Иван', exact: true })).toHaveCount(0);
  const past = new Date(`${currentMonth}-01T12:00:00Z`); past.setUTCMonth(past.getUTCMonth() - 1);
  const selectedMonth = past.toISOString().slice(0, 7);
  await register.getByLabel('Месяц таблицы', { exact: true }).fill(selectedMonth);
  await expect(register.getByRole('link', { name: 'Иванов Иван', exact: true })).toBeVisible();
  await expect(register.getByRole('link', { name: 'Новый ученик', exact: true })).toHaveCount(0);
  const download = page.waitForEvent('download'); await register.getByRole('button', { name: 'Скачать Excel', exact: true }).click();
  expect((await download).suggestedFilename()).toBe(`students-${selectedMonth}.xlsx`);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
});
