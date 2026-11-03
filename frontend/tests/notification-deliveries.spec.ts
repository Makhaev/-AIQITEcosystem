import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test('failed email can be safely retried; conflicts preserve the reason', async ({ page }, info) => {
  let state = 'failed', revision = 3, conflict = true, forbidden = false;
  await page.route('**/api/v1/**', route => {
    const url = new URL(route.request().url()), path = url.pathname.replace('/api/v1', '');
    if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'csrf' } });
    if (path === '/auth/me') return route.fulfill({ json: { id: 1, name: 'Администратор', email: 'admin@example.test', roles: ['admin'], blocked: false, must_change_password: false } });
    if (path === '/notification-deliveries/20/retry') {
      expect(route.request().method()).toBe('POST');
      expect(route.request().headers()['x-csrf-token']).toBe('csrf');
      expect(route.request().postDataJSON()).toEqual({ revision: conflict ? 3 : 4, reason: 'SMTP восстановлен', acknowledge_duplicate_risk: true });
      if (conflict) { conflict = false; revision = 4; return route.fulfill({ status: 409, json: { message: 'Состояние отправки изменилось. Обновите очередь.' } }); }
      state = 'pending'; return route.fulfill({ json: { queued: true } });
    }
    if (path === '/notification-deliveries') {
      if (forbidden) return route.fulfill({ status: 403, json: { message: 'Доступ прекращён.' } });
      const visible = !url.searchParams.get('status') || url.searchParams.get('status') === state;
      const item = { id: 20, user_id: 7, title: 'Получен ответ на обращение', status: state, attempts: 5, cycle_attempts: state === 'pending' ? 0 : 5, max_attempts: 5, revision, next_attempt_at: state === 'pending' ? '2026-10-29T09:00Z' : null, sent_at: null, delivered_at: null, last_error: state === 'failed' ? 'provider_error' : '', can_retry: state === 'failed', retries: [] };
      return route.fulfill({ json: { items: visible ? [item] : [], total: visible ? 1 : 0, page: 1, page_size: 20 } });
    }
    return route.fulfill({ json: { items: [], total: 0 } });
  });
  await page.goto('/#/notification-deliveries');
  await expect(page.getByRole('heading', { name: 'Доставка уведомлений', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Вернуть в очередь' })).toBeDisabled();
  await page.getByLabel('Причина повтора отправки №20').fill('SMTP восстановлен');
  await page.getByRole('checkbox').check();
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.getByRole('heading', { name: 'Доставка уведомлений', exact: true }).click();
  await page.screenshot({ path: info.outputPath('outbox-failed.png'), fullPage: true });
  await page.getByRole('button', { name: 'Вернуть в очередь' }).click();
  await expect(page.getByRole('alert')).toContainText('Состояние отправки изменилось');
  await expect(page.getByRole('button', { name: 'Вернуть в очередь' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Обновить очередь' }).click();
  await expect(page.getByLabel('Причина повтора отправки №20')).toHaveValue('SMTP восстановлен');
  await expect(page.getByRole('checkbox')).toBeChecked();
  await page.getByRole('button', { name: 'Вернуть в очередь' }).click();
  await expect(page.getByText('Отправок с таким статусом нет.')).toBeVisible();
  await page.getByLabel('Статус отправки').selectOption('pending');
  await expect(page.getByText('Попытки: 0 из 5 в текущем цикле; всего 5.')).toBeVisible();
  forbidden = true;
  await page.getByRole('button', { name: 'Обновить очередь' }).click();
  await expect(page.getByRole('alert')).toContainText('Доступ прекращён');
  await expect(page.getByRole('heading', { name: 'Получен ответ на обращение' })).toHaveCount(0);
});

test('family roles cannot open the delivery administration screen', async ({ page }) => {
  let queueRequests = 0;
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/auth/csrf')) return route.fulfill({ json: { csrf_token: 'csrf' } });
    if (path.endsWith('/auth/me')) return route.fulfill({ json: { id: 7, name: 'Родитель', email: 'parent@example.test', roles: ['parent'], blocked: false, must_change_password: false } });
    if (path.includes('/notification-deliveries')) queueRequests++;
    return route.fulfill({ json: { items: [], total: 0 } });
  });
  await page.goto('/#/notification-deliveries');
  await expect(page.getByRole('heading', { name: 'Раздел недоступен' })).toBeVisible();
  expect(queueRequests).toBe(0);
});

test('a delayed failed queue cannot overwrite a newer status filter', async ({ page }) => {
  let release!: () => void, entered!: () => void;
  const held = new Promise<void>(resolve => { release = resolve; });
  const started = new Promise<void>(resolve => { entered = resolve; });
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()), path = url.pathname;
    if (path.endsWith('/auth/csrf')) return route.fulfill({ json: { csrf_token: 'csrf' } });
    if (path.endsWith('/auth/me')) return route.fulfill({ json: { id: 1, name: 'Администратор', email: 'admin@example.test', roles: ['admin'], blocked: false, must_change_password: false } });
    if (path.endsWith('/notification-deliveries')) {
      if (url.searchParams.get('status') === 'failed') {
        entered(); await held;
        return route.fulfill({ json: { items: [{ id: 20, title: 'Устаревшая очередь', status: 'failed', attempts: 5, cycle_attempts: 5, max_attempts: 5, can_retry: true, retries: [] }], total: 1, page_size: 20 } });
      }
      return route.fulfill({ json: { items: [], total: 0, page_size: 20 } });
    }
    return route.fulfill({ json: {} });
  });
  await page.goto('/#/notification-deliveries'); await started;
  await page.getByLabel('Статус отправки').selectOption('sent');
  await expect(page.getByText('Отправок с таким статусом нет.')).toBeVisible();
  release();
  await expect(page.getByRole('heading', { name: 'Устаревшая очередь' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Вернуть в очередь' })).toHaveCount(0);
});
