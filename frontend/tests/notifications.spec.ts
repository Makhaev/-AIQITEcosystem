import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test('notification inbox filters, marks read and clears revoked data', async ({ page }, info) => {
  let read = false, revoked = false;
  await page.route('**/api/v1/**', route => {
    const url = new URL(route.request().url()), p = url.pathname.replace('/api/v1', '');
    if (p === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'csrf' } });
    if (p === '/auth/me') return route.fulfill({ json: { id: 7, name: 'Родитель', email: 'parent@example.test', roles: ['parent'], blocked: false, must_change_password: false } });
    if (p === '/notifications/31/read') { expect(route.request().method()).toBe('POST'); read = true; return route.fulfill({ json: { read: true } }); }
    if (p === '/notifications') {
      if (revoked) return route.fulfill({ status: 403, json: { message: 'Доступ изменён. Обновите страницу.' } });
      const visible = !read || url.searchParams.get('unread') !== 'true';
      return route.fulfill({ json: { items: visible ? [{ id: 31, kind: 'request_answered', title: 'Получен ответ на обращение', url: '#/my-requests/4', created_at: '2026-10-29T09:00:00Z', read_at: read ? '2026-10-29T10:00:00Z' : null }] : [], total: visible ? 1 : 0, unread: read ? 0 : 1, page: 1, page_size: 20 } });
    }
    return route.fulfill({ json: { items: [], total: 0 } });
  });
  await page.goto('/#/notifications');
  await expect(page.getByRole('heading', { name: 'Уведомления', exact: true })).toBeVisible();
  await expect(page.getByRole('status')).toContainText('Непрочитанных: 1');
  await expect(page.getByRole('link', { name: 'Открыть в кабинете' })).toHaveAttribute('href', '#/my-requests/4');
  await page.getByRole('button', { name: 'Отметить прочитанным' }).click();
  await expect(page.getByRole('status')).toContainText('Непрочитанных: 0');
  await page.getByLabel('Показать уведомления').selectOption('true');
  await expect(page.getByText('Нет доступных уведомлений.')).toBeVisible();
  await page.getByLabel('Показать уведомления').selectOption('false');
  await expect(page.getByRole('heading', { name: 'Получен ответ на обращение' })).toBeVisible();
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: info.outputPath('notifications.png'), fullPage: true });
  revoked = true;
  await page.getByRole('button', { name: 'Обновить уведомления' }).click();
  await expect(page.getByRole('alert')).toContainText('Доступ изменён');
  await expect(page.getByRole('link', { name: 'Открыть в кабинете' })).toHaveCount(0);
});

test('a delayed inbox response cannot restore the previous filter', async ({ page }) => {
  let release!: () => void;
  const held = new Promise<void>(resolve => { release = resolve; });
  let requested!: () => void;
  const pending = new Promise<void>(resolve => { requested = resolve; });
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()), p = url.pathname.replace('/api/v1', '');
    if (p === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'csrf' } });
    if (p === '/auth/me') return route.fulfill({ json: { id: 7, name: 'Родитель', email: 'parent@example.test', roles: ['parent'], blocked: false, must_change_password: false } });
    if (p === '/notifications') {
      if (url.searchParams.get('unread') === 'false') { requested(); await held; return route.fulfill({ json: { items: [{ id: 1, title: 'Прежний список', url: '#/family', created_at: '2026-10-29T09:00:00Z', read_at: null }], total: 1, unread: 1, page: 1, page_size: 20 } }); }
      return route.fulfill({ json: { items: [], total: 0, unread: 0, page: 1, page_size: 20 } });
    }
    return route.fulfill({ json: {} });
  });
  await page.goto('/#/notifications'); await pending;
  await page.getByLabel('Показать уведомления').selectOption('true');
  await expect(page.getByText('Нет доступных уведомлений.')).toBeVisible();
  release();
  await expect(page.getByRole('heading', { name: 'Прежний список' })).toHaveCount(0);
});
