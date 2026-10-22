import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

async function fixture(page: Page, role = 'admin') {
  const user = { id: 1, name: 'Администратор', email: 'admin@example.test', roles: [role], blocked: false, must_change_password: false };
  const students = [{ id: 10, name: 'Первый ученик' }, { id: 11, name: 'Второй ученик' }];
  const groups = [
    { id: 4, name: 'Группа развития', program_id: 6, program_name: 'Программа AIQ', teacher_id: 3, teacher_name: 'Педагог AIQ', capacity: 2, status: 'active' },
    { id: 8, name: 'Вторая группа', program_id: 6, program_name: 'Программа AIQ', teacher_id: null, teacher_name: null, capacity: 1, status: 'active' },
  ];
  const enrollments: { id: number; group_id: number; student_id: number; student_name: string; starts_on: string; ends_on: string | null }[] = [
    { id: 5, group_id: 4, student_id: 10, student_name: students[0].name, starts_on: '2026-09-01', ends_on: null },
    { id: 6, group_id: 8, student_id: 11, student_name: students[1].name, starts_on: '2026-09-01', ends_on: null },
  ];
  let nextID = 20;
  const activeCount = (id: number) => enrollments.filter(e => e.group_id === id && !e.ends_on).length;
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()); const path = url.pathname.replace('/api/v1', ''); const method = route.request().method();
    const json = (body: unknown, status = 200) => route.fulfill({ status, json: body });
    const failure = (message: string, status = 409) => json({ code: 'group_conflict', message }, status);
    const pageData = (items: unknown[], total = items.length) => ({ items, total, page: Number(url.searchParams.get('page') ?? 1), page_size: 100 });
    if (path === '/auth/csrf') return json({ csrf_token: 'test' });
    if (path === '/auth/me') return json(user);
    if (path === '/programs') return json(pageData([{ id: 6, name: 'Программа AIQ', status: 'active' }]));
    if (path === '/programs/6/versions') return json({ items: [{ id: 60, number: 1, status: 'published', document: { name: 'Программа AIQ' } }], total: 1 });
    if (path === '/teachers') return json(pageData([{ id: 3, name: 'Педагог AIQ', status: 'active' }]));
    // Prove that student selection includes later API pages.
    if (path === '/students') return json(pageData(url.searchParams.get('page') === '2' ? [students[1]] : [students[0]], 2));
    const match = path.match(/^\/groups(?:\/(\d+))?(?:\/enrollments(?:\/(\d+))?(\/transfer)?)?$/);
    if (!match) return failure('Маршрут не найден.', 404);
    const id = Number(match[1]); const enrollmentID = Number(match[2]);
    if (!id) {
      if (method === 'POST') { const value = { id: nextID++, ...route.request().postDataJSON(), program_name: 'Программа AIQ', teacher_name: 'Педагог AIQ' }; groups.push(value); return json(value, 201); }
      const items = groups.filter(g => g.name.toLowerCase().includes((url.searchParams.get('search') ?? '').toLowerCase()) && (!url.searchParams.get('status') || g.status === url.searchParams.get('status'))).map(g => ({ ...g, active_count: activeCount(g.id) }));
      return json(pageData(items));
    }
    const group = groups.find(g => g.id === id); if (!group) return failure('Группа или зачисление не найдены.', 404);
    if (path.includes('/enrollments')) {
      const body = route.request().postDataJSON();
      if (!enrollmentID) {
        if (activeCount(id) >= group.capacity) return failure('Вместимость группы превышена.');
        const student = students.find(s => s.id === body.student_id)!;
        const value = { id: nextID++, group_id: id, student_id: student.id, student_name: student.name, starts_on: body.starts_on, ends_on: null };
        enrollments.push(value); return json(value, 201);
      }
      const enrollment = enrollments.find(e => e.id === enrollmentID && e.group_id === id)!;
      if (!match[3]) { enrollment.ends_on = body.ends_on; return json(enrollment); }
      const target = groups.find(g => g.id === body.target_group_id)!;
      if (activeCount(target.id) >= target.capacity) return failure('Вместимость группы превышена.');
      enrollment.ends_on = body.transfers_on;
      const value = { ...enrollment, id: nextID++, group_id: target.id, starts_on: body.transfers_on, ends_on: null };
      enrollments.push(value); return json({ previous: enrollment, enrollment: value });
    }
    if (method === 'PATCH') { const body = route.request().postDataJSON(); if (body.capacity && body.capacity < activeCount(id)) return failure('Вместимость группы превышена.'); Object.assign(group, body); }
    return json({ ...group, active_count: activeCount(id), enrollments: enrollments.filter(e => e.group_id === id) });
  });
}
async function accessible(page: Page) {
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
}

