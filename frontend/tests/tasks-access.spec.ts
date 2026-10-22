import { expect, test, type Page } from '@playwright/test';

async function fixture(page: Page, manager = false) {
  const state = { visibility: 'allowed', staleList: false, requests: [] as string[], saves: [] as Record<string, unknown>[] };
  const task = { id: 1, title: 'Подготовить материалы', description: 'Передать рабочие листы.', creator_id: 3, creator_name: 'Руководитель', assignee_id: 2, assignee_name: 'Педагог', due_at: '2026-10-15T09:00:00Z', status: 'todo', overdue: false, closed_at: null, object_type: 'student', object_id: 10, object_name: 'Связанный ученик' };
  const projected = () => ({ ...task, ...(state.visibility === 'hidden' ? { object_type: null, object_id: null, object_name: null } : {}) });
  await page.clock.install({ time: new Date('2026-10-14T09:00:00Z') });
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    const method = route.request().method(); state.requests.push(`${method} ${path}`);
    const json = (body: unknown, status = 200) => route.fulfill({ status, json: body });
    if (path === '/auth/csrf') return json({ csrf_token: 'test' });
    if (path === '/auth/me') return json({ id: manager ? 3 : 2, name: manager ? 'Руководитель' : 'Педагог', email: 'staff@example.test', roles: [manager ? 'leader' : 'teacher'], blocked: false, must_change_password: false });
    if (path === '/tasks/assignees') return json({ items: [{ id: 2, name: 'Педагог', blocked: false }], total: 1, page: 1, page_size: 100 });
    if (path === '/students') return json({ items: [{ id: 10, name: task.object_name }], total: 1, page: 1, page_size: 20 });
    if (path === '/tasks' && method === 'GET') return json({ items: [state.staleList ? task : projected()], total: 1, page: 1, page_size: 20 });
    if (path === '/tasks' && method === 'POST') {
      state.saves.push(route.request().postDataJSON());
      return json({ code: 'assignee_object_access_denied', message: 'Ответственный не имеет доступа к выбранной записи.', field_errors: { assignee_id: 'Выберите сотрудника с доступом к связанному объекту.' }, request_id: 'test' }, 400);
    }
    if (path === '/tasks/1/status') { task.status = route.request().postDataJSON().status; return json(projected()); }
    if (path === '/tasks/1' && state.visibility !== 'denied') return json(projected());
    return json({ code: 'not_found', message: 'Задача не найдена.', field_errors: {}, request_id: 'test' }, 404);
  });
  return state;
}

test('opening a task rechecks its projection without fetching the linked card', async ({ page }) => {
  const state = await fixture(page);
  await page.goto('/#/tasks');
  await expect(page.getByRole('button', { name: 'Подготовить материалы', exact: true })).toBeVisible();
  state.visibility = 'hidden'; state.staleList = true;
  await page.getByRole('button', { name: 'Подготовить материалы', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('Передать рабочие листы.');
  await expect(dialog).toContainText('Без связи');
  await expect(dialog.getByRole('link')).toHaveCount(0);
  await expect(dialog).not.toContainText('Связанный ученик');
  expect(state.requests).toContain('GET /tasks/1');
  expect(state.requests.filter(path => /\/(students|parents|groups|lessons|audit-logs)/.test(path))).toEqual([]);
});

test('an open task removes a revoked link on refresh and remains actionable', async ({ page }) => {
  const state = await fixture(page);
  await page.goto('/#/tasks');
  await page.getByRole('button', { name: 'Подготовить материалы', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('link', { name: 'Ученик: Связанный ученик · №10' })).toHaveAttribute('href', '#/students/10');
  state.visibility = 'hidden'; state.staleList = true;
  await page.clock.runFor(30_000);
  await expect(dialog).toContainText('Без связи');
  await expect(dialog.getByRole('link')).toHaveCount(0);
  await expect(dialog).not.toContainText('Связанный ученик');
  await dialog.getByRole('button', { name: 'В работу', exact: true }).click();
  await page.getByRole('button', { name: 'Подготовить материалы', exact: true }).click();
  await expect(dialog).toContainText('В работе');
  await expect(dialog).toContainText('Без связи');
  expect(state.requests.filter(path => /\/(students|parents|groups|lessons|audit-logs)/.test(path))).toEqual([]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('task access denial clears cached detail and permits a fresh retry', async ({ page }) => {
  const state = await fixture(page);
  await page.goto('/#/tasks');
  await page.getByRole('button', { name: 'Подготовить материалы', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('link')).toHaveCount(1);
  state.visibility = 'denied';
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(dialog.getByRole('alert')).toContainText('Задача не найдена.');
  await expect(dialog.getByRole('link')).toHaveCount(0);
  await expect(dialog).not.toContainText('Передать рабочие листы.');
  await expect(dialog.getByRole('button', { name: 'В работу', exact: true })).toHaveCount(0);
  state.visibility = 'hidden';
  await dialog.getByRole('button', { name: 'Повторить загрузку задачи' }).click();
  await expect(dialog).toContainText('Без связи');
  await expect(dialog).toContainText('Передать рабочие листы.');
});

test('assignment denial is shown at assignee and object selection preserves description', async ({ page }) => {
  const state = await fixture(page, true);
  await page.goto('/#/tasks');
  await page.getByRole('button', { name: 'Добавить: задача' }).click();
  const dialog = page.getByRole('dialog', { name: 'Новая задача' });
  await dialog.getByLabel('Название', { exact: true }).fill('Подготовка');
  await dialog.getByLabel('Ожидаемый результат').fill('Текст постановщика');
  await dialog.getByLabel('Ответственный', { exact: true }).selectOption('2');
  await dialog.getByLabel('Срок (МСК)').fill('2026-10-15T12:00');
  await dialog.getByLabel('Связь с записью').selectOption('student');
  await page.clock.runFor(250);
  await dialog.getByLabel('Связанная запись', { exact: true }).selectOption('10');
  await expect(dialog.getByLabel('Ожидаемый результат')).toHaveValue('Текст постановщика');
  await dialog.getByRole('button', { name: 'Сохранить задачу' }).click();
  await expect(dialog.getByRole('alert')).toContainText('Ответственный не имеет доступа');
  await expect(dialog.getByLabel('Ответственный', { exact: true })).toHaveAttribute('aria-invalid', 'true');
  await expect(dialog.getByText('Выберите сотрудника с доступом к связанному объекту.', { exact: true })).toBeVisible();
  expect(state.saves).toHaveLength(1);
  expect(state.saves[0]).toMatchObject({ assignee_id: 2, object_type: 'student', object_id: 10, description: 'Текст постановщика' });
  expect(state.requests.filter(path => /\/(students\/|parents|audit-logs)/.test(path))).toEqual([]);
});
