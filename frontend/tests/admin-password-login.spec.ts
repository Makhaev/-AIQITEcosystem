import { expect, test } from '@playwright/test';

test('administrator enters workspace with email and password and can reopen the account', async ({ page }) => {
  const user = { id: 1, name: 'Администратор', email: 'admin@example.test', roles: ['admin'], blocked: false, must_change_password: false };
  let signedIn = false;
  const factorRequests: string[] = [];
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    if (path.includes('/mfa/')) factorRequests.push(path);
    if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'test-csrf' } });
    if (path === '/auth/me') return signedIn ? route.fulfill({ json: user }) : route.fulfill({ status: 401, json: { code: 'unauthorized' } });
    if (path === '/auth/login') {
      expect(route.request().postDataJSON()).toEqual({ email: user.email, password: 'Admin-Password-9372' });
      signedIn = true;
      return route.fulfill({ json: { user } });
    }
    if (path === '/users') return route.fulfill({ json: { items: [user], total: 1, page: 1, page_size: 20 } });
    return route.fulfill({ status: 404, json: { message: 'Маршрут не найден' } });
  });
  await page.goto('/');
  await page.getByLabel('Email', { exact: true }).fill(user.email);
  await page.getByLabel('Пароль', { exact: true }).fill('Admin-Password-9372');
  await page.getByRole('button', { name: 'Войти', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Пользователи', exact: true })).toBeVisible();
  const menu = page.getByRole('button', { name: 'Меню', exact: true });
  if (await menu.isVisible()) await menu.click();
  await page.getByRole('link', { name: 'Мой аккаунт', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Смена пароля', exact: true })).toBeVisible();
  await expect(page.getByText(/MFA|Резервный код|Код приложения/)).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole('heading', { name: 'Мой аккаунт', exact: true })).toBeVisible();
  expect(factorRequests).toEqual([]);
});
