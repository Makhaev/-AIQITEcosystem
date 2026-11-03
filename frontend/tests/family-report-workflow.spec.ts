import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test('assigned education leader returns with remarks, approves and separately publishes', async ({ page }, info) => {
 let report: any = { id: 4, student_id: 7, title: 'Отчёт за октябрь', body: 'Анна объясняет алгоритм.', revision: 2, version: 1, state: 'review', actions: ['return', 'approve'], published: false, files: [], decisions: [] };
 let returned = false, conflict = true;
 await page.route('**/api/v1/**', route => {
  const p = new URL(route.request().url()).pathname.replace('/api/v1', '');
  if (p === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'test-csrf' } });
  if (p === '/auth/me') return route.fulfill({ json: { id: 2, name: 'Проверяющий', email: 'reviewer@example.test', roles: ['education_leader'], blocked: false, must_change_password: false } });
  if (p === '/family-reports') return route.fulfill({ json: { items: returned ? [] : [report], total: returned ? 0 : 1 } });
  if (p === '/family-reports/4/actions') {
   const body = route.request().postDataJSON(); expect(body.version).toBe(1); expect(body.revision).toBe(report.revision);
   if (body.action === 'return' && conflict) { conflict = false; return route.fulfill({ status: 409, json: { message: 'Запись изменилась. Обновите отчёт.' } }); }
   if (body.action === 'return') { expect(body.reason).toBe('Уточните образовательный фокус.'); returned = true; report.state = 'draft'; report.actions = []; }
   if (body.action === 'approve') { report.state = 'approved'; report.actions = ['publish']; }
   if (body.action === 'publish') { report.state = 'published'; report.actions = ['withdraw']; report.published_version = 1; report.published = true; }
   report = { ...report, revision: report.revision + 1, decisions: [...report.decisions, { version: 1, action: body.action, reason: body.reason, actor_name: 'Проверяющий', created_at: '2026-10-28T09:00:00Z' }] };
   return route.fulfill({ json: report });
  }
  return route.fulfill({ status: 403, json: { message: 'Нет доступа' } });
 });
 await page.goto('/#/family-reports');
 await expect(page.getByRole('heading', { name: 'Отчёт за октябрь · На проверке' })).toBeVisible();
 await expect(page.getByRole('link', { name: 'Ученики', exact: true })).toHaveCount(0);
 await expect(page.getByRole('button', { name: 'Изменить отчёт' })).toHaveCount(0);
 await expect(page.getByRole('button', { name: 'Опубликовать семье' })).toHaveCount(0);
 await page.getByRole('button', { name: 'Вернуть на доработку' }).click();
 await expect(page.getByRole('alert')).toContainText('Укажите замечание');
 await page.getByLabel('Замечание или причина — отчёт №4').fill('Уточните образовательный фокус.');
 await page.getByRole('button', { name: 'Вернуть на доработку' }).click();
 await expect(page.getByRole('alert')).toContainText('Запись изменилась');
 await expect(page.getByLabel('Замечание или причина — отчёт №4')).toHaveValue('Уточните образовательный фокус.');
 await page.getByRole('button', { name: 'Вернуть на доработку' }).click();
 await expect(page.getByText('Нет доступных отчётов.', { exact: false })).toBeVisible();
 // Curator revises and resubmits outside this reviewer's session.
 returned = false; report = { ...report, revision: report.revision + 2, state: 'review', actions: ['return', 'approve'] };
 await page.getByRole('button', { name: 'Обновить отчёты' }).click();
 await page.getByText('История переходов и замечания', { exact: true }).click();
 await expect(page.getByText('Уточните образовательный фокус.', { exact: true })).toBeVisible();
 await page.getByRole('button', { name: 'Утвердить', exact: true }).click();
 await expect(page.getByRole('heading', { name: 'Отчёт за октябрь · Утверждён' })).toBeVisible();
 await page.getByRole('button', { name: 'Опубликовать семье' }).click();
 await expect(page.getByText('Семье доступна версия 1.')).toBeVisible();
 await expect(page.getByRole('button', { name: 'Утвердить', exact: true })).toHaveCount(0);
 expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
 await page.screenshot({ path: info.outputPath('family-report-review.png'), fullPage: true });
});
