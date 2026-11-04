import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test('build, edit and submit a structured family report without AI', async ({ page }, info) => {
 let report: any = null;
 let rejectSave = true;
 await page.route('**/api/v1/**', route => {
  const p = new URL(route.request().url()).pathname.replace('/api/v1', '');
  if (p === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'test-csrf' } });
  if (p === '/auth/me') return route.fulfill({ json: { id: 1, name: 'Администратор', email: 'admin@example.test', roles: ['admin'], blocked: false, must_change_password: false } });
  if (p === '/students/7/overview') return route.fulfill({ json: { id: 7, name: 'Анна', status: 'active', full_access: true, parents: [] } });
  if (p === '/students/7/files' || p === '/students/7/family-requests' || p === '/students/7/notes') return route.fulfill({ json: { items: [], total: 0 } });
  if (p === '/students/7/family-access') return route.fulfill({ json: [] });
  if (p === '/students/7/family-reports' && route.request().method() === 'GET') return route.fulfill({ json: { items: report ? [report] : [], total: report ? 1 : 0 } });
  if (p === '/students/7/family-reports') {
   const body = route.request().postDataJSON(); expect(body.generate).toBe(true); expect(body.from).toBe('2026-10-01'); expect(body.to).toBe('2026-10-28');
   report = { id: 4, title: 'Отчёт за октябрь', body: 'Посещаемость: 75%. Принятых работ: 1.', revision: 1, published: false, files: [], author_name: 'Куратор', created_at: '2026-10-28T09:00:00Z', updated_at: '2026-10-28T09:00:00Z', ...body,
    snapshot: { student_name: 'Анна', from: body.from, to: body.to, collected_at: '2026-10-28T09:00:00Z', attendance: { present: 2, late: 1, absent: 1, excused: 0, total: 4, percent: 75 }, elements: [{ assignment_id: 1, template_key: 'loops', title: 'Циклы' }], works: [{ id: 9, revision: 2, kind: 'attempts', title: 'Задача о циклах', date: '2026-10-25' }] } };
   report.title = 'Отчёт за октябрь'; report.body = 'Посещаемость: 75%. Принятых работ: 1.'; report.revision = 1;
   report.version = 1; report.state = 'draft'; report.actions = ['edit', 'submit'];
   return route.fulfill({ status: 201, json: report });
  }
  if (p === '/family-reports/4') {
   if (rejectSave) { rejectSave = false; return route.fulfill({ status: 409, json: { message: 'Запись изменилась. Проверьте данные.' } }); }
   const body = route.request().postDataJSON(); expect(body.generate).toBe(false); expect(body.reviewer_id).toBe(2); expect(body.revision).toBe(report.revision);
   report = { ...report, ...body, revision: report.revision + 1, reviewer_name: 'Администратор', published: false }; return route.fulfill({ json: report });
  }
  if (p === '/family-reports/4/actions') { const body = route.request().postDataJSON(); expect(body.action).toBe('submit'); expect(body.version).toBe(1); report = { ...report, revision: report.revision + 1, state: 'review', actions: ['return'] }; return route.fulfill({ json: report }); }
  return route.fulfill({ status: 404, json: { message: 'Недоступно' } });
 });
 await page.goto('/#/students/7');
 await page.getByRole('button', { name: 'Открыть управление' }).click();
 await page.getByLabel('Начало периода отчёта').fill('2026-10-01');
 await page.getByLabel('Окончание периода отчёта').fill('2026-10-28');
 await page.getByRole('button', { name: 'Собрать черновик без ИИ' }).click();
 await expect(page.getByText('Циклы', { exact: true })).toBeVisible();
 await expect(page.getByText('Ученик: Анна.', { exact: false })).toBeVisible();
 await page.getByLabel('Текст для семьи', { exact: true }).fill('Анна уверенно объясняет циклы.');
 await page.getByLabel('Следующий образовательный фокус', { exact: true }).fill('Самостоятельно составить алгоритм.');
 await page.getByLabel('ID проверяющего').fill('2');
 await page.getByRole('button', { name: 'Сохранить черновик', exact: true }).click();
 await expect(page.getByRole('alert')).toContainText('Запись изменилась');
 await expect(page.getByLabel('Текст для семьи', { exact: true })).toHaveValue('Анна уверенно объясняет циклы.');
 await page.getByRole('button', { name: 'Сохранить черновик', exact: true }).click();
 await expect(page.locator('p').filter({ hasText: /^Анна уверенно объясняет циклы\.$/ })).toBeVisible();
 await page.getByRole('button', { name: 'Завершить редактирование' }).click();
 await expect(page.getByRole('button', { name: 'Опубликовать семье' })).toHaveCount(0);
 await page.getByRole('button', { name: 'Отправить на проверку' }).click();
 await expect(page.getByRole('heading', { name: 'Отчёт за октябрь · На проверке' })).toBeVisible();
 await expect(page.getByRole('button', { name: 'Утвердить', exact: true })).toHaveCount(0);
 expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
 await page.screenshot({ path: info.outputPath('family-report-composition.png'), fullPage: true });
});
