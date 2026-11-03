import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

const token = 'a'.repeat(43);
const password = 'New-Password-92741';
const generic = 'Если аккаунт доступен, мы отправим письмо со ссылкой для восстановления пароля. Ссылка действует 30 минут.';

async function api(page: Page, failure = '') {
  const requests: { path: string; body: Record<string, string> }[] = [];
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    expect(route.request().url()).not.toContain(token);
    expect(route.request().headers().referer || '').not.toContain(token);
    if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'synthetic-csrf' } });
    if (path === '/auth/me') return route.fulfill({ status: 401, json: { code: 'unauthorized' } });
    requests.push({ path, body: route.request().postDataJSON() });
    if (failure) return route.fulfill({ status: failure === 'rate_limited' ? 429 : 400, json: { code: failure, message: failure === 'rate_limited' ? 'Слишком много запросов. Повторите через 30 минут.' : 'Ссылка недействительна, истекла или уже использована. Запросите новую ссылку.' } });
    if (path.endsWith('/request')) return route.fulfill({ status: 202, json: { message: generic } });
    return route.fulfill({ status: 204 });
  });
  return requests;
}

test('request is reachable from login and gives the same notice for both emails', async ({ page }) => {
  const requests = await api(page);
  await page.goto('/');
  await page.getByRole('link', { name: 'Забыли пароль?' }).click();
  for (const email of ['existing@example.test', 'unknown@example.test']) {
    await page.getByLabel('Email', { exact: true }).fill(email);
    await page.getByRole('button', { name: 'Отправить ссылку' }).click();
    await expect(page.getByText(generic)).toBeVisible();
  }
  expect(requests.map(r => r.body.email)).toEqual(['existing@example.test', 'unknown@example.test']);
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()).violations).toEqual([]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('reset validates confirmation and UTF-8 length, clears URL and returns to login', async ({ page }) => {
  const requests = await api(page);
  await page.goto(`/#/reset-password?token=${token}`);
  await expect(page.getByRole('heading', { name: 'Установка нового пароля' })).toBeVisible();
  await expect(page).not.toHaveURL(new RegExp(token));
  await page.getByLabel('Новый пароль', { exact: true }).fill('я'.repeat(37));
  await page.getByLabel('Повторите новый пароль', { exact: true }).fill('я'.repeat(37));
  await page.getByRole('button', { name: 'Сохранить новый пароль' }).click();
  await expect(page.getByText('Пароль должен содержать не менее 12 символов и не более 72 байт UTF-8.')).toBeVisible();
  await page.getByLabel('Новый пароль', { exact: true }).fill(password);
  await page.getByLabel('Повторите новый пароль', { exact: true }).fill('Different-Password-92741');
  await page.getByRole('button', { name: 'Сохранить новый пароль' }).click();
  await expect(page.getByText('Новые пароли не совпадают.')).toBeVisible();
  await expect(page.getByLabel('Повторите новый пароль', { exact: true })).toBeFocused();
  await expect(page.getByLabel('Повторите новый пароль', { exact: true })).toHaveAttribute('aria-invalid', 'true');
  expect(requests).toEqual([]);
  await page.getByLabel('Повторите новый пароль', { exact: true }).fill(password);
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa']).analyze()).violations).toEqual([]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.getByRole('button', { name: 'Сохранить новый пароль' }).click();
  await expect(page.getByRole('heading', { name: 'Пароль изменён', exact: true })).toBeVisible();
  expect(requests).toEqual([{ path: '/auth/password-reset/confirm', body: { token, password } }]);
  await page.getByRole('link', { name: 'Войти', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Вход в AIQ Core' })).toBeVisible();
});

for (const failure of ['reset_invalid', 'rate_limited']) {
  test(`reset displays ${failure} and offers a new link`, async ({ page }) => {
    await api(page, failure);
    await page.goto(`/#/reset-password?token=${token}`);
    await page.getByLabel('Новый пароль', { exact: true }).fill(password);
    await page.getByLabel('Повторите новый пароль', { exact: true }).fill(password);
    await page.getByRole('button', { name: 'Сохранить новый пароль' }).click();
    await expect(page.getByRole('alert')).toContainText(failure === 'rate_limited' ? 'Слишком много запросов' : 'Ссылка недействительна');
    await page.getByRole('link', { name: 'Запросить новую ссылку' }).click();
    await expect(page.getByRole('heading', { name: 'Восстановление пароля' })).toBeVisible();
  });
}

test('missing token cannot submit a password', async ({ page }) => {
  const requests = await api(page);
  await page.goto('/#/reset-password');
  await expect(page.getByText('В ссылке нет токена.', { exact: false })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Сохранить новый пароль' })).toHaveCount(0);
  expect(requests).toEqual([]);
});
