import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

async function fixture(page: Page) {
  let role = 'teacher'; let linked = true; let fail = false;
  const paths: string[] = [];
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()); const path = url.pathname.replace('/api/v1', ''); paths.push(url.pathname + url.search);
    const json = (value: unknown, status = 200) => route.fulfill({ json: value, status });
    if (path === '/features') return json({ student360: true });
    if (path === '/auth/csrf') return json({ csrf_token: 'fixture' });
    if (path === '/auth/me') return json({ id: 2, name: 'Педагог AIQ', email: 'teacher@example.test', roles: [role], blocked: false, must_change_password: false });
    if (path === '/teacher/workspace') {
      if (fail) return json({ code: 'internal_error', message: 'Не удалось загрузить кабинет.' }, 500);
      return json({ profile: linked ? { id: 3, name: 'Педагог AIQ', specialization: 'Коммуникация', status: 'active' } : null, counts: { groups: 2, students: 3, lessons_today: 1, pending_reviews: 2, draft_reports: 1 }, upcoming_lessons: [{ id: 8, topic: 'Аргументация', group_id: 4, group_name: 'Моя группа', starts_at: '2026-11-05T09:00:00Z', ends_at: '2026-11-05T10:00:00Z', location: 'Кабинет 3', program_name: 'Мышление', module_name: 'Обсуждение' }] });
    }
    if (path === '/teacher/groups') {
      const filtered = !!url.searchParams.get('search');
      return json({ items: filtered ? [] : [{ id: 4, name: 'Моя группа', program_name: 'Мышление', status: 'active', primary_teacher: true, student_count: 3 }], total: filtered ? 0 : 21, page: Number(url.searchParams.get('page')), page_size: 20 });
    }
    if (path === '/teacher/students') {
      const group = url.searchParams.get('group_id');
      if (group === '999') return json({ code: 'not_found', message: 'Группа не найдена или не относится к вашим занятиям.' }, 404);
      return json({ items: [{ id: 10, name: 'Ученик моей группы', status: 'active', groups: [{ id: 4, name: 'Моя группа' }] }], total: 1, page: 1, page_size: 20, group: group ? { id: 4, name: 'Моя группа' } : null });
    }
    return json({ code: 'not_found', message: 'Нет данных.' }, 404);
  });
  return { paths, unlinked: () => { linked = false; }, fail: () => { fail = true; }, recover: () => { fail = false; }, role: (value: string) => { role = value; } };
}
async function accessible(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
}
test('personal overview, groups, pupil filters and accessible mobile navigation', async ({ page }) => {
  const api = await fixture(page);
  await page.goto('/#/teaching');
  await expect(page.getByRole('heading', { name: 'Кабинет педагога', exact: true })).toBeVisible();
  await expect(page.getByLabel('Мои группы', { exact: true })).toHaveText('2');
  await expect(page.getByLabel('Мои ученики', { exact: true })).toHaveText('3');
  await expect(page.getByText('05.11.2026, 12:00', { exact: false })).toBeVisible();
  await expect(page.getByRole('link', { name: 'Открыть журнал' })).toHaveAttribute('href', '#/lessons/8');
  await expect(page.getByRole('link', { name: 'Моя группа', exact: true })).toHaveAttribute('href', '#/my-groups/4');
  await accessible(page);
  await page.goto('/#/my-groups'); await expect(page.getByRole('table', { name: 'Мои группы', exact: true })).toContainText('Моя группа');
  await page.getByRole('button', { name: 'Следующая страница' }).click(); await expect.poll(() => api.paths.some(path => path.includes('/teacher/groups?page=2'))).toBe(true);
  await page.getByLabel('Поиск моей группы').fill('Нет совпадений'); await page.getByRole('button', { name: 'Применить', exact: true }).click(); await expect(page.getByRole('heading', { name: 'Группы не найдены' })).toBeVisible();
  await page.goto('/#/my-groups/4');
  await expect(page.getByRole('heading', { name: 'Ученики: Моя группа' })).toBeVisible();
  await expect(page.getByRole('article', { name: 'Ученик моей группы', exact: true })).toContainText('Ученик моей группы');
  await expect(page.getByRole('link', { name: 'Ученик моей группы', exact: true })).toHaveAttribute('href', '#/students/10');
  await expect(page.getByRole('button', { name: 'Добавить ученика', exact: true })).toHaveCount(0);
  await accessible(page);
  if (page.viewportSize()!.width < 1024) {
    await page.getByRole('button', { name: 'Меню', exact: true }).click();
    await expect(page.getByRole('dialog').getByRole('link', { name: 'Мои ученики', exact: true })).toBeVisible();
    await expect(page.getByRole('dialog').getByRole('link', { name: 'Мои группы', exact: true })).toBeVisible();
    await page.getByRole('dialog').getByRole('link', { name: 'Кабинет педагога', exact: true }).click();
    await expect(page.getByRole('dialog')).toHaveCount(0);
  }
});
test('unlinked profile, retry, foreign group and denied roles are explicit', async ({ page }) => {
  const api = await fixture(page); api.unlinked();
  await page.goto('/#/teaching'); await expect(page.getByRole('heading', { name: 'Профиль педагога пока не связан с аккаунтом' })).toBeVisible();
  await expect(page.getByLabel('Мои группы', { exact: true })).toHaveCount(0); await accessible(page);
  api.fail(); await page.reload(); await expect(page.getByRole('alert')).toContainText('Не удалось загрузить кабинет.');
  api.recover(); await page.getByRole('button', { name: 'Повторить загрузку' }).click(); await expect(page.getByRole('heading', { name: 'Профиль педагога пока не связан с аккаунтом' })).toBeVisible();
  await page.goto('/#/my-groups/999'); await expect(page.getByRole('alert')).toContainText('Группа не найдена'); await expect(page.getByRole('table')).toHaveCount(0);
  api.role('parent'); await page.reload(); await expect(page.getByRole('heading', { name: 'Раздел недоступен' })).toBeVisible();
});
