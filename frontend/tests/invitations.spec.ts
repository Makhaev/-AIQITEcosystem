import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

const token = 'synthetic-invitation-capability';
const email = 'parent@example.test';
const password = 'Synthetic-Parent-Password-4829';
const parent = { id: 2, name: 'Родитель', email, roles: ['parent'], blocked: false, must_change_password: false };
const admin = { ...parent, id: 1, name: 'Администратор', email: 'admin@example.test', roles: ['admin'] };
const preview = { email, role: 'parent', expires_at: '2026-10-26T10:00:00Z', requires_login: false };

async function recipientAPI(page: Page, existing = false, foreign = false, invalid = false) {
  let user: typeof parent | null = foreign ? admin : null;
  const accepted: unknown[] = [];
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    expect(route.request().url()).not.toContain(token);
    if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'csrf-test' } });
    if (path === '/auth/me') return user ? route.fulfill({ json: user }) : route.fulfill({ status: 401, json: { code: 'unauthorized' } });
    if (path === '/invitations/inspect') return invalid ? route.fulfill({ status: 410, json: { code: 'invitation_invalid', message: 'Приглашение недействительно, истекло или уже использовано.' } }) : route.fulfill({ json: { ...preview, requires_login: existing } });
    if (path === '/auth/logout') { user = null; return route.fulfill({ status: 204 }); }
    if (path === '/auth/login') { expect(route.request().postDataJSON().email).toBe(email); user = parent; return route.fulfill({ json: { user, csrf_token: 'signed-in-csrf' } }); }
    if (path === '/invitations/accept') { accepted.push(route.request().postDataJSON()); return route.fulfill({ json: { role: 'parent', card_id: 4, created_account: !existing } }); }
    if (path === '/account/links') return route.fulfill({ json: { items: [{ role: 'parent', card_id: 4, name: 'Родитель' }] } });
    return route.fulfill({ status: 404, json: { message: 'Недоступно' } });
  });
  return accepted;
}

test('new recipient sets a password, accepts only server binding and sees own account', async ({ page }, info) => {
  const accepted = await recipientAPI(page);
  await page.goto(`/#/invitation?token=${token}`);
  await expect(page.getByLabel('Новый пароль', { exact: true })).toBeVisible();
  await expect(page.getByLabel('Роль получателя')).toHaveCount(0);
  await page.getByLabel('Новый пароль', { exact: true }).fill(password);
  await page.getByLabel('Повторите пароль', { exact: true }).fill('Different-Password-4829');
  await page.getByRole('button', { name: 'Принять приглашение' }).click();
  await expect(page.getByText('Пароли не совпадают.')).toBeVisible();
  expect(accepted).toEqual([]);
  await page.getByLabel('Повторите пароль', { exact: true }).fill(password);
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: info.outputPath('invitation-accept.png'), fullPage: true });
  await page.getByRole('button', { name: 'Принять приглашение' }).click();
  await expect(page.getByRole('heading', { name: 'Приглашение принято' })).toBeVisible();
  expect(accepted).toEqual([{ token, password }]);
  await expect(page).not.toHaveURL(new RegExp(token));
  await page.getByRole('link', { name: 'Открыть мой аккаунт' }).click();
  await expect(page.getByRole('heading', { name: 'Подключённые карточки' })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Пользователи', exact: true })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'Программы', exact: true })).toHaveCount(0);
});

