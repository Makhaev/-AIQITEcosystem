import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test('private files upload, explicit access and revocation', async ({ page }) => {
  const files: { id: number; name: string; size: number; can_manage: boolean }[] = [];
  const grants: unknown[] = [];
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'test' } });
    if (path === '/auth/me') return route.fulfill({ json: { id: 1, name: 'Педагог', email: 'teacher@example.test', roles: ['teacher'], blocked: false, must_change_password: false } });
    if (path === '/students/10/overview') return route.fulfill({ json: { id: 10, name: 'Ученик', status: 'active', full_access: false, enrollments: [], upcoming_lessons: [], attendance: { from: '2026-10-01', to: '2026-10-31', present: 0, late: 0, absent: 0, excused: 0, unmarked: 0, percent: null, items: [] } } });
    if (path === '/students/10/files') return route.fulfill({ json: { items: files, total: files.length, page: 1, page_size: 20 } });
    if (path === '/files') { const input = route.request().postDataJSON(); expect(input.student_id).toBe(10); expect(input.data).toBe(Buffer.from('Private content').toString('base64')); files.push({ id: 1, name: input.name, size: 15, can_manage: true }); return route.fulfill({ status: 201, json: files[0] }); }
    if (path === '/files/1/access') { grants.push(route.request().postDataJSON()); return route.fulfill({ status: 204 }); }
    if (path === '/files/1/revoke') { files.length = 0; return route.fulfill({ status: 204 }); }
    return route.fulfill({ status: 404, json: { message: 'Недоступно' } });
  });
  await page.goto('/#/students/10');
  await expect(page.getByText('Нет доступных приватных файлов')).toBeVisible();
  await page.getByLabel('Файл для загрузки').setInputFiles({ name: 'Материал.txt', mimeType: 'text/plain', buffer: Buffer.from('Private content') });
  await page.getByRole('button', { name: 'Загрузить приватный файл' }).click();
  await expect(page.getByRole('link', { name: 'Материал.txt' })).toHaveAttribute('href', '/api/v1/files/1/content');
  await page.getByLabel('ID сотрудника для доступа к файлу Материал.txt').fill('2');
  await page.getByRole('button', { name: 'Предоставить доступ', exact: true }).click();
  await expect(page.getByText('Права на файл обновлены.')).toBeVisible();
  expect(grants).toEqual([{ user_id: 2, allow: true }]);
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  page.on('dialog', dialog => void dialog.accept());
  await page.getByRole('button', { name: 'Отозвать файл для всех' }).click();
  await expect(page.getByText('Нет доступных приватных файлов')).toBeVisible();
});

test('group program progress shows real zero and keeps load failure distinct', async ({ page }) => {
  let state: 'ready' | 'no_plan' | 'error' = 'ready';
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'test' } });
    if (path === '/auth/me') return route.fulfill({ json: { id: 1, name: 'Руководитель', roles: ['leader'], blocked: false, must_change_password: false } });
    if (path === '/groups/1') return route.fulfill({ json: { id: 1, name: 'Группа', status: 'active', program_id: 2, program_version_id: 0, program_name: 'Программа', active_count: 0, capacity: 10, enrollments: [] } });
    if (path === '/groups/1/progress') return state === 'error' ? route.fulfill({ status: 500, json: { message: 'Ошибка расчёта' } }) : route.fulfill({ json: { state, version_number: 1, assigned_at: '2026-10-01T00:00:00+03:00', percent: state === 'ready' ? 0 : null, total_weight: 3, completed_weight: state === 'ready' ? 0 : null, elements: [] } });
    return route.fulfill({ status: 404, json: { message: 'Недоступно' } });
  });
  await page.goto('/#/groups/1');
  await expect(page.getByLabel('Прогресс программы группы')).toHaveText('0%');
  state = 'no_plan'; await page.reload();
  await expect(page.getByText('Нет данных для расчёта прогресса')).toBeVisible();
  state = 'error'; await page.reload();
  await expect(page.getByRole('button', { name: 'Повторить расчёт прогресса' })).toBeVisible();
  await expect(page.getByLabel('Прогресс программы группы')).toHaveCount(0);
  await expect(page.getByText('Нет данных для расчёта прогресса')).toHaveCount(0);
});
