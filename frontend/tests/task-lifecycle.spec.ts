import { expect, test, type Page } from '@playwright/test';

async function fixture(page: Page, status: string, closedAt: string | null) {
  const user = { id: 3, name: 'Постановщик', email: 'creator@example.test', roles: ['curator'], blocked: false, must_change_password: false };
  const task = { id: 1, title: 'Проверить материалы', description: 'Проверить учебные материалы.', creator_id: 3, creator_name: user.name, assignee_id: 2, assignee_name: 'Ответственный', due_at: '2026-11-01T09:00:00Z', status, closed_at: closedAt, overdue: false, object_type: null, object_id: null, object_name: null };
  const changes: string[] = [];
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'example' } });
    if (path === '/auth/me') return route.fulfill({ json: user });
    if (path === '/tasks') return route.fulfill({ json: { items: [task], total: 1, page: 1, page_size: 20 } });
    if (path === '/tasks/1/status') {
      task.status = route.request().postDataJSON().status; changes.push(task.status);
      if (task.status === 'in_progress') task.closed_at = null;
      return route.fulfill({ json: task });
    }
    if (path === '/tasks/1') return route.fulfill({ json: task });
    return route.fulfill({ status: 404, json: { code: 'not_found', message: 'Не найдено.' } });
  });
  return { task, changes };
}

for (const scenario of [
  { status: 'todo', closedAt: null, text: 'Задача открыта' },
  { status: 'done', closedAt: null, text: 'Историческая дата закрытия неизвестна' },
  { status: 'cancelled', closedAt: null, text: 'Историческая дата закрытия неизвестна' },
  { status: 'done', closedAt: '2026-10-14T21:15:00Z', text: '15.10.2026, 00:15' },
]) {
  test(`task closure displays ${scenario.status} / ${scenario.closedAt ?? 'null'}`, async ({ page }) => {
    await fixture(page, scenario.status, scenario.closedAt);
    await page.goto('/#/tasks');
    await page.getByRole('button', { name: 'Проверить материалы', exact: true }).click();
    const dialog = page.getByRole('dialog');
    const row = dialog.locator('.detail-list > div').filter({ has: page.getByText('Закрытие (МСК)', { exact: true }) });
    await expect(row.locator('dd')).toHaveText(scenario.text);
    await expect(dialog.getByRole('button', { name: 'Изменить', exact: true })).toHaveCount(0);
    await expect(dialog.getByRole('link')).toHaveCount(0);
    if (scenario.status === 'cancelled') {
      await expect(dialog.getByRole('button', { name: 'Вернуть в работу' })).toHaveCount(0);
      await expect(dialog.getByRole('button', { name: 'Отменить задачу' })).toHaveCount(0);
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  });
}

test('creator with curator role can reopen and cancel but cannot edit or finish', async ({ page }) => {
  const state = await fixture(page, 'done', '2026-10-14T21:15:00Z');
  await page.goto('/#/tasks');
  await page.getByRole('button', { name: 'Проверить материалы', exact: true }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Вернуть в работу' }).click();
  await expect(page.getByRole('table')).toContainText('В работе');
  await page.getByRole('button', { name: 'Проверить материалы', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('Задача открыта');
  await expect(dialog.getByRole('button', { name: 'Завершить', exact: true })).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Изменить', exact: true })).toHaveCount(0);
  await dialog.getByRole('button', { name: 'Отменить задачу' }).click();
  await expect(page.getByRole('table')).toContainText('Отменена');
  expect(state.changes).toEqual(['in_progress', 'cancelled']);
});
