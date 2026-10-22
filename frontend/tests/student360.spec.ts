import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

async function fixture(page: Page, role = 'admin', age: number | null = 13) {
  const user = { id: 1, name: 'Тестовый сотрудник', email: 'staff@example.test', roles: [role], blocked: false, must_change_password: false };
  const lesson = { id: 9, group_id: 4, group_name: 'Развитие мышления', topic: 'Аргументация', teacher_name: 'Педагог', starts_at: '2026-10-12T21:00:00Z', ends_at: '2026-10-12T22:00:00Z', location: 'Кабинет 3' };
  let currentRole = role; let fail = false;
  const periods: string[] = [];
  const overviewPaths: string[] = [];
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()); const path = url.pathname.replace('/api/v1', '');
    if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'test' } });
    if (path === '/auth/me') return route.fulfill({ json: { ...user, roles: [currentRole] } });
    if (path === '/students/10/overview') {
      overviewPaths.push(path);
      if (fail) return route.fulfill({ status: 403, json: { code: 'forbidden', message: 'Доступ к карточке изменён.' } });
      periods.push(url.search);
      const full = currentRole !== 'teacher'; const empty = url.searchParams.get('from') === '2020-01-01';
      return route.fulfill({ json: {
        id: 10, name: 'Иван Иванов', status: 'active', full_access: full,
        ...(full ? { birth_date: age === null ? null : '2013-05-10', age, curator_id: 2, curator_name: 'Куратор ученика' } : {}),
        ...(full ? { parents: [{ id: 11, parent_id: 11, student_id: 10, name: 'Анна Иванова', phone: '+79001112233', email: 'private@example.test', status: 'active', relationship: 'Мать', is_primary: true }], history: [{ id: 12, occurred_at: '2026-10-12T09:00:00Z', actor_name: user.name, action: 'enrollment.transferred_in', object_type: 'enrollment', object_id: 3 }] } : {}),
        enrollments: [{ id: 3, group_id: 4, group_name: lesson.group_name, group_status: 'active', program_id: 6, program_name: 'Коммуникация', teacher_id: 5, teacher_name: 'Педагог', starts_on: '2026-09-01', ends_on: null }],
        upcoming_lessons: [lesson],
        attendance: { from: url.searchParams.get('from') || '2026-09-13', to: url.searchParams.get('to') || '2026-10-12', present: empty ? 0 : 1, late: empty ? 0 : 1, absent: empty ? 0 : 1, excused: empty ? 0 : 1, unmarked: 0, percent: empty ? null : 50, items: empty ? [] : ['present', 'late', 'absent', 'excused'].map((status, index) => ({ ...lesson, id: index + 20, status, comment: status === 'excused' ? 'По согласованию' : '' })) },
      } });
    }
    return route.fulfill({ status: 404, json: { code: 'not_found', message: 'Карточка не найдена.' } });
  });
  return { periods, overviewPaths, changeRole: (value: string) => { currentRole = value; }, fail: () => { fail = true; } };
}
async function accessible(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  const result = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze();
  expect(result.violations).toEqual([]);
}

test('Student 360 full card, Moscow time, period and no data', async ({ page }) => {
  const api = await fixture(page);
  await page.goto('/#/students/10'); await expect(page.getByRole('heading', { name: 'Иван Иванов', exact: true })).toBeVisible();
  await expect(page.getByText('Student 360 · Карточка №10')).toBeVisible();
  const ageRow = page.locator('.detail-list > div').filter({ has: page.getByText('Возраст (полных лет)', { exact: true }) });
  await expect(ageRow.locator('dd')).toHaveText('13');
  await expect(page.getByRole('table', { name: 'Родители ученика' })).toContainText('private@example.test');
  await expect(page.getByRole('table', { name: 'Зачисления ученика' })).toContainText('Коммуникация');
  await expect(page.getByRole('table', { name: 'Ближайшие занятия ученика' })).toContainText('13.10.2026, 00:00');
  await expect(page.getByLabel('Процент посещаемости')).toHaveText('50%');
  await expect(page.getByRole('table', { name: 'История изменений ученика' })).toContainText('Ученик переведён в группу');
  await accessible(page);
  await page.getByLabel('Период с', { exact: true }).fill('2026-10-13');
  await page.getByRole('button', { name: 'Показать посещаемость' }).click();
  await expect(page.getByRole('alert')).toContainText('Начало периода'); expect(api.periods).toHaveLength(1);
  await page.getByLabel('Период с', { exact: true }).fill('2020-01-01'); await page.getByLabel('Период по', { exact: true }).fill('2020-01-31');
  await page.getByRole('button', { name: 'Показать посещаемость' }).click();
  await expect(page.getByLabel('Процент посещаемости')).toHaveText('Нет данных');
  await expect(ageRow.locator('dd')).toHaveText('13');
  expect(api.periods.at(-1)).toBe('?from=2020-01-01&to=2020-01-31');
  expect(api.overviewPaths).toEqual(['/students/10/overview', '/students/10/overview']);
  await expect(page.getByRole('heading', { name: 'Ближайшие занятия', exact: true })).toBeVisible();
  await accessible(page);
});

test('Student 360 teacher has restricted card and live role changes remove private sections', async ({ page }) => {
  const api = await fixture(page, 'curator');
  await page.clock.install();
  await page.goto('/#/students/10'); await expect(page.getByRole('table', { name: 'Родители ученика' })).toBeVisible();
  await expect(page.getByText('Возраст (полных лет)', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Изменить карточку' })).toHaveCount(0);
  api.changeRole('teacher'); await page.clock.fastForward(60_000);
  await expect(page.getByText('Ограниченный доступ: ваши занятия', { exact: false })).toBeVisible();
  await expect(page.getByRole('table', { name: 'Родители ученика' })).toHaveCount(0);
  await expect(page.getByRole('heading', { name: 'История критических изменений' })).toHaveCount(0);
  await expect(page.getByText('private@example.test')).toHaveCount(0);
  await expect(page.getByText('Возраст (полных лет)', { exact: true })).toHaveCount(0);
  await expect(page.getByRole('table', { name: 'Посещаемость ученика' })).toBeVisible();
  await accessible(page);
  api.fail(); await page.getByRole('button', { name: 'Показать посещаемость' }).click();
  await expect(page.getByRole('alert')).toContainText('Доступ к карточке изменён');
  await expect(page.getByRole('table', { name: 'Посещаемость ученика' })).toHaveCount(0);
  await accessible(page);
});

for (const age of [null, 0]) {
  test(`Student 360 displays ${age === null ? 'unknown' : 'zero'} age`, async ({ page }) => {
    await fixture(page, 'admin', age);
    await page.goto('/#/students/10');
    const row = page.locator('.detail-list > div').filter({ has: page.getByText('Возраст (полных лет)', { exact: true }) });
    await expect(row.locator('dd')).toHaveText(age === null ? 'Не указано' : '0');
    await accessible(page);
  });
}
