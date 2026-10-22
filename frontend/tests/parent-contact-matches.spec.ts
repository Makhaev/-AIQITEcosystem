import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

type Match = { id: number; name: string; status: 'active' | 'archived'; matched_fields: ('phone' | 'email')[] };
async function fixture(page: Page) {
  const parent = { id: 5, name: 'Текущий родитель', phone: '+79990000001', email: 'own@example.test', status: 'active' };
  const state = {
    matches: [{ id: 8, name: 'Другой родитель', status: 'archived', matched_fields: ['phone', 'email'] }] as Match[],
    failNext: false,
    checks: [] as { url: string; body: Record<string, unknown> }[],
    saves: [] as { method: string; path: string; body: Record<string, unknown> }[],
  };
  await page.route('**/api/v1/**', route => {
    const url = new URL(route.request().url()); const path = url.pathname.replace('/api/v1', ''); const method = route.request().method();
    const json = (body: unknown, status = 200) => route.fulfill({ status, json: body });
    if (path === '/auth/csrf') return json({ csrf_token: 'test' });
    if (path === '/auth/me') return json({ id: 1, name: 'Администратор', email: 'admin@example.test', roles: ['admin'], blocked: false, must_change_password: false });
    if (path === '/parents/contact-matches') {
      state.checks.push({ url: url.toString(), body: route.request().postDataJSON() });
      if (state.failNext) { state.failNext = false; return json({ code: 'unavailable', message: 'Попробуйте позже.', field_errors: {}, request_id: 'test' }, 503); }
      const page = Number(url.searchParams.get('page') || 1); const size = Number(url.searchParams.get('page_size') || 20);
      return json({ items: state.matches.slice((page - 1) * size, page * size), total: state.matches.length, page, page_size: size });
    }
    if ((path === '/parents' && method === 'POST') || (path === '/parents/5' && method === 'PATCH')) {
      const body = route.request().postDataJSON(); state.saves.push({ method, path, body });
      return json({ ...body, id: method === 'POST' ? 6 : 5 }, method === 'POST' ? 201 : 200);
    }
    if (path === '/parents' && method === 'GET') return json({ items: [parent], total: 1, page: 1, page_size: 20 });
    return json({ code: 'not_found', message: 'Не найдено.', field_errors: {}, request_id: 'test' }, 404);
  });
  return state;
}

