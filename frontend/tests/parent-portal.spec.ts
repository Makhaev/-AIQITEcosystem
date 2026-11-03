import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

const record = (id: number, child: string, kind: string) => ({ id, student_id: id, kind, state: kind === 'attempts' ? 'accepted' : 'reviewed', revision: 1, content: { title: `${child}: ${kind === 'attempts' ? 'работа' : 'результат'}`, fact: `${child}: разрешённая обратная связь` }, files: [], sources: [] });
function home(id: number, emptyPeriod = false) {
 const child = id === 7 ? 'Анна' : 'Борис';
 return {
  programs: [{ title: `${child}: программа`, version: 2, percent: 50 }],
  blocks: { observations: [record(id, child, 'observations')], attempts: [record(id, child, 'attempts')] }, totals: { observations: 1, attempts: 1 },
  parent: {
   attendance: { from: emptyPeriod ? '2020-01-01' : '2026-10-01', to: emptyPeriod ? '2020-01-31' : '2026-10-31', present: emptyPeriod ? 0 : 2, late: 0, absent: emptyPeriod ? 0 : 1, excused: 0, total: emptyPeriod ? 0 : 3, percent: emptyPeriod ? null : 200 / 3 },
   lessons: [{ id, topic: `${child}: занятие`, starts_at: '2099-10-28T12:00:00Z', ends_at: '2099-10-28T13:00:00Z' }],
   reports: [{ id, title: `${child}: отчёт`, body: `${child}: итоги месяца`, published: true, files: [{ id, name: `${child}.pdf`, url: `/api/v1/family/reports/${id}/files/${id}` }] }],
   events: [{ id, kind: 'assignment', title: `${child}: срок задания`, due_on: '2099-10-29' }],
   requests: [{ id, subject: `${child}: вопрос`, body: 'Мой вопрос куратору', reply: `${child}: ответ` }], totals: { lessons: 1, reports: 1, events: 1, requests: 1 },
  },
 };
}

test('parent home resets every child block, ignores late responses, and clears revoked data', async ({ page }, info) => {
 let holdSecond = true; let revoked = false; let requestedSecond!: () => void; let releaseSecond!: () => void;
 const secondRequested = new Promise<void>(resolve => { requestedSecond = resolve; });
 const secondRelease = new Promise<void>(resolve => { releaseSecond = resolve; });
 const errors: string[] = [];
 page.on('pageerror', e => errors.push(e.message));
 await page.route('**/api/v1/**', async route => {
  const url = new URL(route.request().url()); const p = url.pathname.replace('/api/v1', '');
  if (p === '/auth/me') return route.fulfill({ json: { id: 2, name: 'Родитель', email: 'parent@example.test', roles: ['parent'], must_change_password: false, blocked: false } });
  if (p === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'csrf' } });
  if (p === '/family/students') return route.fulfill({ json: { items: [{ id: 7, name: 'Анна' }, { id: 8, name: 'Борис' }], total: 2 } });
  const match = p.match(/^\/family\/students\/(7|8)\/(.+)$/);
  if (match) {
   const id = Number(match[1]);
   if (revoked) return route.fulfill({ status: 404, json: { message: 'Данные не найдены или доступ отозван.' } });
   if (match[2] === 'parent-home') {
    if (id === 8 && holdSecond) { requestedSecond(); await secondRelease; }
    return route.fulfill({ json: home(id, url.searchParams.get('from') === '2020-01-01') });
   }
   if (match[2] === 'requests') return route.fulfill({ json: { items: [], total: 0 } });
   if (match[2] === 'events') return route.fulfill({ json: { items: home(id).parent.events, total: 1 } });
  }
  return route.fulfill({ status: 404, json: { message: 'Недоступно' } });
 });
 await page.goto('/');
 await expect(page.getByRole('heading', { name: 'Кабинет родителя' })).toBeVisible();
 await expect(page.getByLabel('Ученик', { exact: true })).toHaveValue('7');
 for (const block of ['занятие', 'программа · версия 2', 'результат', 'работа', 'отчёт', 'срок задания', 'вопрос']) await expect(page.getByRole('heading', { name: `Анна: ${block}`, exact: true })).toBeVisible();
 await expect(page.getByText('Посещаемость: 66,7%')).toBeVisible();
 expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
 await page.screenshot({ path: info.outputPath('parent-home.png'), fullPage: true });
 await page.getByLabel('Период с', { exact: true }).fill('2020-01-01');
 await page.getByLabel('Период по', { exact: true }).fill('2020-01-31');
 await page.getByRole('button', { name: 'Показать посещаемость' }).click();
 await expect(page.getByText('Нет данных о посещаемости за этот период.')).toBeVisible();
 await page.getByLabel('Ученик', { exact: true }).selectOption('8');
 await secondRequested;
 await expect(page.getByRole('status')).toContainText('Загрузка');
 await expect(page.getByText(/Анна:/)).toHaveCount(0);
 await expect(page.getByRole('link', { name: 'Анна.pdf' })).toHaveCount(0);
 // A response arriving after switching away must never overwrite the current child.
 await page.getByLabel('Ученик', { exact: true }).selectOption('7');
 await expect(page.getByRole('heading', { name: 'Анна: отчёт', exact: true })).toBeVisible();
 holdSecond = false; releaseSecond();
 await expect(page.getByText(/Борис:/)).toHaveCount(0);
 await expect(page.getByLabel('Период с', { exact: true })).toHaveValue('2026-10-01');
 await page.getByLabel('Раздел обучения').selectOption('requests');
 await page.getByLabel('Тема', { exact: true }).fill('Черновик Анны');
 await page.getByLabel('Текст обращения').fill('Не переносить другому ребёнку');
 await page.getByLabel('Ученик', { exact: true }).selectOption('8');
 await expect(page.getByRole('heading', { name: 'Борис: отчёт', exact: true })).toBeVisible();
 await expect(page.getByText(/Анна:/)).toHaveCount(0);
 await page.getByLabel('Раздел обучения').selectOption('requests');
 await expect(page.getByLabel('Тема', { exact: true })).toHaveValue('');
 await expect(page.getByLabel('Текст обращения')).toHaveValue('');
 await page.getByLabel('Раздел обучения').selectOption('home');
 await expect(page.getByRole('heading', { name: 'Борис: отчёт', exact: true })).toBeVisible();
 revoked = true;
 await page.getByRole('button', { name: 'Обновить главную', exact: true }).click();
 await expect(page.getByRole('alert')).toContainText('доступ отозван');
 await expect(page.getByText(/Борис:/)).toHaveCount(0);
 await expect(page.getByRole('link', { name: 'Борис.pdf' })).toHaveCount(0);
 expect(errors).toEqual([]);
});

test('empty parent account stays on a clear empty screen', async ({ page }) => {
 await page.route('**/api/v1/**', route => {
  const p = new URL(route.request().url()).pathname;
  if (p.endsWith('/auth/csrf')) return route.fulfill({ json: { csrf_token: 'csrf' } });
  if (p.endsWith('/auth/me')) return route.fulfill({ json: { id: 2, name: 'Родитель', roles: ['parent'], must_change_password: false } });
  if (p.endsWith('/family/students')) return route.fulfill({ json: { items: [], total: 0 } });
  return route.fulfill({ status: 404, json: {} });
 });
 await page.goto('/');
 await expect(page.getByText('Нет доступных учеников.', { exact: false })).toBeVisible();
 await expect(page.getByLabel('Раздел обучения')).toHaveCount(0);
});
