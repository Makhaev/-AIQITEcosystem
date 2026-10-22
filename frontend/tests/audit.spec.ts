import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

async function fixture(page: Page, role = 'admin') {
  let currentRole = role, fail = false;
  const queries: URLSearchParams[] = [];
  const writes: string[] = [];
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname.replace('/api/v1', '');
    if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'test' } });
    if (path === '/auth/me') return route.fulfill({ json: { id: 1, name: 'Администратор', email: 'staff@example.test', roles: [currentRole], blocked: false, must_change_password: false } });
    if (path === '/audit-logs') {
      if (request.method() !== 'GET') writes.push(request.method());
      queries.push(url.searchParams);
      if (fail) return route.fulfill({ status: 503, json: { code: 'not_ready', message: 'Журнал временно недоступен.' } });
      const pageNumber = Number(url.searchParams.get('page') || '1'), filtered = url.searchParams.has('action');
      const empty = url.searchParams.get('object_id') === '999';
      const items = empty ? [] : Array.from({ length: filtered ? 1 : pageNumber === 1 ? 20 : 1 }, (_, index) => ({
        id: 30 - (pageNumber - 1) * 20 - index,
        occurred_at: '2026-10-12T21:00:00Z',
        actor_id: index === 1 ? null : 1, actor_name: index === 1 ? null : 'Администратор',
        action: index === 1 ? 'auth.login_failed' : 'student.updated',
        object_type: 'student', object_id: index === 1 ? null : 10,
        changed_fields: index === 1 ? [] : ['name', 'curator_id'],
        request_id: '0123456789abcdef0123456789abcdef',
      }));
      return route.fulfill({ json: { items, total: empty ? 0 : filtered ? 1 : 21, page: pageNumber, page_size: 20 } });
    }
    return route.fulfill({ status: 404, json: { code: 'not_found', message: 'Маршрут не найден.' } });
  });
  return { queries, writes, setFail: (value: boolean) => { fail = value; }, setRole: (value: string) => { currentRole = value; } };
}

async function accessible(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
}

test('Audit shows all required fields, Moscow time, pagination and filters without write controls', async ({ page }) => {
  const api = await fixture(page);
  await page.goto('/#/audit');
  const table = page.getByRole('table', { name: 'Журнал критических действий' });
  await expect(table).toBeVisible();
  for (const title of ['Время (МСК)', 'Пользователь', 'Действие', 'Тип объекта', 'ID объекта', 'Изменённые поля', 'Request ID']) {
    await expect(table.getByRole('columnheader', { name: title, exact: true })).toHaveCount(1);
  }
  await expect(table).toContainText('13 окт. 2026 г., 00:00');
  await expect(table).toContainText('Система / неизвестный пользователь');
  await expect(table).toContainText('curator_id');
  await expect(table).toContainText('0123456789abcdef0123456789abcdef');
  await expect(page.getByRole('button', { name: /Изменить|Удалить|Добавить/ })).toHaveCount(0);
  await accessible(page);
  await page.getByRole('button', { name: 'Следующая страница' }).click();
  await expect(page.getByText('Страница 2 из 2', { exact: false })).toBeVisible();
  expect(api.queries.at(-1)?.get('page')).toBe('2');
  await page.getByLabel('С даты', { exact: true }).fill('2026-10-13');
  await page.getByLabel('По дату', { exact: true }).fill('2026-10-13');
  await page.getByLabel('Автор (номер пользователя)', { exact: true }).fill('1');
  await page.getByLabel('Действие', { exact: true }).fill('student.updated');
  await page.getByLabel('Тип объекта', { exact: true }).fill('student');
  await page.getByLabel('ID объекта', { exact: true }).fill('10');
  await page.getByLabel('Request ID', { exact: true }).fill('0123456789abcdef0123456789abcdef');
  await page.getByRole('button', { name: 'Применить', exact: true }).click();
  await expect(page.getByText('Страница 1 из 1', { exact: false })).toBeVisible();
  expect(Object.fromEntries(api.queries.at(-1)!)).toEqual({ page: '1', page_size: '20', date_from: '2026-10-13', date_to: '2026-10-13', actor_id: '1', action: 'student.updated', object_type: 'student', object_id: '10', request_id: '0123456789abcdef0123456789abcdef' });
  const requests = api.queries.length;
  await page.getByLabel('С даты', { exact: true }).fill('2026-10-14');
  await page.getByRole('button', { name: 'Применить', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Начало периода');
  expect(api.queries).toHaveLength(requests);
  await page.getByLabel('С даты', { exact: true }).fill('2026-10-13');
  await page.getByLabel('ID объекта', { exact: true }).fill('999');
  await page.getByRole('button', { name: 'Применить', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Действий за этот период нет' })).toBeVisible();
  expect(api.writes).toEqual([]);
  await accessible(page);
});

test('Audit hides records on API failure and supports retry and repeated filters', async ({ page }) => {
  const api = await fixture(page);
  await page.goto('/#/audit');
  await expect(page.getByRole('table', { name: 'Журнал критических действий' })).toBeVisible();
  api.setFail(true);
  await page.getByRole('button', { name: 'Применить', exact: true }).click();
  await expect(page.getByText('Журнал временно недоступен.', { exact: true })).toBeVisible();
  await expect(page.getByRole('table', { name: 'Журнал критических действий' })).toHaveCount(0);
  await accessible(page);
  api.setFail(false);
  await page.getByRole('button', { name: 'Повторить загрузку' }).click();
  await expect(page.getByRole('table', { name: 'Журнал критических действий' })).toBeVisible();
  expect(api.queries).toHaveLength(3);
});

test('Audit rejects direct routes for leader, teacher and curator and reacts to role revocation', async ({ page }) => {
  await page.clock.install();
  const api = await fixture(page);
  await page.goto('/#/audit');
  await expect(page.getByRole('table', { name: 'Журнал критических действий' })).toBeVisible();
  api.setRole('leader');
  await page.clock.fastForward(60_000);
  await expect(page.getByRole('heading', { name: 'Раздел недоступен', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'История действий', exact: true })).toHaveCount(0);
  await expect(page.getByRole('table', { name: 'Журнал критических действий' })).toHaveCount(0);
  for (const role of ['leader', 'teacher', 'curator']) {
    api.setRole(role);
    await page.reload();
    await expect(page.getByRole('heading', { name: 'Раздел недоступен', exact: true })).toBeVisible();
  }
  expect(api.queries).toHaveLength(1);
  await accessible(page);
});
