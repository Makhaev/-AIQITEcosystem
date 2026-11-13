import { expect, test } from '@playwright/test';

const admin = { id: 1, name: 'Администратор', email: 'admin@example.test', roles: ['admin'], blocked: false, must_change_password: false, created_at: '2026-11-13T08:00:00Z', updated_at: '2026-11-13T08:00:00Z' };
const teacher = { ...admin, id: 2, name: 'Удаляемый педагог', email: 'teacher@example.test', roles: ['teacher'] };

test('deletion requires confirmation, handles errors and updates the list', async ({ page }) => {
  let items = [admin, teacher]; let deletes = 0; let fail = true;
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1/', '');
    if (path === 'auth/me') return route.fulfill({ json: admin });
    if (path === 'auth/csrf') return route.fulfill({ json: { csrf_token: 'test-csrf' } });
    if (path === 'features') return route.fulfill({ json: { student360: true } });
    if (path === 'users') return route.fulfill({ json: { items, total: items.length, page: 1, page_size: 20 } });
    if (path === 'users/2' && route.request().method() === 'DELETE') {
      deletes++;
      expect(route.request().headers()['x-csrf-token']).toBe('test-csrf');
      if (fail) return route.fulfill({ status: 409, json: { code: 'conflict', message: 'Не удалось удалить пользователя.' } });
      items = [admin];
      return route.fulfill({ status: 204 });
    }
    return route.fulfill({ status: 404, json: { message: 'Unexpected request' } });
  });
  await page.goto('/#/users');
  await expect(page.getByRole('button', { name: 'Удалить пользователя Администратор', exact: true })).toBeDisabled();
  const remove = page.getByRole('button', { name: 'Удалить пользователя Удаляемый педагог', exact: true });
  await remove.click();
  const dialog = page.getByRole('dialog', { name: 'Удалить пользователя?', exact: true });
  await expect(dialog).toContainText(teacher.email);
  await expect(dialog).toContainText('Связанные учебные записи и история сохранятся');
  expect(deletes).toBe(0);
  await dialog.getByRole('button', { name: 'Отмена' }).click();
  await expect(dialog).not.toBeVisible();
  expect(deletes).toBe(0);
  await remove.click();
  await dialog.getByRole('button', { name: 'Удалить пользователя', exact: true }).click();
  await expect(dialog).toContainText('Не удалось удалить пользователя.');
  await expect(dialog.getByRole('button', { name: 'Удалить пользователя', exact: true })).toBeEnabled();
  fail = false;
  await dialog.getByRole('button', { name: 'Удалить пользователя', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  await expect(page.getByRole('status').filter({ hasText: 'Пользователь удалён.' })).toBeVisible();
  await expect(remove).toHaveCount(0);
  await expect(page.getByText('Всего: 1', { exact: true })).toBeVisible();
  expect(deletes).toBe(2);
});

test('deleting the last row on a later page returns to the previous page', async ({ page }) => {
  let deleted = false;
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()); const path = url.pathname.replace('/api/v1/', '');
    if (path === 'auth/me') return route.fulfill({ json: admin });
    if (path === 'auth/csrf') return route.fulfill({ json: { csrf_token: 'test-csrf' } });
    if (path === 'features') return route.fulfill({ json: {} });
    if (path === 'users') return route.fulfill({ json: { items: url.searchParams.get('page') === '2' ? (deleted ? [] : [teacher]) : [admin], total: deleted ? 20 : 21, page: Number(url.searchParams.get('page') ?? 1), page_size: 20 } });
    if (path === 'users/2' && route.request().method() === 'DELETE') { deleted = true; return route.fulfill({ status: 204 }); }
    return route.fulfill({ status: 404 });
  });
  await page.goto('/#/users');
  await page.getByRole('button', { name: 'Следующая страница' }).click();
  await expect(page.getByText('Страница 2 из 2')).toBeVisible();
  await page.getByRole('button', { name: 'Удалить пользователя Удаляемый педагог' }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Удалить пользователя', exact: true }).click();
  await expect(page.getByText('Страница 1 из 1')).toBeVisible();
  await expect(page.getByText(admin.email, { exact: true })).toBeVisible();
});
