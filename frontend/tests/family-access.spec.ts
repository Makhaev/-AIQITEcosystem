import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test('administrator confirms a link and publishes then withdraws a family report', async ({ page }, info) => {
 let confirmed = false; let report: any = { id: 4, title: 'Отчёт семье', body: 'Итог месяца', revision: 3, version: 1, state: 'approved', actions: ['publish'], published: false, files: [] };
 await page.route('**/api/v1/**', route => {
  const p = new URL(route.request().url()).pathname.replace('/api/v1', '');
  if (p === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'test-csrf' } });
  if (p === '/auth/me') return route.fulfill({ json: { id: 1, name: 'Администратор', email: 'admin@example.test', roles: ['admin'], blocked: false, must_change_password: false } });
  if (p === '/students/7/overview') return route.fulfill({ json: { id: 7, name: 'Анна', status: 'active', full_access: true, parents: [] } });
  if (p === '/students/7/files' || p === '/students/7/family-requests') return route.fulfill({ json: { items: [], total: 0 } });
  if (p === '/students/7/family-access') return route.fulfill({ json: [{ parent_id: 8, name: 'Родитель Анны', confirmed }] });
  if (p === '/students/7/parents/8/access') { confirmed = route.request().postDataJSON().confirmed; return route.fulfill({ status: 204 }); }
  if (p === '/students/7/family-reports') return route.fulfill({ json: { items: [report], total: 1 } });
  if (p === '/family-reports/4/actions') { const body = route.request().postDataJSON(); expect(body.revision).toBe(report.revision); expect(body.version).toBe(1); const published = body.action === 'publish'; if (!published) expect(body.reason).toBe('Ошибка в отчёте'); report = { ...report, published, published_version: published ? 1 : null, state: published ? 'published' : 'withdrawn', actions: published ? ['withdraw', 'correct'] : ['correct'], revision: report.revision + 1 }; return route.fulfill({ json: report }); }
  return route.fulfill({ status: 404, json: { message: 'Недоступно' } });
 });
 await page.goto('/#/students/7');
 await page.getByRole('button', { name: 'Открыть управление' }).click();
 await page.getByRole('button', { name: 'Подтвердить доступ', exact: true }).click();
 await expect(page.getByRole('button', { name: 'Отозвать доступ', exact: true })).toBeVisible();
 expect(confirmed).toBe(true);
 await page.getByRole('button', { name: 'Опубликовать семье' }).click();
 await expect(page.getByRole('button', { name: 'Отозвать публикацию' })).toBeVisible();
 expect(report.published).toBe(true);
 expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
 await page.screenshot({ path: info.outputPath('family-management.png'), fullPage: true });
 await page.getByLabel('Замечание или причина — отчёт №4').fill('Ошибка в отчёте');
 await page.getByRole('button', { name: 'Отозвать публикацию' }).click();
 await expect(page.getByRole('heading', { name: 'Отчёт семье · Отозван' })).toBeVisible();
 await expect(page.getByRole('button', { name: 'Опубликовать семье' })).toHaveCount(0);
 expect(report.published).toBe(false);
 await page.getByRole('button', { name: 'Отозвать доступ', exact: true }).click();
 await expect(page.getByRole('button', { name: 'Подтвердить доступ', exact: true })).toBeVisible();
 expect(confirmed).toBe(false);
});

test('family portal shows permitted data and clears a revoked report on the next request', async ({ page }, info) => {
 let revoked = false;
 await page.route('**/api/v1/**', route => {
  const p = new URL(route.request().url()).pathname.replace('/api/v1', '');
  if (p === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'test-csrf' } });
  if (p === '/auth/me') return route.fulfill({ json: { id: 2, name: 'Родитель', email: 'parent@example.test', roles: ['parent'], blocked: false, must_change_password: false } });
  if (p === '/family/students') return route.fulfill({ json: { items: [{ id: 7, name: 'Анна' }], total: 1 } });
  if (p.startsWith('/family/students/7/')) {
   if (revoked) return route.fulfill({ status: 404, json: { message: 'Данные не найдены или доступ отозван.' } });
   if (p.endsWith('/lessons')) return route.fulfill({ json: { items: [{ id: 1, topic: 'Алгоритмы', starts_at: '2026-10-26T12:00:00Z', ends_at: '2026-10-26T13:00:00Z', status: 'planned', attendance: 'unmarked' }], total: 1 } });
   if (p.endsWith('/reports')) return route.fulfill({ json: { items: [{ id: 4, title: 'Результаты октября', body: 'Анна объясняет решение.', revision: 2, published: true, files: [{ id: 9, name: 'Результат.pdf', url: '/api/v1/family/reports/4/files/9' }] }], total: 1 } });
   if (p.endsWith('/requests')) return route.fulfill({ json: { items: [], total: 0 } });
   if (p.endsWith('/notifications')) return route.fulfill({ json: [{ report_id: 4, version: 1, title: 'Результаты октября', created_at: '2026-10-28T09:00:00Z' }] });
  }
  return route.fulfill({ status: 403, json: { message: 'Нет доступа' } });
 });
 await page.goto('/#/family');
 await page.getByLabel('Ученик', { exact: true }).selectOption('7');
 await page.getByLabel('Раздел обучения').selectOption('lessons');
 await expect(page.getByRole('heading', { name: 'Алгоритмы' })).toBeVisible();
 await page.getByLabel('Раздел обучения').selectOption('reports');
 await expect(page.getByRole('heading', { name: 'Результаты октября' })).toBeVisible();
 await expect(page.getByRole('link', { name: 'Результат.pdf' })).toHaveAttribute('href', '/api/v1/family/reports/4/files/9');
 await expect(page.getByRole('link', { name: 'Пользователи', exact: true })).toHaveCount(0);
 expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
 await page.screenshot({ path: info.outputPath('family-report.png'), fullPage: true });
 revoked = true;
 await page.getByRole('button', { name: 'Обновить данные', exact: true }).click();
 await expect(page.getByRole('alert')).toContainText('доступ отозван');
 await expect(page.getByRole('heading', { name: 'Результаты октября' })).toHaveCount(0);
 await expect(page.getByRole('link', { name: 'Результат.pdf' })).toHaveCount(0);
});
