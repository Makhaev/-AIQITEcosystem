import { expect, test } from '@playwright/test';

test('curators directory opens activity and filters by name', async ({ page }) => {
  const curator = { id: 7, name: 'Иванов Иван Иванович', email: 'curator@example.test', blocked: false, groups_count: 1, students_count: 2, reports_count: 1, published_count: 0, last_activity: '2026-11-08T08:00:00Z' };
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url());
    const path = url.pathname.replace('/api/v1', '');
    if (path === '/auth/me') return route.fulfill({ json: { id: 1, name: 'Администратор', email: 'admin@example.test', roles: ['admin'], must_change_password: false } });
    if (path === '/features') return route.fulfill({ json: {} });
    if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'fixture' } });
    if (path === '/curators') return route.fulfill({ json: { items: url.searchParams.get('search') ? [] : [curator], total: url.searchParams.get('search') ? 0 : 1 } });
    if (path === '/curators/7') return route.fulfill({ json: { ...curator, groups: [{ id: 3, name: 'Группа развития', status: 'active' }], recent_reports: [{ id: 9, student_name: 'Ученик AIQ', summary: 'Учебный прогресс за месяц', updated_at: curator.last_activity, published_at: null }] } });
    return route.fulfill({ json: { items: [], total: 0 } });
  });
  await page.goto('/#/curator-reports');
  await expect(page.getByRole('heading', { name: 'Кураторы', exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: curator.name, exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'Посмотреть активность' }).click();
  await expect(page.getByRole('heading', { name: curator.name, exact: true })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Группа развития' })).toBeVisible();
  await expect(page.getByText('Учебный прогресс за месяц', { exact: true })).toBeVisible();
  await page.getByRole('link', { name: '← Все кураторы' }).click();
  await page.getByLabel('Поиск по ФИО или email').fill('Другой');
  await expect(page.getByRole('heading', { name: 'Кураторы не найдены' })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
});
