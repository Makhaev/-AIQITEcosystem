import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

const empty = { state: 'ready', items: [], total: 0, page: 1, page_size: 20, from: '2026-10-01', to: '2026-10-31' };
const observation = { id: 81, student_id: 10, kind: 'observations', revision: 4, state: 'reviewed', published: true, created_at: '2026-10-10T09:00:00Z', author_id: 2, reviewer_id: 1, content: { title: 'Объяснил решение', fact: 'Сравнил два способа', interpretation: 'Нужна дополнительная практика', occurred_on: '2026-10-10', model_version: 'model-v1', skill_key: 'explain', skill_name: 'Объяснение' }, source: { id: 81, kind: 'observations', revision: 4, title: 'Объяснил решение', date: '2026-10-10', url: '#/learning/81?revision=4', outside_period: false }, sources: [{ id: 70, kind: 'projects', revision: 3, title: 'Разрешённый вклад', date: '2026-09-05', url: '#/learning/70?revision=3', outside_period: true }], files: [], basis_unavailable: true, needs_review: true, actions: [], history: [{ id: 1, revision: 4, action: 'publish', reason: 'Проверено', actor_id: 1, created_at: '2026-10-11T09:00:00Z' }] };

test('Student 360 sources, chronology, source revision and access loss', async ({ page }) => {
  let deny = false;
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'test' } });
    if (path === '/auth/me') return route.fulfill({ json: { id: 2, name: 'Педагог', email: 'teacher@example.test', roles: ['teacher'], blocked: false, must_change_password: false } });
    if (path === '/students/10/files') return route.fulfill({ json: empty });
    if (path === '/students/10/overview') return route.fulfill({ json: { id: 10, name: 'Ученик', status: 'active', full_access: false, enrollments: [], upcoming_lessons: [], attendance: { from: '2026-10-01', to: '2026-10-31', items: [], percent: null, present: 0, late: 0, absent: 0, excused: 0, unmarked: 0 }, education: { from: '2026-10-01', to: '2026-10-31', ...Object.fromEntries(['goals', 'observations', 'skills', 'assignments', 'attempts', 'projects', 'achievements', 'current_goals', 'current_assignments'].map(k => [k, empty])), observations: { ...empty, items: [observation], total: 1 }, skills: { ...empty, items: [{ ...observation, state: 'no_methodology', sources: [observation.source] }], total: 1 } } } });
    if (path === '/learning/81') return deny ? route.fulfill({ status: 404, json: { message: 'Запись недоступна.' } }) : route.fulfill({ json: observation });
    return route.fulfill({ status: 404, json: { message: 'Недоступно' } });
  });
  await page.goto('/#/students/10');
  const region = page.getByRole('region', { name: 'Образовательный контекст' });
  await expect(region.getByText('Наблюдаемый факт:', { exact: true })).toBeVisible();
  await expect(region.getByText('Трактовка:', { exact: true })).toBeVisible();
  await expect(region.getByText('Основание недоступно для просмотра.')).toBeVisible();
  await expect(region.getByText(/Вне выбранного периода/)).toBeVisible();
  await expect(region.getByText(/Уровень не определён/)).toBeVisible();
  await expect(region).not.toContainText('0%');
  await expect(page.getByText('Возраст (полных лет)')).toHaveCount(0);
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await region.getByRole('link', { name: 'Объяснил решение', exact: true }).first().click();
  await expect(page.getByText(/Сохранённое содержимое редакции 4/)).toBeVisible();
  await expect(page.getByRole('heading', { name: 'История решений' })).toBeVisible();
  deny = true; await page.reload();
  await expect(page.getByRole('alert')).toContainText('Запись недоступна');
  await expect(page.getByText('Сравнил два способа', { exact: false })).toHaveCount(0);
});

test('education block failure is distinct from an empty result and retries independently', async ({ page }) => {
  let overviewCalls = 0, retries = 0;
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'test' } });
    if (path === '/auth/me') return route.fulfill({ json: { id: 1, name: 'Сотрудник', roles: ['admin'], blocked: false, must_change_password: false } });
    if (path === '/students/10/files') return route.fulfill({ json: empty });
    if (path === '/students/10/overview') { overviewCalls++; return route.fulfill({ json: { id: 10, name: 'Ученик', status: 'active', full_access: false, enrollments: [], upcoming_lessons: [], attendance: { from: '2026-10-01', to: '2026-10-31', items: [], percent: null, present: 0, late: 0, absent: 0, excused: 0, unmarked: 0 }, education: { from: '2026-10-01', to: '2026-10-31', ...Object.fromEntries(['goals', 'observations', 'skills', 'assignments', 'attempts', 'projects', 'achievements', 'current_goals', 'current_assignments'].map(k => [k, empty])), projects: { state: 'error' } } } }); }
    if (path === '/students/10/education/projects') { retries++; return route.fulfill({ json: empty }); }
    return route.fulfill({ status: 404, json: { message: 'Недоступно' } });
  });
  await page.goto('/#/students/10');
  await expect(page.getByRole('button', { name: 'Повторить загрузку: Проекты' })).toBeVisible();
  await expect(page.getByText('Нет доступных записей за период', { exact: true })).toHaveCount(6);
  await page.getByRole('button', { name: 'Повторить загрузку: Проекты' }).click();
  await expect(page.getByText('Нет доступных записей за период', { exact: true })).toHaveCount(7);
  expect(overviewCalls).toBe(1); expect(retries).toBe(1);
});