async function newParent(page: Page, name = 'Отдельный человек') {
  await page.goto('/#/parents');
  await page.getByRole('button', { name: 'Добавить родителя', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Имя и фамилия').fill(name);
  await dialog.getByLabel('Телефон', { exact: true }).fill('8 (999) 123-45-67');
  await dialog.getByLabel('Email', { exact: true }).fill('shared@example.test');
  return dialog;
}

test('failed recheck removes old duplicate confirmation and offers an explicit retry', async ({ page }) => {
  const state = await fixture(page); const dialog = await newParent(page);
  await dialog.getByRole('button', { name: 'Сохранить родителя', exact: true }).click();
  await expect(dialog.getByRole('link', { name: 'Другой родитель' })).toBeVisible();
  await expect(dialog.getByRole('listitem').getByText('В архиве', { exact: true })).toBeVisible();
  await expect(dialog.getByText(/Совпадает: телефон, email/)).toBeVisible();
  state.failNext = true;
  await dialog.getByRole('button', { name: 'Сохранить родителя', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('Не удалось проверить совпадения контактов.');
  await expect(dialog.getByRole('button', { name: 'Подтвердить сохранение отдельно' })).toHaveCount(0);
  await expect(dialog.getByRole('link', { name: 'Другой родитель' })).toHaveCount(0);
  await expect(dialog.getByLabel('Имя и фамилия')).toHaveValue('Отдельный человек');
  await expect(dialog.getByLabel('Телефон', { exact: true })).toHaveValue('8 (999) 123-45-67');
  expect(state.saves).toHaveLength(0);
  await dialog.getByRole('button', { name: 'Повторить проверку', exact: true }).click();
  await expect(dialog.getByRole('button', { name: 'Подтвердить сохранение отдельно' })).toBeVisible();
  const axe = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze();
  expect(axe.violations).toEqual([]);
  await dialog.getByRole('button', { name: 'Подтвердить сохранение отдельно' }).click();
  await expect(dialog).not.toBeVisible();
  expect(state.checks).toHaveLength(3); expect(state.saves).toHaveLength(1);
  expect(state.saves[0]).toMatchObject({ method: 'POST', path: '/parents', body: { name: 'Отдельный человек' } });
});

test('all matches can be paged using contacts only in the POST body', async ({ page }) => {
  const state = await fixture(page);
  state.matches = Array.from({ length: 21 }, (_, i) => ({ id: i + 100, name: `Совпадение ${i + 1}`, status: i === 20 ? 'archived' : 'active', matched_fields: ['email'] }));
  const dialog = await newParent(page);
  await dialog.getByRole('button', { name: 'Сохранить родителя', exact: true }).click();
  await expect(dialog.getByRole('link')).toHaveCount(20);
  await expect(dialog.getByRole('listitem').first().getByText('Активен', { exact: true })).toBeVisible();
  await dialog.getByRole('button', { name: 'Следующая страница' }).click();
  await expect(dialog.getByRole('link')).toHaveCount(1);
  await expect(dialog.getByRole('link', { name: 'Совпадение 21', exact: true })).toBeVisible();
  await expect(dialog.getByRole('listitem').getByText('В архиве', { exact: true })).toBeVisible();
  expect(state.saves).toHaveLength(0);
  expect(state.checks.map(check => new URL(check.url).searchParams.get('page'))).toEqual(['1', '2']);
  for (const check of state.checks) {
    expect([...new URL(check.url).searchParams.keys()].sort()).toEqual(['page', 'page_size']);
    expect(check.body).toEqual({ phone: '8 (999) 123-45-67', email: 'shared@example.test' });
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await dialog.getByRole('button', { name: 'Подтвердить сохранение отдельно' }).click();
  await expect(dialog).not.toBeVisible(); expect(state.saves).toHaveLength(1);
});

test('editing excludes the current parent and saves without a self warning', async ({ page }) => {
  const state = await fixture(page); state.matches = [];
  await page.goto('/#/parents');
  await page.getByRole('button', { name: 'Изменить родителя Текущий родитель', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Имя и фамилия').fill('Исправленное имя');
  await dialog.getByRole('button', { name: 'Сохранить родителя', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect(state.checks).toHaveLength(1);
  expect(state.checks[0].body).toEqual({ phone: '+79990000001', email: 'own@example.test', exclude_id: 5 });
  expect(state.saves).toEqual([{ method: 'PATCH', path: '/parents/5', body: { name: 'Исправленное имя', status: 'active', phone: '+79990000001', email: 'own@example.test' } }]);
});

test('editing to shared contacts requires confirmation and preserves the parent identity', async ({ page }) => {
  const state = await fixture(page);
  await page.goto('/#/parents');
  await page.getByRole('button', { name: 'Изменить родителя Текущий родитель', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Email', { exact: true }).fill('shared@example.test');
  await dialog.getByRole('button', { name: 'Сохранить родителя', exact: true }).click();
  await expect(dialog.getByRole('button', { name: 'Подтвердить сохранение отдельно' })).toBeVisible();
  expect(state.saves).toHaveLength(0);
  expect(state.checks[0].body).toMatchObject({ email: 'shared@example.test', exclude_id: 5 });
  await dialog.getByRole('button', { name: 'Подтвердить сохранение отдельно' }).click();
  await expect(dialog).not.toBeVisible();
  expect(state.saves).toHaveLength(1); expect(state.saves[0]).toMatchObject({ method: 'PATCH', path: '/parents/5' });
});

test('same name with distinct contacts creates a separate parent without warning', async ({ page }) => {
  const state = await fixture(page); state.matches = [];
  const dialog = await newParent(page, 'Текущий родитель');
  await dialog.getByRole('button', { name: 'Сохранить родителя', exact: true }).click();
  await expect(dialog).not.toBeVisible();
  expect(state.checks[0].body).not.toHaveProperty('name');
  expect(state.checks[0].body).not.toHaveProperty('exclude_id');
  expect(state.saves).toHaveLength(1); expect(state.saves[0]).toMatchObject({ method: 'POST', path: '/parents', body: { name: 'Текущий родитель' } });
});
