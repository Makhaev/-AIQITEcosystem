import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test('student HP follows accepted work, handles empty data and retry, stays private', async ({ page }, info) => {
  let role = 'student'; let reviewed = 1; let total = 2; let failure = false;
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    const json = (body: unknown) => route.fulfill({ json: body });
    if (path === '/features') return json({});
    if (path === '/auth/csrf') return json({ csrf_token: 'fixture' });
    if (path === '/auth/me') return json({ id: 7, name: 'Ученик AIQ', email: 'student@example.test', roles: [role], must_change_password: false });
    if (path === '/learning/progress') {
      if (failure) return route.fulfill({ status: 500, json: { message: 'Не удалось загрузить игровой прогресс.' } });
      return json({ items: [{ student_id: 10, student_name: 'Ученик AIQ', total, reviewed, assigned: total - reviewed, submitted: 0 }] });
    }
    if (path === '/learning/students') return json({ items: [{ id: 10, name: 'Ученик AIQ' }] });
    return json({ items: [], total: 0 });
  });
  await page.goto('/#/portal');
  const bar = page.getByRole('progressbar', { name: 'Прогресс заданий: Ученик AIQ' });
  await expect(bar).toHaveAttribute('aria-valuenow', '50');
  await expect(page.getByText('50 / 100 HP', { exact: true })).toBeVisible();
  await expect(page.getByText('Принято педагогом: 1 из 2.', { exact: false })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
  await page.screenshot({ path: `../.cache/student-game-progress-${info.project.name}.png`, fullPage: true });
  await page.getByRole('link', { name: 'К следующему заданию →' }).click();
  await expect(page.getByRole('heading', { name: 'Задания', exact: true })).toBeVisible();
  await expect(bar).toHaveAttribute('aria-valuenow', '50');
  reviewed = 2; await page.reload();
  await expect(bar).toHaveAttribute('aria-valuenow', '100');
  await expect(page.getByText('Все задания пройдены!', { exact: true })).toBeVisible();
  reviewed = 0; total = 0; await page.reload();
  await expect(bar).toHaveAttribute('aria-valuetext', 'Заданий пока нет');
  await expect(page.getByText('Старт приключения', { exact: true })).toBeVisible();
  await expect(page.getByText('Все задания пройдены!', { exact: true })).toHaveCount(0);
  failure = true; await page.reload();
  await expect(page.getByRole('alert')).toContainText('Не удалось загрузить игровой прогресс.');
  failure = false; await page.getByRole('button', { name: 'Обновить прогресс' }).click();
  await expect(bar).toBeVisible();
  role = 'parent'; await page.goto('/#/portal'); await page.reload();
  await expect(page.getByRole('heading', { name: 'Семейный кабинет', exact: true })).toBeVisible();
  await expect(page.getByRole('progressbar')).toHaveCount(0);
});
