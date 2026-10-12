import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

async function fixture(page: Page, role = 'admin') {
  const user = { id: role === 'teacher' ? 2 : 1, name: 'Сотрудник AIQ', email: 'staff@example.test', roles: [role], blocked: false, must_change_password: false };
  const groups = [{ id: 4, name: 'Группа AIQ', program_id: 6, teacher_id: 3, status: 'active' }, { id: 9, name: 'Другая группа', program_id: 13, teacher_id: 12, status: 'active' }];
  const teachers = [{ id: 3, name: 'Педагог AIQ', user_id: 2 }, { id: 12, name: 'Второй педагог', user_id: null }];
  const modules = [{ id: 7, program_id: 6, name: 'Модуль AIQ' }, { id: 14, program_id: 13, name: 'Другой модуль' }];
  const enrollments = [{ id: 5, group_id: 4, student_id: 10, student_name: 'Первый ученик', starts_on: '2026-09-01', ends_on: null }, { id: 6, group_id: 4, student_id: 11, student_name: 'Второй ученик', starts_on: '2026-10-03', ends_on: null }];
  type Lesson = { id: number; group_id: number; module_id: number | null; teacher_id: number; topic: string; starts_at: string; ends_at: string; location: string; status: string };
  const lessons: Lesson[] = [{ id: 8, group_id: 4, module_id: 7, teacher_id: 3, topic: 'Ночное занятие', starts_at: '2026-10-01T21:00:00Z', ends_at: '2026-10-01T22:00:00Z', location: 'Класс AIQ', status: 'scheduled' }];
  type Entry = { id: number; enrollment_id: number; student_id: number; name: string; status: string; comment: string; included: boolean };
  const journals = new Map<number, Entry[]>(); let nextID = 20;
  const getJournal = (lesson: Lesson) => {
    if (!journals.has(lesson.id)) {
      const day = new Intl.DateTimeFormat('en-CA', { timeZone: 'Europe/Moscow' }).format(new Date(lesson.starts_at));
      journals.set(lesson.id, enrollments.filter(e => e.group_id === lesson.group_id && e.starts_on <= day && (!e.ends_on || e.ends_on >= day)).map(e => ({ id: nextID++, enrollment_id: e.id, student_id: e.student_id, name: e.student_name, status: 'unmarked', comment: '', included: true })));
    }
    return journals.get(lesson.id)!;
  };
  const card = (l: Lesson) => { const group = groups.find(g => g.id === l.group_id)!; const teacher = teachers.find(t => t.id === l.teacher_id)!; return { ...l, group_name: group.name, program_id: group.program_id, module_name: modules.find(m => m.id === l.module_id)?.name ?? null, teacher_name: teacher.name, teacher_user_id: teacher.user_id, roster_initialized_at: journals.has(l.id) ? '2026-10-12T12:00:00Z' : null }; };
  const conflict = (l: Lesson) => l.status !== 'cancelled' && lessons.some(other => other.id !== l.id && other.status !== 'cancelled' && (other.group_id === l.group_id || other.teacher_id === l.teacher_id) && new Date(l.starts_at) < new Date(other.ends_at) && new Date(l.ends_at) > new Date(other.starts_at));
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()); const path = url.pathname.replace('/api/v1', ''); const method = route.request().method();
    const json = (body: unknown, status = 200) => route.fulfill({ json: body, status }); const failure = (message: string, status = 409) => json({ code: 'lesson_conflict', message }, status);
    const list = (items: unknown[]) => ({ items, total: items.length, page: Number(url.searchParams.get('page') ?? 1), page_size: 100 });
    if (path === '/auth/csrf') return json({ csrf_token: 'test' }); if (path === '/auth/me') return json(user);
    if (path === '/groups') return json(list(groups)); if (path === '/teachers') return json(list(teachers));
    if (path.match(/^\/groups\/\d+$/)) { const group = groups.find(g => g.id === Number(path.split('/')[2]))!; return json({ ...group, enrollments: enrollments.filter(e => e.group_id === group.id) }); }
    if (path.match(/^\/programs\/\d+$/)) return json({ modules: modules.filter(m => m.program_id === Number(path.split('/')[2])) });
    const match = path.match(/^\/lessons(?:\/(\d+))?(?:\/(attendance|roster|cancel|complete))?$/); if (!match) return failure('Маршрут не найден.', 404);
    const id = Number(match[1]); const operation = match[2];
    if (!id) {
      if (method === 'POST') { const lesson = { ...route.request().postDataJSON(), id: nextID++ } as Lesson; if (conflict(lesson)) return failure('У педагога уже есть занятие в это время.'); lessons.push(lesson); return json(card(lesson), 201); }
      const values = lessons.filter(l => l.topic.toLowerCase().includes((url.searchParams.get('search') ?? '').toLowerCase()) && (!url.searchParams.get('status') || l.status === url.searchParams.get('status')) && (!url.searchParams.get('group_id') || l.group_id === Number(url.searchParams.get('group_id'))) && (!url.searchParams.get('teacher_id') || l.teacher_id === Number(url.searchParams.get('teacher_id')))); return json(list(values.map(card)));
    }
    const lesson = lessons.find(l => l.id === id); if (!lesson) return failure('Занятие не найдено.', 404);
    if (operation === 'attendance' || operation === 'roster') {
      const entries = getJournal(lesson);
      if (method === 'PUT' && operation === 'attendance') { for (const mark of route.request().postDataJSON().items) Object.assign(entries.find(e => e.student_id === mark.student_id)!, mark); return route.fulfill({ status: 204 }); }
      if (method === 'PUT' && operation === 'roster') {
        const body = route.request().postDataJSON(); if (!body.reason.trim()) return failure('Укажите причину корректировки.', 400);
        entries.forEach(e => e.included = false);
        for (const enrollmentID of body.enrollment_ids) { const e = enrollments.find(e => e.id === enrollmentID)!; const old = entries.find(item => item.student_id === e.student_id); if (old) old.included = true; else entries.push({ id: nextID++, enrollment_id: e.id, student_id: e.student_id, name: e.student_name, status: 'unmarked', comment: '', included: true }); }
      }
      return json({ lesson_id: id, lesson_date: '2026-10-02', initialized_at: '2026-10-12T12:00:00Z', items: entries.filter(e => e.included) });
    }
    if (method === 'PATCH' || operation === 'cancel' || operation === 'complete') {
      const body = operation ? { status: operation === 'cancel' ? 'cancelled' : 'completed' } : route.request().postDataJSON(); const updated = { ...lesson, ...body };
      if (conflict(updated)) return failure('У педагога уже есть занятие в это время.'); Object.assign(lesson, updated);
    }
    return json(card(lesson));
  });
  return { changeGroup: () => { enrollments[0].student_name = 'Новое имя'; enrollments.push({ id: 15, group_id: 4, student_id: 16, student_name: 'Добавлен позже', starts_on: '2026-09-01', ends_on: null }); } };
}
async function accessible(page: Page) {
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
}

