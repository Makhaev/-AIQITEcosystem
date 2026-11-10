import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { mkdir } from 'node:fs/promises';

const longTitle = 'Аргументация и критическое мышление: как подготовить убедительное выступление и ответить на сложные вопросы';
const lesson = { id: 8, topic: longTitle, group_id: 4, group_name: 'Мышление · группа 12', starts_at: '2026-11-06T09:00:00Z', ends_at: '2026-11-06T10:00:00Z', location: '', program_name: 'Коммуникация', module_name: null, teacher_name: 'Мария Смирнова' };
async function fixture(page: Page, role: string, empty = false) {
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    const json = (value: unknown) => route.fulfill({ json: value });
    if (path === '/features') return json({ student360: true });
    if (path === '/auth/csrf') return json({ csrf_token: 'fixture' });
    if (path === '/auth/me') return json({ id: 2, name: 'Мария Смирнова', email: 'test@example.test', roles: [role], blocked: false, must_change_password: false });
    if (path === '/teacher/workspace') return json({ profile: { id: 3, name: 'Мария Смирнова', specialization: 'Коммуникация', status: 'active' }, counts: { groups: 2, students: 12, lessons_today: 1, pending_reviews: 2, draft_reports: 1 }, upcoming_lessons: empty ? [] : [lesson] });
    if (path === '/learning/students') return json({ items: [{ id: 10, name: 'Иван Иванов' }] });
    if (path === '/learning/lessons' || path === '/curator-reports') return json({ items: [], total: 0 });
    if (path === '/reports') return json({ items: role === 'parent' ? [{ id: 1, lesson_id: 8, student_id: 10, student_name: 'Иван Иванов', lesson_topic: 'Результат занятия', lesson_starts_at: lesson.starts_at, summary: 'Ученик научился подкреплять аргументы примерами.', published_at: '2026-11-06T12:00:00Z' }] : [] });
    if (path === '/learning/progress') return json({ items: [{ student_id: 10, student_name: 'Иван Иванов', total: 4, reviewed: 1, submitted: 1, assigned: 2 }] });
    if (path === '/teacher/students') return json({ items: [{ id: 10, name: 'Иван Иванов', status: 'active', groups: [{ id:4, name:lesson.group_name }] }], total: 1 });
    if (path === '/programs') return json({ items: ['active','draft','archived'].map((status, i) => ({ id: i+6, name: i === 0 ? longTitle : i === 1 ? 'Программа без модулей' : 'Архивная программа', description: i ? '' : 'Практика аргументации и общения.', module_count: i ? 0 : 3, status })), total: 3 });
    if (path === '/assignments') return json({ items: empty ? [] : ['assigned', 'assigned', 'submitted', 'reviewed'].map((status, i) => ({ id: i + 1, lesson_id: 8, student_id: 10, student_name: 'Иван Иванов', lesson_topic: 'Аргументация', title: i === 0 ? longTitle : ['','Подготовить примеры','Записать выступление','Составить план'][i], instructions: 'Подготовьте три аргумента в защиту выбранной позиции. Подкрепите каждый примером.', due_at: i === 0 ? '2026-11-08T15:00:00Z' : null, status, answer: i ? 'Мой ответ' : '', feedback: i === 1 ? 'Добавь пример ко второму аргументу.' : i === 3 ? 'Хорошая работа!' : '' })) });
    if (path === '/students/10/360') return json({ id: 10, name: 'Иван Иванов', status: 'active', birth_date: null, curators: empty ? [] : [{ id: 2, name: 'Елена Петрова' }], full_access: role !== 'teacher', ...(role !== 'teacher' ? { parents: empty ? [] : [{ id: 11, name: 'Анна Иванова', phone: '+79001112233', email: 'parent@example.test', status: 'active', is_primary: true, relationship: 'Мать' }], history: [] } : {}), enrollments: empty ? [] : [{ id: 3, group_id: 4, group_name: lesson.group_name, group_status: 'active', program_id: 6, program_name: 'Коммуникация', teacher_id: 5, teacher_name: lesson.teacher_name, starts_on: '2026-09-01', ends_on: null }], upcoming_lessons: empty ? [] : [lesson], attendance: { from: '2026-10-07', to: '2026-11-05', percent: empty ? null : 75, present: empty ? 0 : 3, late: 0, absent: empty ? 0 : 1, excused: 0, unmarked: 0, items: [] } });
    return route.fulfill({ status: 404, json: { message: 'Нет данных.' } });
  });
}
async function capture(page: Page, name: string, width: number) {
  await page.evaluate(() => document.fonts.ready);
  const phase = process.env.CARDS_BEFORE ? 'before' : 'after';
  await mkdir('../docs/frontend/cards', { recursive: true });
  await page.screenshot({ path: `../docs/frontend/cards/${name}-${width}-${phase}.png`, fullPage: true });
}
async function accessible(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
}
test('lesson card: time, long title and journal', async ({ page }, info) => {
  await fixture(page, 'teacher'); await page.goto('/#/teaching');
  await expect(page.getByRole('heading', { name: longTitle })).toBeVisible();
  await capture(page, 'teacher', info.project.use.viewport!.width);
  if (process.env.CARDS_BEFORE) return;
  await expect(page.getByRole('link', { name: 'Открыть журнал', exact: true })).toHaveAttribute('href', '#/lessons/8');
  await expect(page.locator('.entity-card--highlight')).toContainText('06.11.2026, 12:00');
  await accessible(page);
});
test('assignment cards: deadline, revision, pending and result', async ({ page }, info) => {
  await fixture(page, 'student'); await page.goto('/#/assignments');
  await expect(page.getByRole('heading', { name: longTitle })).toBeVisible();
  await capture(page, 'assignment', info.project.use.viewport!.width);
  if (process.env.CARDS_BEFORE) return;
  await expect(page.getByLabel(`Ваш ответ: ${longTitle}`)).toBeHidden();
  await expect(page.getByRole('button', { name: 'Выполнить', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Доработать', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Посмотреть результат', exact: true })).toBeVisible();
  await expect(page.getByText('Срок не задан', { exact: true })).toHaveCount(3);
  await page.getByRole('button', { name: 'Доработать', exact: true }).click();
  await expect(page.getByLabel('Ваш ответ: Подготовить примеры')).toHaveValue('Мой ответ');
  await accessible(page);
});
test('Student 360: summary, tabs and private contact', async ({ page }, info) => {
  await fixture(page, 'curator'); await page.goto('/#/students/10');
  await expect(page.getByRole('heading', { name: 'Иван Иванов', exact: true })).toBeVisible();
  await capture(page, 'student360', info.project.use.viewport!.width);
  if (process.env.CARDS_BEFORE) return;
  const summary = page.getByRole('region', { name: 'Кратко об ученике' });
  await expect(summary).toContainText('Елена Петрова'); await expect(summary).toContainText('+79001112233');
  await expect(summary).toContainText('07.10.2026'); await expect(summary).toContainText('75%');
  await expect(page.getByRole('heading', { name: 'История критических изменений' })).toHaveCount(0);
  await page.getByRole('tab', { name: 'История', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'История критических изменений' })).toBeVisible();
  await page.getByRole('tab', { name: 'Файлы', exact: true }).click();
  await expect(page.getByText('Загрузка файлов пока недоступна.')).toBeVisible(); await accessible(page);
});
test('Student 360: missing data and restricted role', async ({ page }) => {
  test.skip(!!process.env.CARDS_BEFORE);
  await fixture(page, 'teacher', true); await page.goto('/#/students/10');
  const summary = page.getByRole('region', { name: 'Кратко об ученике' });
  await expect(summary).toContainText('Нет данных'); await expect(summary).toContainText('Не запланировано');
  await expect(summary).not.toContainText('Контакт родителя');
  await expect(page.getByRole('tab', { name: 'История', exact: true })).toHaveCount(0);
  await accessible(page);
});

