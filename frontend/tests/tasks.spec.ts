import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

async function fixture(page: Page, role = 'leader') {
  const user = { id: role === 'teacher' ? 2 : 1, name: role === 'teacher' ? 'Педагог' : 'Руководитель', email: 'staff@example.test', roles: [role], blocked: false, must_change_password: false };
  const people = [{ id: 1, name: 'Руководитель', blocked: false }, { id: 2, name: 'Педагог', blocked: false }, { id: 3, name: 'Куратор', blocked: false }, { id: 4, name: 'Заблокированный', blocked: true }];
  const base = { id: 1, title: 'Подготовить материалы', description: 'Передать рабочие листы\nПроверить комплектность.', creator_id: 1, creator_name: 'Руководитель', assignee_id: 2, assignee_name: 'Педагог', due_at: '2026-10-11T09:00:00Z', status: 'todo', object_type: null as string | null, object_id: null as number | null, object_name: null as string | null };
  let tasks = [{ ...base }, { ...base, id: 2, title: 'Подготовить встречу', assignee_id: 3, assignee_name: 'Куратор', status: 'done' }]; let nextID = 3; let failSave = false;
  const saved: Record<string, unknown>[] = []; const queries: URLSearchParams[] = []; const requests: string[] = [];
  await page.clock.install({ time: new Date('2026-10-12T09:00:00Z') });
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()); const path = url.pathname.replace('/api/v1', ''); const method = route.request().method(); requests.push(path);
    const json = (body: unknown, status = 200) => route.fulfill({ status, json: body });
    const overdue = (v: typeof base) => ['todo', 'in_progress'].includes(v.status) && v.due_at < '2026-10-12T09:00:00Z';
    if (path === '/auth/csrf') return json({ csrf_token: 'test' });
    if (path === '/auth/me') return json(user);
    if (path === '/tasks/assignees') return json({ items: people, total: people.length, page: 1, page_size: 100 });
    if (['/students', '/groups', '/lessons'].includes(path)) return json({ items: [{ id: 10, name: path === '/students' ? 'Иван Иванов' : 'Группа развития', topic: 'Аргументация' }], total: 1, page: 1, page_size: 20 });
    if (path === '/tasks' && method === 'GET') {
      queries.push(url.searchParams);
      const items = tasks.filter(v => role !== 'teacher' || v.assignee_id === user.id).filter(v => !url.searchParams.get('status') || v.status === url.searchParams.get('status')).filter(v => !url.searchParams.get('assignee_id') || v.assignee_id === Number(url.searchParams.get('assignee_id'))).filter(v => !url.searchParams.get('overdue') || overdue(v) === (url.searchParams.get('overdue') === 'true')).map(v => ({ ...v, overdue: overdue(v) }));
      return json({ items, total: items.length, page: 1, page_size: 20 });
    }
    if (path === '/tasks' && method === 'POST' || /^\/tasks\/\d+$/.test(path) && method === 'PATCH') {
      if (failSave) { failSave = false; return json({ code: 'internal_error', message: 'Повторите сохранение.' }, 500); }
      const body = route.request().postDataJSON(); saved.push(body);
      const v = { ...base, ...body, id: path === '/tasks' ? nextID++ : Number(path.split('/')[2]), assignee_name: people.find(p => p.id === body.assignee_id)?.name || '', object_name: body.object_id ? 'Иван Иванов' : null };
      if (method === 'POST') tasks.push(v); else tasks = tasks.map(old => old.id === v.id ? v : old);
      return json({ ...v, overdue: overdue(v) }, method === 'POST' ? 201 : 200);
    }
    if (/^\/tasks\/\d+\/status$/.test(path)) { const v = tasks.find(v => v.id === Number(path.split('/')[2]))!; v.status = route.request().postDataJSON().status; return json({ ...v, overdue: overdue(v) }); }
    if (/^\/tasks\/\d+$/.test(path) && method === 'GET') { const v = tasks.find(v => v.id === Number(path.split('/')[2]))!; return json({ ...v, overdue: overdue(v) }); }
    return json({ code: 'not_found', message: 'Не найдено.' }, 404);
  });
  return { saved, queries, requests, failNextSave: () => { failSave = true; } };
}
async function accessible(page: Page) { expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true); const axe = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze(); expect(axe.violations).toEqual([]); }