test('schedule creates lessons with modules, Moscow time, conflicts and adjacent intervals', async ({ page }, info) => {
  await fixture(page); await page.goto('/#/lessons'); await expect(page.getByRole('table', { name: 'Расписание', exact: true })).toContainText('2 окт. 2026');
  await page.getByRole('button', { name: 'Добавить занятие', exact: true }).click(); let dialog = page.getByRole('dialog');
  await dialog.getByLabel('Группа', { exact: true }).selectOption('9'); await expect(dialog.getByLabel('Педагог', { exact: true })).toHaveValue('12'); await dialog.getByLabel('Модуль', { exact: true }).selectOption('14');
  await dialog.getByLabel('Группа', { exact: true }).selectOption('4'); await expect(dialog.getByLabel('Модуль', { exact: true })).toHaveValue(''); await dialog.getByLabel('Модуль', { exact: true }).selectOption('7');
  await dialog.getByLabel('Тема', { exact: true }).fill('Смежное занятие'); await dialog.getByLabel('Начало (МСК)').fill('2026-10-02T00:30'); await dialog.getByLabel('Окончание (МСК)').fill('2026-10-02T01:30');
  await dialog.getByRole('button', { name: 'Сохранить занятие' }).click(); await expect(dialog.getByRole('alert')).toContainText('У педагога уже есть занятие');
  await dialog.getByLabel('Начало (МСК)').fill('2026-10-02T01:00'); await dialog.getByLabel('Окончание (МСК)').fill('2026-10-02T02:00'); await dialog.getByLabel('Место проведения').fill('Зал AIQ'); await accessible(page);
  await page.screenshot({ path: `../docs/frontend/screenshots/lesson-editor-${info.project.name}.png`, fullPage: true });
  await dialog.getByRole('button', { name: 'Сохранить занятие' }).click(); await page.getByRole('link', { name: 'Смежное занятие', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Смежное занятие', exact: true })).toBeVisible(); await expect(page.getByText('Зал AIQ', { exact: true })).toBeVisible(); await expect(page.getByText('Модуль AIQ', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Изменить занятие', exact: true }).click(); dialog = page.getByRole('dialog'); await expect(dialog.getByLabel('Начало (МСК)')).toHaveValue('2026-10-02T01:00'); await expect(dialog.getByLabel('Группа', { exact: true })).toBeDisabled(); await dialog.getByLabel('Модуль', { exact: true }).selectOption(''); await dialog.getByRole('button', { name: 'Сохранить занятие' }).click(); await expect(page.getByText('Без модуля', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Отменить занятие', exact: true }).click(); await page.getByRole('dialog').getByRole('button', { name: 'Подтвердить' }).click(); await expect(page.locator('.badge').filter({ hasText: /^Отменено$/ })).toBeVisible();
  await page.goto('/#/lessons'); await page.getByLabel('Статус', { exact: true }).selectOption('cancelled'); await page.getByRole('button', { name: 'Применить', exact: true }).click(); await expect(page.getByRole('link', { name: 'Смежное занятие', exact: true })).toBeVisible(); await expect(page.getByRole('link', { name: 'Ночное занятие', exact: true })).not.toBeVisible(); await page.getByRole('button', { name: 'По дням', exact: true }).click(); await accessible(page);
});

test('journal snapshot survives group changes and audited roster correction preserves marks', async ({ page }, info) => {
  const state = await fixture(page); await page.goto('/#/lessons/8');
  await expect(page.getByText('Первый ученик', { exact: true })).toBeVisible(); await expect(page.getByText('Второй ученик', { exact: true })).not.toBeVisible();
  await page.getByLabel('Посещаемость: Первый ученик').selectOption('present'); await page.getByLabel('Комментарий: Первый ученик').fill('Заметка'); await page.getByRole('button', { name: 'Сохранить посещаемость', exact: true }).click(); await expect(page.getByRole('status').filter({ hasText: 'Посещаемость сохранена' })).toBeVisible();
  state.changeGroup(); await page.reload(); await expect(page.getByText('Первый ученик', { exact: true })).toBeVisible(); await expect(page.getByText('Добавлен позже', { exact: true })).not.toBeVisible(); await expect(page.getByLabel('Комментарий: Первый ученик')).toHaveValue('Заметка');
  await page.getByRole('button', { name: 'Корректировать состав', exact: true }).click(); let dialog = page.getByRole('dialog'); await dialog.getByLabel('Второй ученик', { exact: true }).check(); await dialog.getByLabel('Причина корректировки').fill(' '); await dialog.getByRole('button', { name: 'Сохранить состав' }).click(); await expect(dialog.getByRole('alert')).toContainText('Укажите причину');
  await dialog.getByLabel('Причина корректировки').fill('Уточнили состав'); await accessible(page); await page.screenshot({ path: `../docs/frontend/screenshots/lesson-roster-${info.project.name}.png`, fullPage: true }); await dialog.getByRole('button', { name: 'Сохранить состав' }).click(); await expect(dialog).not.toBeVisible();
  await expect(page.getByLabel('Посещаемость: Второй ученик')).toBeVisible(); await page.getByRole('button', { name: 'Корректировать состав', exact: true }).click(); dialog = page.getByRole('dialog'); await dialog.getByLabel('Первый ученик', { exact: true }).uncheck(); await dialog.getByLabel('Причина корректировки').fill('Временно исключён'); await dialog.getByRole('button', { name: 'Сохранить состав' }).click(); await expect(dialog).not.toBeVisible(); await expect(page.getByLabel('Посещаемость: Первый ученик')).not.toBeVisible();
  await page.getByRole('button', { name: 'Корректировать состав', exact: true }).click(); dialog = page.getByRole('dialog'); await dialog.getByLabel('Новое имя', { exact: true }).check(); await dialog.getByLabel('Причина корректировки').fill('Включён повторно'); await dialog.getByRole('button', { name: 'Сохранить состав' }).click(); await expect(dialog).not.toBeVisible();
  await expect(page.getByLabel('Посещаемость: Первый ученик')).toHaveValue('present'); await expect(page.getByLabel('Комментарий: Первый ученик')).toHaveValue('Заметка'); await accessible(page); await page.screenshot({ path: `../docs/frontend/screenshots/lesson-card-${info.project.name}.png`, fullPage: true });
});

test('lesson readers cannot edit schedule or journal composition', async ({ page }) => {
  for (const role of ['teacher', 'leader', 'curator']) {
    await page.unrouteAll(); await fixture(page, role); await page.goto('/#/lessons'); await page.reload(); await expect(page.getByRole('link', { name: 'Ночное занятие', exact: true })).toBeVisible(); await expect(page.getByRole('button', { name: 'Добавить занятие' })).not.toBeVisible();
    await page.getByRole('link', { name: 'Ночное занятие', exact: true }).click(); await expect(page.getByText('Первый ученик', { exact: true })).toBeVisible(); await expect(page.getByRole('button', { name: 'Изменить занятие' })).not.toBeVisible(); await expect(page.getByRole('button', { name: 'Корректировать состав' })).not.toBeVisible();
    if (role === 'teacher') await expect(page.getByRole('button', { name: 'Сохранить посещаемость' })).toBeEnabled(); else await expect(page.getByRole('button', { name: 'Сохранить посещаемость' })).toBeDisabled(); await accessible(page);
  }
  await page.goto('/#/lessons/99999'); await expect(page.getByRole('alert')).toContainText('Занятие не найдено'); await accessible(page);
});