test('program cards show real module counts, text status and one primary action', async ({ page }) => {
  test.skip(!!process.env.CARDS_BEFORE);
  await fixture(page, 'leader'); await page.goto('/#/programs');
  const card = page.getByRole('article', { name: longTitle, exact: true });
  await expect(card).toContainText('3'); await expect(card).toContainText('Активна');
  await expect(card.getByRole('link', {name:'Открыть программу',exact:true})).toHaveAttribute('href','#/programs/6');
  await expect(page.getByRole('article', {name:'Программа без модулей',exact:true})).toContainText('0');
  await expect(page.getByRole('article', {name:'Архивная программа',exact:true})).toContainText('В архиве');
  await accessible(page);
});
test('parent report card shows lesson period, publication and opens the full result', async ({ page }) => {
  test.skip(!!process.env.CARDS_BEFORE);
  await fixture(page, 'parent'); await page.goto('/#/portal');
  const card = page.getByRole('article', {name:'Результат занятия',exact:true});
  await expect(card).toContainText('06.11.2026'); await expect(card).toContainText('15:00');
  await card.getByRole('button', {name:'Читать отчёт',exact:true}).click();
  await expect(card.locator('[id^="report-body-"]')).toContainText('подкреплять аргументы');
  await accessible(page);
});
test('compact pupil card uses scoped upcoming lesson and attendance period', async ({ page }) => {
  test.skip(!!process.env.CARDS_BEFORE);
  await fixture(page, 'teacher'); await page.goto('/#/my-students');
  const card = page.getByRole('article', {name:'Иван Иванов',exact:true});
  await expect(card).toContainText('75%'); await expect(card).toContainText('07.10.2026');
  await expect(card).toContainText('12:00');
  await expect(card.getByRole('link', {name:'Открыть карточку',exact:true})).toHaveAttribute('href','#/students/10');
  await accessible(page);
});
