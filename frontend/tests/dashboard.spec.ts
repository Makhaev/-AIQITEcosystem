import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

async function fixture(page: Page, roles = ['admin'], attendancePercent = 40) {
  let currentRoles = roles, fail = false;
  const periods: string[] = [];
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url());
    const path = url.pathname.replace('/api/v1', '');
    if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'test' } });
    if (path === '/auth/me') return route.fulfill({ json: { id: 1, name: 'Сотрудник', email: 'staff@example.test', roles: currentRoles, blocked: false, must_change_password: false } });
    if (path === '/dashboard') {
      periods.push(url.search);
      if (fail) return route.fulfill({ status: 503, json: { code: 'not_ready', message: 'База данных недоступна.' } });
      const from = url.searchParams.get('date_from');
      return route.fulfill({ json: { active_students: 12, active_groups: 3, active_teachers: 4, lessons_today: 2, attendance_percent: from === '2020-01-01' ? null : from === '2020-02-01' ? 0 : attendancePercent, overdue_tasks: 5 } });
    }
    return route.fulfill({ status: 404, json: { code: 'not_found', message: 'Маршрут не найден.' } });
  });
  return { periods, setFail: (value: boolean) => { fail = value; }, setRoles: (value: string[]) => { currentRoles = value; } };
}

async function accessible(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
}

