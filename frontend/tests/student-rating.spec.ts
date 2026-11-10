import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test('admin sees student ranking and activity instead of the test editor', async ({ page }, info) => {
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url());
    const path = url.pathname.replace('/api/v1', '');
    const json = (body: unknown) => route.fulfill({ json: body });
    if (path === '/auth/me') return json({ id: 1, name: 'Администратор AIQ', email: 'admin@example.test', roles: ['admin'], must_change_password: false });
    if (path === '/auth/csrf') return json({ csrf_token: 'fixture' });
    if (path === '/features') return json({});
    if (path === '/learning/rating') return json({ items: url.searchParams.get('search') ? [] : [{ id: 10, name: 'Иванов Иван Иванович', rank: 1, progress_marks: 3, completed_lessons: 1, accepted_assignments: 1, submitted_assignments: 0, completed_tests: 1, failed_tests: 1, pending: 2, last_activity: '2026-11-08T10:00:00Z' }], total: url.searchParams.get('search') ? 0 : 1 });
    return json({ items: [], total: 0 });
  });
  await page.goto('/#/homework-tests');
  await expect(page.getByRole('heading', { name: 'Рейтинг учеников', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Создать тест', exact: true })).toHaveCount(0);
  await expect(page.getByRole('link', { name: 'Иванов Иван Иванович', exact: true })).toBeVisible();
  await expect(page.getByText('#1', { exact: true })).toBeVisible();
  await expect(page.getByText('1 посещено', { exact: true })).toBeVisible();
  await expect(page.getByText('1 принято', { exact: true })).toBeVisible();
  await expect(page.getByText('1 пройдено', { exact: true })).toBeVisible();
  await expect(page.getByText('1 с ошибками', { exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
  if (info.project.name.includes('mobile')) await page.getByRole('button', { name: 'Меню', exact: true }).click();
  const nav = info.project.name.includes('mobile') ? page.getByRole('dialog') : page.locator('.sidebar');
  await expect(nav.getByRole('link', { name: 'Рейтинг учеников', exact: true })).toBeVisible();
  await expect(nav.getByRole('link', { name: 'Тесты и ДЗ', exact: true })).toHaveCount(0);
  await nav.getByRole('link', { name: 'Рейтинг учеников', exact: true }).click();
  await page.getByLabel('Поиск ученика по ФИО').fill('Нет такого');
  await expect(page.getByRole('heading', { name: 'Ученики не найдены', exact: true })).toBeVisible();
});