test('existing recipient logs in before accepting without password replacement', async ({ page }) => {
  const accepted = await recipientAPI(page, true);
  await page.goto(`/#/invitation?token=${token}`);
  await expect(page.getByLabel('Новый пароль', { exact: true })).toHaveCount(0);
  await page.getByLabel('Пароль существующего аккаунта', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Войти для принятия' }).click();
  expect(accepted).toEqual([]);
  await page.getByRole('button', { name: 'Принять приглашение' }).click();
  await expect(page.getByRole('heading', { name: 'Приглашение принято' })).toBeVisible();
  expect(accepted).toEqual([{ token }]);
});

test('foreign session must exit before recipient can log in', async ({ page }) => {
  const accepted = await recipientAPI(page, true, true);
  await page.goto(`/#/invitation?token=${token}`);
  await expect(page.getByRole('button', { name: 'Принять приглашение' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Выйти из другого аккаунта' }).click();
  await page.getByLabel('Пароль существующего аккаунта', { exact: true }).fill(password);
  await page.getByRole('button', { name: 'Войти для принятия' }).click();
  await page.getByRole('button', { name: 'Принять приглашение' }).click();
  await expect(page.getByRole('heading', { name: 'Приглашение принято' })).toBeVisible();
  expect(accepted).toEqual([{ token }]);
});

test('expired invitation never offers registration', async ({ page }) => {
  const accepted = await recipientAPI(page, false, false, true);
  await page.goto(`/#/invitation?token=${token}`);
  await expect(page.getByText('Приглашение недействительно, истекло или уже использовано.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Принять приглашение' })).toHaveCount(0);
  await expect(page.getByLabel('Новый пароль', { exact: true })).toHaveCount(0);
  expect(accepted).toEqual([]);
});

test('administrator chooses an existing card, sends, resends and revokes', async ({ page }, info) => {
  const invitations: { id: number; email: string; role: string; card_id: number; created_by: number; created_at: string; expires_at: string; status: string }[] = [];
  let mailFailure = false;
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'csrf-test' } });
    if (path === '/auth/me') return route.fulfill({ json: admin });
    if (path === '/parents') return route.fulfill({ json: { items: [{ id: 4, name: 'Выбранный родитель' }], total: 1 } });
    if (path === '/invitations' && route.request().method() === 'GET') return route.fulfill({ json: { items: invitations, total: invitations.length, page: 1, page_size: 20 } });
    if (path === '/invitations') {
      expect(route.request().postDataJSON()).toEqual({ email, role: 'parent', card_id: 4 });
      if (mailFailure) return route.fulfill({ status: 503, json: { message: 'Отправка приглашений не настроена.' } });
      const value = { id: 1, email, role: 'parent', card_id: 4, created_by: 1, created_at: '2026-10-24T10:00:00Z', expires_at: preview.expires_at, status: 'pending' }; invitations.push(value); return route.fulfill({ status: 201, json: value });
    }
    if (path === '/invitations/1/resend') { invitations[0].status = 'revoked'; const value = { ...invitations[0], id: 2, status: 'pending' }; invitations.unshift(value); return route.fulfill({ status: 201, json: value }); }
    if (path === '/invitations/2/revoke') { invitations[0].status = 'revoked'; return route.fulfill({ status: 204 }); }
    return route.fulfill({ status: 404, json: { message: 'Недоступно' } });
  });
  await page.goto('/#/invitations');
  await page.getByLabel('Email получателя').fill(email);
  await page.getByLabel('Существующая карточка', { exact: true }).selectOption('4');
  await page.getByRole('button', { name: 'Отправить приглашение', exact: true }).click();
  await expect(page.getByText('Ожидает принятия', { exact: true })).toBeVisible();
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: info.outputPath('invitations-admin.png'), fullPage: true });
  page.on('dialog', dialog => void dialog.accept());
  await page.getByRole('button', { name: 'Отправить повторно' }).click();
  await expect(page.getByText('Новое письмо передано почтовому серверу. Прежняя ссылка отозвана.')).toBeVisible();
  await page.getByRole('button', { name: 'Отозвать', exact: true }).click();
  await expect(page.getByText('Приглашение отозвано.', { exact: true })).toBeVisible();
  mailFailure = true;
  await page.getByLabel('Email получателя').fill(email);
  await page.getByLabel('Существующая карточка', { exact: true }).selectOption('4');
  await page.getByRole('button', { name: 'Отправить приглашение', exact: true }).click();
  await expect(page.getByText('Отправка приглашений не настроена.')).toBeVisible();
});