test('groups create, edit, close, reenroll, transfer and preserve history', async ({ page }, info) => {
  await fixture(page); await page.goto('/#/groups');
  await page.getByRole('button', { name: 'Добавить: группа' }).click(); let dialog = page.getByRole('dialog');
  await dialog.getByLabel('Название', { exact: true }).fill('Новая группа');
  await expect(dialog.getByLabel('Статус', { exact: true })).toHaveValue('draft');
  await dialog.getByLabel('Программа', { exact: true }).selectOption('6'); await dialog.getByLabel('Основной педагог').selectOption('3');
  await dialog.getByLabel('Опубликованная версия', { exact: true }).selectOption('60');
  await dialog.getByLabel('Вместимость').fill('3'); await dialog.getByLabel('Статус', { exact: true }).selectOption('active'); await accessible(page);
  await dialog.getByRole('button', { name: 'Сохранить', exact: true }).click();
  await expect(page.getByRole('link', { name: 'Новая группа', exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'Группа развития', exact: true }).click();
  await expect(page.getByRole('table', { name: 'История зачислений' })).toContainText('Первый ученик');
  await page.getByRole('button', { name: 'Изменить группу', exact: true }).click(); dialog = page.getByRole('dialog');
  await expect(dialog.getByLabel('Программа', { exact: true })).toHaveValue('6'); await expect(dialog.getByLabel('Основной педагог')).toHaveValue('3');
  await dialog.getByLabel('Вместимость').fill('3'); await dialog.getByRole('button', { name: 'Сохранить', exact: true }).click();
  await page.getByRole('button', { name: 'Зачислить ученика', exact: true }).click(); dialog = page.getByRole('dialog');
  await dialog.getByLabel('Ученик', { exact: true }).selectOption('11'); await dialog.getByLabel('Дата начала').fill('2026-09-02'); await accessible(page);
  await dialog.getByRole('button', { name: 'Зачислить', exact: true }).click();
  await page.getByRole('button', { name: 'Вывести Второй ученик', exact: true }).click(); dialog = page.getByRole('dialog');
  await dialog.getByLabel('Дата окончания').fill('2026-10-01'); await dialog.getByRole('button', { name: 'Завершить зачисление' }).click();
  await expect(page.getByRole('table', { name: 'История зачислений' })).toContainText('1 окт. 2026');
  await page.getByRole('button', { name: 'Зачислить ученика', exact: true }).click(); dialog = page.getByRole('dialog');
  await dialog.getByLabel('Ученик', { exact: true }).selectOption('11'); await dialog.getByLabel('Дата начала').fill('2026-10-02'); await dialog.getByRole('button', { name: 'Зачислить', exact: true }).click();
  await expect(page.getByRole('table', { name: 'История зачислений' }).locator('tbody tr')).toHaveCount(3);
  await page.getByRole('button', { name: 'Перевести Первый ученик', exact: true }).click(); dialog = page.getByRole('dialog');
  await dialog.getByLabel('Новая группа').selectOption('8'); await dialog.getByLabel('Дата перевода').fill('2026-10-03'); await dialog.getByRole('button', { name: 'Перевести', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('Вместимость группы превышена'); await accessible(page);
  await dialog.getByLabel('Новая группа').selectOption({ label: 'Новая группа · 0/3' }); await dialog.getByRole('button', { name: 'Перевести', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Ученик переведён' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Перевести Первый ученик', exact: true })).not.toBeVisible();
  await expect(page.getByRole('table', { name: 'История зачислений' }).locator('tbody tr')).toHaveCount(3); await accessible(page);
  await page.screenshot({ path: `../docs/frontend/screenshots/group-card-${info.project.name}.png`, fullPage: true });
  await page.goto('/#/groups'); const row = page.getByRole('row').filter({ has: page.getByRole('link', { name: 'Группа развития', exact: true }) });
  await row.getByRole('button', { name: 'В архив', exact: true }).click(); dialog = page.getByRole('dialog'); await dialog.getByRole('button', { name: 'Перенести в архив', exact: true }).click();
  await page.getByRole('link', { name: 'Группа развития', exact: true }).click(); await expect(page.getByRole('button', { name: 'Зачислить ученика' })).toBeDisabled(); await expect(page.getByRole('table', { name: 'История зачислений' }).locator('tbody tr')).toHaveCount(3);
});

test('groups readers have no management actions and see ID errors', async ({ page }) => {
  for (const role of ['leader', 'teacher', 'curator']) {
    await page.unrouteAll(); await fixture(page, role); await page.goto('/#/groups'); await page.reload();
    await expect(page.getByRole('link', { name: 'Группа развития', exact: true })).toBeVisible(); await expect(page.getByRole('button', { name: 'Добавить: группа' })).not.toBeVisible();
    await page.getByRole('link', { name: 'Группа развития', exact: true }).click(); await expect(page.getByRole('table', { name: 'История зачислений' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Изменить группу' })).not.toBeVisible(); await expect(page.getByRole('button', { name: 'Зачислить ученика' })).not.toBeVisible(); await expect(page.getByRole('button', { name: 'Вывести Первый ученик' })).not.toBeVisible(); await accessible(page);
  }
  await page.goto('/#/groups/99999'); await expect(page.getByRole('alert')).toContainText('Группа или зачисление не найдены'); await accessible(page);
});