test('Dashboard shows six metrics, Moscow period, validation and no data versus zero', async ({ page }) => {
  await page.clock.install({ time: new Date('2026-09-30T21:30:00Z') });
  const api = await fixture(page, ['leader']);
  await page.goto('/#/dashboard');
  await expect(page.getByRole('region', { name: 'Посещаемость за период', exact: true })).toContainText('40%');
  for (const [label, value] of [['Активные ученики', '12'], ['Активные группы', '3'], ['Активные педагоги', '4'], ['Занятия сегодня', '2'], ['Просроченные задачи', '5']]) {
    await expect(page.getByRole('region', { name: label, exact: true }).locator('.stat-value')).toHaveText(value);
  }
  expect(api.periods).toEqual(['?date_from=2026-10-01&date_to=2026-10-31']);
  await expect(page.getByLabel('Период с', { exact: true })).toHaveValue('2026-10-01');
  await expect(page.getByLabel('Период по', { exact: true })).toHaveValue('2026-10-31');
  await expect(page.locator('.dashboard-period-summary')).toContainText('01.10.2026 — 31.10.2026');
  await accessible(page);
  await page.getByLabel('Период с', { exact: true }).fill('2026-10-02');
  await page.getByLabel('Период по', { exact: true }).fill('2026-10-01');
  await page.getByRole('button', { name: 'Показать', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Начало периода');
  expect(api.periods).toHaveLength(1);
  await expect(page.locator('.dashboard-period-summary')).toContainText('01.10.2026 — 31.10.2026');
  for (const [from, to, value] of [['2020-01-01', '2020-01-31', 'Нет данных'], ['2020-02-01', '2020-02-29', '0%']]) {
    await page.getByLabel('Период с', { exact: true }).fill(from);
    await page.getByLabel('Период по', { exact: true }).fill(to);
    await page.getByRole('button', { name: 'Показать', exact: true }).click();
    await expect(page.getByRole('region', { name: 'Посещаемость за период', exact: true }).locator('.stat-value')).toHaveText(value);
    await expect(page.getByRole('region', { name: 'Занятия сегодня', exact: true }).locator('.stat-value')).toHaveText('2');
    expect(api.periods.at(-1)).toBe(`?date_from=${from}&date_to=${to}`);
    await expect(page.locator('.dashboard-period-summary')).toContainText(`${from.split('-').reverse().join('.')} — ${to.split('-').reverse().join('.')}`);
  }
  await expect(page.getByRole('alert')).toHaveCount(0);
  await accessible(page);
});

for (const [now, from, to] of [
  ['2024-02-28T23:00:00Z', '2024-02-01', '2024-02-29'],
  ['2026-02-28T20:59:00Z', '2026-02-01', '2026-02-28'],
  ['2026-12-31T21:00:00Z', '2027-01-01', '2027-01-31'],
]) {
  test(`Dashboard defaults to the entire Moscow month at ${now}`, async ({ page }) => {
    await page.clock.install({ time: new Date(now) });
    const api = await fixture(page);
    await page.goto('/#/dashboard');
    await expect(page.getByRole('region', { name: 'Активные ученики', exact: true })).toBeVisible();
    expect(api.periods).toEqual([`?date_from=${from}&date_to=${to}`]);
    await expect(page.getByLabel('Период с', { exact: true })).toHaveValue(from);
    await expect(page.getByLabel('Период по', { exact: true })).toHaveValue(to);
    await expect(page.locator('.dashboard-period-summary')).toContainText(`${from.split('-').reverse().join('.')} — ${to.split('-').reverse().join('.')}`);
  });
}

test('Dashboard and Student 360 present the same six marks as 66,67% and distinguish empty data', async ({ page }) => {
  await fixture(page, ['leader'], 200 / 3);
  await page.route('**/api/v1/students/10/overview**', async route => {
    const query = new URL(route.request().url()).searchParams;
    const empty = query.get('from') === '2020-01-01';
    return route.fulfill({ json: {
      id: 10, name: 'Ученик R09', status: 'active', full_access: true, age: null,
      parents: [], enrollments: [], upcoming_lessons: [], history: [],
      attendance: { from: query.get('from') || '2024-06-01', to: query.get('to') || '2024-06-30',
        present: empty ? 0 : 3, late: empty ? 0 : 1, absent: empty ? 0 : 1, excused: empty ? 0 : 1,
        unmarked: 0, percent: empty ? null : 200 / 3, items: [] },
    } });
  });
  await page.goto('/#/dashboard');
  await expect(page.getByRole('region', { name: 'Посещаемость за период', exact: true }).locator('.stat-value')).toHaveText('66,67%');
  await page.getByLabel('Период с', { exact: true }).fill('2020-01-01');
  await page.getByLabel('Период по', { exact: true }).fill('2020-01-31');
  await page.getByRole('button', { name: 'Показать', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Посещаемость за период', exact: true }).locator('.stat-value')).toHaveText('Нет данных');
  await page.goto('/#/students/10');
  await expect(page.getByLabel('Процент посещаемости')).toHaveText('66,67%');
  await page.getByLabel('Период с', { exact: true }).fill('2020-01-01');
  await page.getByLabel('Период по', { exact: true }).fill('2020-01-31');
  await page.getByRole('button', { name: 'Показать посещаемость', exact: true }).click();
  await expect(page.getByLabel('Процент посещаемости')).toHaveText('Нет данных');
  await expect(page.locator('.student-attendance-summary')).toContainText('Присутствовал: 0 · Опоздал: 0 · Отсутствовал: 0 · Уважительная причина: 0');
  await accessible(page);
});

test('Dashboard refreshes the same period and hides metrics after errors until retry succeeds', async ({ page }) => {
  const api = await fixture(page);
  await page.goto('/#/dashboard');
  await expect(page.getByRole('region', { name: 'Активные ученики', exact: true })).toBeVisible();
  api.setFail(true);
  await page.getByRole('button', { name: 'Показать', exact: true }).click();
  await expect(page.getByText('База данных недоступна.', { exact: true })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Активные ученики', exact: true })).toHaveCount(0);
  expect(api.periods).toHaveLength(2);
  await accessible(page);
  api.setFail(false);
  await page.getByRole('button', { name: 'Повторить загрузку' }).click();
  await expect(page.getByRole('region', { name: 'Активные ученики', exact: true })).toBeVisible();
  expect(api.periods).toHaveLength(3);
});

test('Dashboard restricts direct routes and reacts to revoked leader role', async ({ page }) => {
  await page.clock.install();
  const api = await fixture(page, ['leader', 'teacher']);
  await page.goto('/#/dashboard');
  await expect(page.getByRole('region', { name: 'Активные ученики', exact: true })).toBeVisible();
  api.setRoles(['teacher', 'curator']);
  await page.clock.fastForward(60_000);
  await expect(page.getByRole('heading', { name: 'Раздел недоступен', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Обзор', exact: true })).toHaveCount(0);
  await expect(page.getByRole('region', { name: 'Активные ученики', exact: true })).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Раздел недоступен', exact: true })).toBeVisible();
  expect(api.periods).toHaveLength(1);
  await accessible(page);
});
