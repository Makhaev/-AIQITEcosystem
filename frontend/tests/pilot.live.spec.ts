import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

const base = process.env.PILOT_BASE_URL || 'http://127.0.0.1:5175';
const password = 'PilotOnly-Browser-2026';
async function login(page: Page, email: string, secret: string) {
  await page.goto(`${base}/#/login`);
  await page.getByLabel('Email', { exact: true }).fill(email);
  await page.getByLabel('Пароль', { exact: true }).fill(secret);
  await page.getByRole('button', { name: 'Войти', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Выйти', exact: true })).toBeVisible();
}
async function api(page: Page, path: string, method = 'GET', data?: unknown) {
  const csrf = await (await page.request.get(`${base}/api/v1/auth/csrf`)).json();
  const response = await page.request.fetch(`${base}/api/v1${path}`, { method, data, headers: { Origin: base, 'X-CSRF-Token': csrf.csrf_token } });
  expect(response.ok(), `${method} ${path}: ${response.status()}`).toBe(true);
  return response.status() === 204 ? undefined : response.json();
}
async function accessible(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
}
test('real MVP: invitations, lesson, submission, review, publication and recovery', async ({ page, browser }, info) => {
  test.skip(!process.env.PILOT_ADMIN_EMAIL || !process.env.PILOT_ADMIN_PASSWORD, 'Requires an isolated acceptance database and administrator credentials');
  const errors: string[] = []; page.on('pageerror', error => errors.push(error.message));
  await login(page, process.env.PILOT_ADMIN_EMAIL!, process.env.PILOT_ADMIN_PASSWORD!);
  const tag = `${Date.now()}-${info.project.name}`;
  const teacher = await api(page, '/teachers', 'POST', { name: `Педагог ${tag}` });
  const child = await api(page, '/students', 'POST', { name: `Ученик ${tag}` });
  const parent = await api(page, '/parents', 'POST', { name: `Родитель ${tag}`, email: `parent-${tag}@example.test` });
  await api(page, `/students/${child.id}/parents`, 'POST', { parent_id: parent.id, relationship: 'Родитель', is_primary: true });
  const program = await api(page, '/programs', 'POST', { name: `Программа ${tag}`, status: 'active' });
  const group = await api(page, '/groups', 'POST', { name: `Группа ${tag}`, program_id: program.id, teacher_id: teacher.id, capacity: 10, status: 'active' });
  await api(page, `/groups/${group.id}/enrollments`, 'POST', { student_id: child.id, starts_on: '2000-01-01' });
  const lesson = await api(page, '/lessons', 'POST', { group_id: group.id, teacher_id: teacher.id, topic: `Аргументация ${tag}`, starts_at: new Date(Date.now() - 7200_000).toISOString(), ends_at: new Date(Date.now() - 3600_000).toISOString(), location: 'Класс пилота' });
  const contexts = [];
  async function invite(role: string, profile: number, email: string) {
    await page.goto(`${base}/#/invitations`);
    await page.getByLabel('Роль приглашённого').selectOption(role);
    await expect(page.getByLabel('Карточка приглашённого').locator(`option[value="${profile}"]`)).toHaveCount(1);
    await page.getByLabel('Карточка приглашённого').selectOption(String(profile));
    await page.getByLabel('Email для входа').fill(email);
    await page.getByRole('button', { name: 'Создать приглашение', exact: true }).click();
    await expect(page.getByLabel('Ссылка доступа')).toBeVisible();
    const link = await page.getByLabel('Ссылка доступа').inputValue();
    await accessible(page);
    const context = await browser.newContext({ viewport: page.viewportSize()! }); contexts.push(context);
    const actor = await context.newPage(); actor.on('pageerror', error => errors.push(error.message));
    await actor.goto(link);
    await actor.getByLabel('Новый пароль', { exact: true }).fill(password);
    await actor.getByLabel('Повторите пароль', { exact: true }).fill(password);
    await actor.getByRole('button', { name: 'Установить пароль' }).click();
    await expect(actor.getByText('Пароль установлен. Войдите с вашим email и новым паролем.')).toBeVisible();
    await actor.getByRole('link', { name: 'Перейти ко входу' }).click();
    await login(actor, email, password);
    return actor;
  }
  try {
    const teacherPage = await invite('teacher', teacher.id, `teacher-${tag}@example.test`);
    await expect(teacherPage.getByRole('heading', { name: 'Кабинет педагога', exact: true })).toBeVisible();
    await expect(teacherPage.getByLabel('Мои группы', { exact: true })).toHaveText('1');
    await expect(teacherPage.getByLabel('Мои ученики', { exact: true })).toHaveText('1');
    await teacherPage.goto(`${base}/#/my-groups`);
    await expect(teacherPage.getByRole('table', { name: 'Мои группы', exact: true })).toContainText(`Группа ${tag}`);
    await teacherPage.goto(`${base}/#/my-groups/${group.id}`);
    await expect(teacherPage.getByRole('article', { name: `Ученик ${tag}`, exact: true })).toContainText(`Ученик ${tag}`);
    await accessible(teacherPage);
    const studentPage = await invite('student', child.id, `student-${tag}@example.test`);
    const parentPage = await invite('parent', parent.id, `parent-${tag}@example.test`);
    await teacherPage.goto(`${base}/#/lessons/${lesson.id}`);
    await teacherPage.getByRole('button', { name: 'Все присутствовали', exact: true }).click();
    await teacherPage.getByRole('button', { name: 'Сохранить посещаемость', exact: true }).click();
    await expect(teacherPage.getByRole('button', { name: 'Завершить занятие', exact: true })).toBeEnabled();
    await teacherPage.getByRole('button', { name: 'Завершить занятие', exact: true }).click();
    await teacherPage.getByRole('dialog').getByRole('button', { name: 'Подтвердить' }).click();
    await expect(teacherPage.locator('.badge').filter({ hasText: /^Проведено$/ })).toBeVisible();
    await teacherPage.goto(`${base}/#/assignments`);
    await teacherPage.getByLabel('Занятие и ученик').selectOption(`${lesson.id}:${child.id}`);
    await teacherPage.getByLabel('Название задания').fill('Два аргумента');
    await teacherPage.getByLabel('Условие задания').fill('Напишите два аргумента в пользу совместной работы.');
    await teacherPage.getByRole('button', { name: 'Выдать задание', exact: true }).click();
    await expect(teacherPage.getByText('Задание выдано ученику.')).toBeVisible();
    await studentPage.goto(`${base}/#/assignments`);
    await studentPage.getByRole('button', {name:'Выполнить',exact:true}).click();
    await studentPage.getByLabel('Ваш ответ: Два аргумента').fill('Совместная работа помогает обмениваться идеями и учиться друг у друга.');
    await studentPage.getByRole('button', { name: 'Отправить ответ' }).click();
    await expect(studentPage.getByText('Ответ отправлен на проверку.')).toBeVisible();
    await accessible(studentPage);
    await teacherPage.reload();
    await teacherPage.getByRole('button', {name:'Проверить работу',exact:true}).click();
    await teacherPage.getByLabel('Комментарий к работе: Два аргумента').fill('Работа принята: оба аргумента понятны.');
    await teacherPage.getByRole('button', { name: 'Сохранить проверку' }).click();
    await expect(teacherPage.getByText('Результат проверки сохранён.')).toBeVisible();
    await teacherPage.goto(`${base}/#/reports`);
    await teacherPage.getByLabel('Занятие и ученик').selectOption(`${lesson.id}:${child.id}`);
    await teacherPage.getByLabel('Результат и рекомендации родителю').fill('Ученик сформулировал два аргумента. Рекомендуем продолжать совместные обсуждения дома.');
    await teacherPage.getByRole('button', { name: 'Сохранить черновик' }).click();
    await expect(teacherPage.getByText('Черновик — семье не виден')).toBeVisible();
    await parentPage.reload();
    await expect(parentPage.getByText('Опубликованных результатов пока нет')).toBeVisible();
    await expect(parentPage.getByText('Ученик сформулировал два аргумента.', { exact: false })).toHaveCount(0);
    await teacherPage.getByRole('button', { name: 'Опубликовать родителю' }).click();
    await expect(teacherPage.getByText('Отчёт опубликован и доступен семье.')).toBeVisible();
    await parentPage.reload();
    await expect(parentPage.locator('.entity-card__description').filter({hasText:'Ученик сформулировал два аргумента.'})).toBeVisible();
    await expect(parentPage.getByRole('link', { name: 'Пользователи', exact: true })).toHaveCount(0);
    await expect(parentPage.getByRole('link', { name: 'Задачи', exact: true })).toHaveCount(0);
    await accessible(parentPage);
    await parentPage.screenshot({ path: `../.cache/pilot-parent-${info.project.name}.png`, fullPage: true });
    await studentPage.getByRole('button', { name: 'Выйти', exact: true }).click();
    await studentPage.getByRole('link', { name: 'Забыли пароль? Восстановить доступ' }).click();
    await studentPage.getByLabel('Email', { exact: true }).fill(`student-${tag}@example.test`);
    await studentPage.getByRole('button', { name: 'Запросить восстановление' }).click();
    await expect(studentPage.getByText('Если учётная запись существует', { exact: false })).toBeVisible();
    await page.goto(`${base}/#/invitations`);
    await page.getByRole('button', { name: 'Обновить запросы', exact: true }).click();
    const recovery = page.locator('.pilot-list-row').filter({ hasText: `student-${tag}@example.test` });
    await recovery.getByRole('button', { name: 'Выбрать пользователя' }).click();
    await page.getByRole('button', { name: 'Создать ссылку восстановления' }).click();
    await expect(page.getByLabel('Ссылка доступа')).toBeVisible();
    await studentPage.goto(await page.getByLabel('Ссылка доступа').inputValue());
    await studentPage.getByLabel('Новый пароль', { exact: true }).fill('Updated-Pilot-Browser-2026');
    await studentPage.getByLabel('Повторите пароль', { exact: true }).fill('Updated-Pilot-Browser-2026');
    await studentPage.getByRole('button', { name: 'Установить пароль' }).click();
    await expect(studentPage.getByText('Пароль установлен.', { exact: false })).toBeVisible();
    await login(studentPage, `student-${tag}@example.test`, 'Updated-Pilot-Browser-2026');
    expect(errors).toEqual([]);
  } finally { await Promise.all(contexts.map(context => context.close())); }
});
