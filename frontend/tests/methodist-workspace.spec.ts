import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test('methodist edits programs and modules and reads implementation reports', async ({ page }, info) => {
  const program = { id: 5, name: 'Развитие речи', status: 'active', description: 'Образовательная программа', modules: [{ id: 6, name: 'Диалог', position: 1, description: 'Практика общения' }] };
  const metric = { attendance: { present: 2, late: 0, absent: 0, excused: 0, unmarked: 0, total: 2, percent: 100 }, assignments: { total: 2, assigned: 0, submitted: 1, reviewed: 1, overdue: 0, percent: 50 } };
  const progress = { period: { from: '2026-10-06', to: '2026-11-04' }, previous_period: { from: '2026-09-06', to: '2026-10-05' }, current: metric, previous: metric, monthly: [] };
  const report = { id: 1, student_id: 10, student_name: 'Ученик AIQ', author_name: 'Автор AIQ', summary: 'Учебные результаты.', created_at: '2026-11-04T09:00:00Z', published_at: null, program_id: 5, program_name: 'Развитие речи' };
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url());
    const path = url.pathname.replace('/api/v1', '');
    const method = route.request().method();
    const json = (body: unknown) => route.fulfill({ json: body });
    if (path === '/auth/csrf') return json({ csrf_token: 'fixture' });
    if (path === '/features') return json({});
    if (path === '/auth/me') return json({ id: 7, name: 'Методист AIQ', email: 'methodist@example.test', roles: ['methodist'], must_change_password: false });
    if (path === '/methodist/workspace') return json({ counts: { students: 1, programs: 1, teacher_reports: 1, curator_reports: 1 }, recent_reports: [{ ...report, kind: 'teacher' }] });
    if (path === '/methodist/programs') return json({ items: url.searchParams.get('search') === 'Нет' ? [] : [{ id: 3, group_name: 'Группа речи', teacher_name: 'Педагог AIQ', program_id: 5, program_name: 'Развитие речи', module_count: 1, student_count: 1, completed_lessons: 2, lesson_count: 3 }], total: url.searchParams.get('search') === 'Нет' ? 0 : 1 });
    if (path === '/programs/5/modules' && method === 'POST') {
      const body = route.request().postDataJSON();
      expect(body).toEqual({ name: 'Новый модуль', description: 'Упражнения для диалога', position: 2 });
      program.modules.push({ id: 7, ...body }); return json(program.modules[1]);
    }
    if (path === '/programs') {
      if (method === 'POST') { Object.assign(program, route.request().postDataJSON()); return json(program); }
      return json({ items: [program], total: 1 });
    }
    if (path === '/programs/5') {
      if (method === 'PATCH') Object.assign(program, route.request().postDataJSON());
      return json(program);
    }
    if (path === '/methodist/students/10') return json({ student: { id: 10, name: 'Ученик AIQ', status: 'active' }, groups: [{ id: 3, name: 'Группа речи', program_id: 5, program_name: 'Развитие речи', teacher_name: 'Педагог AIQ' }], progress, assignments: [] });
    if (path === '/methodist/teacher-reports' || path === '/methodist/curator-reports') return json({ items: url.searchParams.get('publication') === 'published' ? [] : [{ ...report, ...(path.includes('curator') ? { goals: 'Освоить диалог', recommendations: 'Практика дома' } : {}) }], total: url.searchParams.get('publication') === 'published' ? 0 : 1 });
    return route.fulfill({ status: 404, json: { message: 'Не найдено' } });
  });
  await page.goto('/#/methodology');
  await expect(page.getByRole('heading', { name: 'Кабинет методиста', exact: true })).toBeVisible();
  if (info.project.name.includes('mobile')) await page.getByRole('button', { name: 'Меню', exact: true }).click();
  const nav = info.project.name.includes('mobile') ? page.getByRole('dialog').getByRole('navigation', { name: 'Основная навигация' }) : page.locator('.sidebar').getByRole('navigation', { name: 'Основная навигация' });
  await expect(nav.getByRole('link')).toHaveText(['Обзор', 'Программы', 'Реализация программ']);
  await nav.getByRole('link', { name: 'Реализация программ', exact: true }).click();
  await expect(page.getByRole('table', { name: 'Программы педагогов' })).toContainText('Педагог AIQ');
  await expect(page.getByRole('table', { name: 'Программы педагогов' })).toContainText('2 / 3');
  await page.getByRole('link', { name: 'Развитие речи', exact: true }).click();
  const mobile = info.project.name.includes('mobile');
  if (mobile) await page.getByRole('button', { name: 'Содержание программы', exact: true }).click();
  const outline = mobile ? page.getByRole('dialog') : page.locator('.program-outline');
  await outline.getByRole('button', { name: '1. Диалог', exact: true }).click();
  await expect(page.getByText('Практика общения')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Изменить модуль', exact: true })).toBeVisible();
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
  if (mobile) await page.getByRole('button', { name: 'Содержание программы', exact: true }).click();
  await outline.getByRole('button', { name: 'Добавить модуль', exact: true }).click();
  await page.getByRole('dialog').getByLabel('Название', { exact: true }).fill('Новый модуль');
  await page.getByRole('dialog').getByLabel('Описание', { exact: true }).fill('Упражнения для диалога');
  await page.getByRole('button', { name: 'Сохранить модуль', exact: true }).click();
  await expect(page.getByText('Модуль сохранён.', { exact: true })).toBeVisible();
  if (mobile) await page.getByRole('button', { name: 'Содержание программы', exact: true }).click();
  await outline.getByRole('button', { name: '2. Новый модуль', exact: true }).click();
  await expect(page.getByText('Упражнения для диалога', { exact: true })).toBeVisible();
  await page.goto('/#/programs');
  await page.getByRole('button', { name: 'Добавить программу', exact: true }).click();
  await page.getByRole('dialog').getByLabel('Название', { exact: true }).fill('Новая программа');
  await page.getByRole('button', { name: 'Сохранить программу', exact: true }).click();
  await expect(page.getByText('Программа сохранена.', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Уведомления', exact: true }).click();
  await expect(page.getByRole('dialog')).toContainText('Учебные результаты.');
  await page.getByRole('dialog').getByRole('link', { name: 'Открыть отчёты ученика' }).click();
  await expect(page.getByRole('heading', { name: 'Отчёты педагогов', exact: true })).toBeVisible();
  await page.goto('/#/methodist-students/10');
  await expect(page.getByLabel('Посещаемость за период')).toHaveText('100%');
  await expect(page.getByLabel('Принятые работы за период')).toHaveText('1 / 2');
  await expect(page.getByText('Освоить диалог')).toBeVisible();
  for (const kind of ['teacher', 'curator']) {
    await page.goto(`/#/methodist-${kind}-reports`);
    await expect(page.getByText('Учебные результаты.', { exact: true })).toBeVisible();
    await page.getByLabel('Ученик или автор отчёта').fill('Автор');
    await page.getByLabel('Публикация', { exact: true }).selectOption('published');
    const response = page.waitForRequest(req => req.url().includes(`/${kind}-reports?`) && req.url().includes('publication=published') && new URL(req.url()).searchParams.get('search') === 'Автор');
    await page.getByRole('button', { name: 'Применить', exact: true }).click();
    await response;
    await expect(page.getByText('Отчётов за выбранный период нет.')).toBeVisible();
  }
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
  await page.goto('/#/students');
  await expect(page.getByRole('heading', { name: 'Раздел недоступен' })).toBeVisible();
});