test('leader creates, links, reassigns and cancels tasks without user administration access', async ({ page }) => {
  const api = await fixture(page); await page.goto('/#/tasks'); await expect(page.getByRole('table', { name: 'Задачи', exact: true })).toBeVisible(); await accessible(page);
  await page.getByRole('button', { name: 'Добавить: задача' }).click(); const dialog = page.getByRole('dialog', { name: 'Новая задача' });
  await dialog.getByLabel('Название', { exact: true }).fill('Рабочие листы для группы'); await dialog.getByLabel('Ожидаемый результат').fill('Распечатать комплект материалов'); await dialog.getByLabel('Ответственный', { exact: true }).selectOption('2');
  await expect(dialog.getByRole('option', { name: 'Заблокированный · Заблокирован · №4' })).toBeDisabled();
  await dialog.getByLabel('Срок (МСК)').fill('2026-10-12T10:00'); await dialog.getByLabel('Связь с записью').selectOption('student');
  await page.clock.runFor(250); await dialog.getByLabel('Связанная запись', { exact: true }).selectOption('10'); await accessible(page);
  api.failNextSave(); await dialog.getByRole('button', { name: 'Сохранить задачу' }).click(); await expect(dialog.getByRole('alert')).toContainText('Повторите сохранение'); await expect(dialog.getByLabel('Название', { exact: true })).toHaveValue('Рабочие листы для группы');
  await dialog.getByRole('button', { name: 'Сохранить задачу' }).click(); await expect(dialog).not.toBeVisible();
  expect(api.saved[0]).toMatchObject({ assignee_id: 2, due_at: '2026-10-12T07:00:00.000Z', status: 'todo', object_type: 'student', object_id: 10 }); expect(api.saved[0]).not.toHaveProperty('creator_id');
  await page.getByRole('button', { name: 'Рабочие листы для группы', exact: true }).click(); const details = page.getByRole('dialog'); await expect(details).toContainText('Распечатать комплект материалов'); await expect(details).toContainText('Ученик: Иван Иванов'); await accessible(page);
  await details.getByRole('button', { name: 'Изменить', exact: true }).click(); const edit = page.getByRole('dialog', { name: 'Изменить задачу' });
  await expect(page.getByRole('dialog')).toHaveCount(1); await edit.getByLabel('Ответственный', { exact: true }).selectOption('3'); await edit.getByLabel('Статус задачи').selectOption('cancelled'); await edit.getByLabel('Связь с записью').selectOption(''); await edit.getByRole('button', { name: 'Сохранить задачу' }).click();
  expect(api.saved[1]).toMatchObject({ assignee_id: 3, status: 'cancelled', object_type: null, object_id: null });
  await expect(page.getByRole('row').filter({ hasText: 'Рабочие листы для группы' })).toContainText('Отменена');
  expect(api.requests).not.toContain('/users'); await accessible(page);
});

test('task assignee status and overdue filters combine and exclude finished tasks', async ({ page }) => {
  const api = await fixture(page); await page.goto('/#/tasks'); await expect(page.getByRole('table', { name: 'Задачи', exact: true })).toBeVisible();
  await page.getByLabel('Ответственный', { exact: true }).selectOption('2'); await page.getByLabel('Статус', { exact: true }).selectOption('todo'); await page.getByLabel('Просрочка').selectOption('true'); await page.getByRole('button', { name: 'Применить', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Подготовить материалы', exact: true })).toBeVisible(); await expect(page.getByRole('button', { name: 'Подготовить встречу', exact: true })).toHaveCount(0);
  await expect.poll(() => Object.fromEntries(api.queries.at(-1) ?? [])).toMatchObject({ assignee_id: '2', status: 'todo', overdue: 'true' });
  await expect(page.getByRole('button', { name: 'Завершить', exact: true })).toHaveCount(0);
  await page.getByRole('button', { name: 'В работу', exact: true }).click(); await expect(page.getByRole('heading', { name: 'Задачи не найдены' })).toBeVisible(); await accessible(page);
});

test('teacher sees own tasks, reads expected result, starts and completes without management controls', async ({ page }) => {
  const api = await fixture(page, 'teacher'); await page.goto('/#/tasks'); await expect(page.getByRole('button', { name: 'Подготовить материалы', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Добавить: задача' })).toHaveCount(0); await expect(page.getByRole('button', { name: 'Изменить', exact: true })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Подготовить встречу', exact: true })).toHaveCount(0); await expect(page.getByText('Просрочена', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Подготовить материалы', exact: true }).click(); await expect(page.getByRole('dialog')).toContainText('Проверить комплектность.'); await page.getByRole('dialog').getByRole('button', { name: 'В работу', exact: true }).click();
  await expect(page.getByRole('table').getByText('В работе', { exact: true })).toBeVisible(); await page.getByRole('button', { name: 'Завершить', exact: true }).click(); await expect(page.getByRole('table').getByText('Выполнена', { exact: true })).toBeVisible();
  await expect(page.getByText('Просрочена', { exact: true })).toHaveCount(0); await expect(page.getByRole('button', { name: 'В работу', exact: true })).toHaveCount(0); await expect(page.getByRole('button', { name: 'Завершить', exact: true })).toHaveCount(0);
  expect(api.requests).not.toContain('/tasks/assignees'); await accessible(page);
});
