import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test('parent registers a child, reads documents and cancels without losing history', async ({ page }, info) => {
  let status = '', revision = 0;
  const history: { status: string; revision: number; created_at: string }[] = [];
  const docs: { id: number; name: string; size: number }[] = [];
  const stamp = '2026-10-29T12:00:00Z';
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', ''), method = route.request().method();
    if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'csrf' } });
    if (path === '/auth/me') return route.fulfill({ json: { id: 1, name: 'Родитель', roles: ['parent'], must_change_password: false } });
    if (path === '/events/11') return route.fulfill({ json: { id: 11, title: 'Встреча центра', type: 'Встреча', description: 'Открытая встреча', organizer: 'AIQ', responsibles: [], starts_at: '2027-04-01T10:00:00Z', ends_at: '2027-04-01T12:00:00Z', time_zone: 'Europe/Moscow', location: 'Зал', capacity: 1, registration_starts_at: stamp, registration_ends_at: '2027-04-01T09:00:00Z', conditions: 'Бесплатно', required_documents: 'Согласие', status: 'published', revision: 1, registration_open: true, can_edit: false, allowed_actions: [] } });
    if (path.endsWith('/candidates')) return route.fulfill({ json: { items: [{ id: 8, name: 'Алексей', active: true }], total: 1, page_size: 20 } });
    if (path.endsWith('/history')) return route.fulfill({ json: history });
    if (path.endsWith('/documents')) {
      if (method === 'POST') { expect(route.request().headers()['x-csrf-token']).toBe('csrf'); const body = route.request().postDataJSON(); expect(body.data).toBe(Buffer.from('Consent').toString('base64')); docs.push({ id: 2, name: body.name, size: 7 }); return route.fulfill({ status: 201, json: docs[0] }); }
      return route.fulfill({ json: { items: docs, total: docs.length, page_size: 20 } });
    }
    if (path.includes('/registrations')) {
      if (method === 'POST') {
        expect(route.request().headers()['x-csrf-token']).toBe('csrf');
        const body = route.request().postDataJSON();
        if (path.endsWith('/actions')) { expect(body).toEqual({ action: 'cancelled', revision }); status = 'cancelled'; }
        else { expect(body).toEqual({ student_id: 8 }); status = 'requested'; }
        revision++; history.push({ status, revision, created_at: stamp });
        return route.fulfill({ status: path.endsWith('/actions') ? 200 : 201, json: { id: 5, status } });
      }
      return route.fulfill({ json: { items: status ? [{ id: 5, student_id: 8, name: 'Алексей', status, revision, created_at: stamp, allowed_actions: status === 'requested' ? ['cancelled'] : [] }] : [], total: status ? 1 : 0, page_size: 20, can_manage: false, self_registration_min_age: 0, occupied: status === 'requested' ? 1 : 0 } });
    }
    return route.fulfill({ json: { items: [], total: 0 } });
  });
  await page.goto('/#/events/11');
  await page.getByLabel('Участник', { exact: true }).selectOption('8');
  await page.getByRole('button', { name: 'Подать заявку', exact: true }).click();
  await expect(page.getByText('Заявка подана', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Подтвердить участие' })).toHaveCount(0);
  await page.getByRole('button', { name: 'История и документы', exact: true }).click();
  await page.getByLabel('Добавить документ (до 10 МиБ)').setInputFiles({ name: 'consent.txt', mimeType: 'text/plain', buffer: Buffer.from('Consent') });
  await expect(page.getByRole('link', { name: /consent.txt/ })).toHaveAttribute('href', '/api/v1/events/11/registrations/5/documents/2');
  await expect(page.getByRole('status').filter({ hasText: 'Документ сохранён.' })).toBeVisible();
  await page.getByRole('button', { name: 'Отменить участие', exact: true }).click();
  await expect(page.getByText('Отменено', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'История и документы', exact: true }).click();
  await expect(page.getByText(/· Заявка подана/)).toBeVisible();
  await expect(page.getByText(/· Отменено/)).toBeVisible();
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: info.outputPath('event-participation.png'), fullPage: true });
});
