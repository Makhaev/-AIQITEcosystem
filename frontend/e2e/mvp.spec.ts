import { expect, test, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';
import AxeBuilder from '@axe-core/playwright';

const access = JSON.parse(readFileSync(process.env.MVP_ACCESS_FILE!, 'utf8'));
const origin = access.app_url;
async function api(page: Page, method: string, path: string, data?: unknown, status = 200) {
 const csrf = await (await page.request.get('/api/v1/auth/csrf')).json();
 const response = await page.request.fetch(`/api/v1${path}`, { method, data, headers: { Origin: origin, 'X-CSRF-Token': csrf.csrf_token } });
 expect(response.status(), `${method} ${path}: ${response.status() === status ? '' : (await response.json()).message}`).toBe(status);
 return status === 204 ? undefined : response.json();
}
async function login(page: Page, email: string, password = access.password) {
 await page.goto('/');
 await page.getByLabel('Email', { exact: true }).fill(email);
 await page.getByLabel('Пароль', { exact: true }).fill(password);
 await page.getByRole('button', { name: 'Войти', exact: true }).click();
 await expect(page.getByRole('button', { name: 'Выйти', exact: true })).toBeVisible();
}
async function mail(page: Page, to: string, marker: string) {
 let found: any;
 await expect.poll(async () => {
  const response = await page.request.get(`${access.mailbox_url}/messages`, { headers: { 'X-Pilot-Key': access.mail_key } });
  const messages = await response.json();
  found = messages.filter((m: any) => m.to === to && m.body.includes(marker)).at(-1);
  return !!found;
 }, { timeout: 30000 }).toBe(true);
 return found.body as string;
}
async function invite(admin: Page, recipient: Page, email: string, role: string, card: number) {
 await admin.goto('/#/invitations');
 await admin.getByLabel('Email получателя').fill(email);
 await admin.getByLabel('Роль получателя').selectOption(role);
 await admin.getByLabel('Существующая карточка').selectOption(String(card));
 await admin.getByRole('button', { name: 'Отправить приглашение', exact: true }).click();
 await expect(admin.getByRole('status').filter({ hasText: 'Приглашение создано.' })).toBeVisible();
 const body = await mail(admin, email, '/#/invitation?');
 expect(body).toContain('48 часов');
 const link = body.match(/http:\/\/localhost:5174\/#\/invitation\?token=[^\s]+/)![0];
 await recipient.goto(link);
 await recipient.getByLabel('Новый пароль', { exact: true }).fill(access.password);
 await recipient.getByLabel('Повторите пароль', { exact: true }).fill(access.password);
 await recipient.getByRole('button', { name: 'Принять приглашение', exact: true }).click();
 await expect(recipient.getByRole('heading', { name: 'Приглашение принято' })).toBeVisible();
 await recipient.goto('/#/family');
}
async function fits(page: Page) {
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
 expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
}

test('real MVP: TLS mail, invitations, assignment, review, family report, request, event and reset', async ({ browser }, info) => {
 const contexts: Awaited<ReturnType<typeof browser.newContext>>[] = [];
 const newPage = async () => { const c = await browser.newContext({ baseURL: origin, viewport: info.project.use.viewport }); contexts.push(c); return c.newPage(); };
 const admin = await newPage(), methodologist = await newPage(), reviewer = await newPage(), teacher = await newPage(), curator = await newPage(), parent = await newPage(), student = await newPage();
 const suffix = `${info.project.name}-${Date.now()}`;
 const parentEmail = `parent-${suffix}@example.test`, studentEmail = `student-${suffix}@example.test`;
 const today = new Date().toLocaleDateString('en-CA', { timeZone: 'Europe/Moscow' });
 try {
  await login(admin, 'admin@example.test');
  await login(methodologist, 'methodologist@example.test');
  await login(reviewer, 'education_leader@example.test');
  await login(teacher, 'teacher@example.test');
  await login(curator, 'curator@example.test');
  const child = await api(admin, 'POST', '/students', { name: `Анна ${suffix}`, birth_date: '2012-05-15', curator_id: access.users.curator }, 201);
  const sibling = await api(admin, 'POST', '/students', { name: `Борис ${suffix}`, curator_id: access.users.curator }, 201);
  const outsider = await api(admin, 'POST', '/students', { name: `Другой ученик ${suffix}` }, 201);
  const parentCard = await api(admin, 'POST', '/parents', { name: `Родитель ${suffix}`, email: parentEmail }, 201);
  for (const c of [child, sibling]) {
   await api(admin, 'POST', `/students/${c.id}/parents`, { parent_id: parentCard.id, relationship: 'Родитель', is_primary: true }, 201);
   await api(admin, 'PUT', `/students/${c.id}/parents/${parentCard.id}/access`, { confirmed: true }, 204);
  }
  const program = await api(methodologist, 'POST', '/programs', { name: `Программа ${suffix}`, status: 'active' }, 201);
  let version = await api(methodologist, 'POST', `/programs/${program.id}/versions`, {}, 201);
  const vp = `/programs/${program.id}/versions/${version.id}`;
  const document = { name: program.name, description: 'Локальная приёмка', sections: [{ key: 's1', name: 'Основы', modules: [{ key: 'm1', name: 'Алгоритмы', lessons: [{ key: 'l1', topic: 'Маршрут робота', goals: 'Объяснить решение', duration_minutes: 45, outline: 'Объяснение и практика', exercises: 'Самостоятельная работа', equipment: 'Компьютер', materials: 'Конспект', criteria: 'Объяснён каждый шаг' }] }] }] };
  version = await api(methodologist, 'PUT', vp, { revision: version.revision, document });
  version = await api(methodologist, 'POST', `${vp}/actions`, { revision: version.revision, action: 'submit' });
  version = await api(reviewer, 'POST', `${vp}/actions`, { revision: version.revision, action: 'publish' });
  const group = await api(admin, 'POST', '/groups', { name: `Группа ${suffix}`, program_id: program.id, program_version_id: version.id, teacher_id: access.teacher_id, capacity: 10, status: 'active' }, 201);
  await api(admin, 'POST', `/groups/${group.id}/enrollments`, { student_id: child.id, starts_on: today }, 201);
  // Separate dates avoid overlapping the same teacher between viewport runs.
  const starts = new Date(Date.now() + (info.project.name === 'desktop' ? 1 : 2) * 86400000);
  const lesson = await api(admin, 'POST', '/lessons', { group_id: group.id, teacher_id: access.teacher_id, topic: 'Маршрут робота', starts_at: starts.toISOString(), ends_at: new Date(+starts + 2700000).toISOString(), version_module_key: 'm1', template_key: 'l1' }, 201);
  const assignment = await api(teacher, 'POST', `/students/${child.id}/education`, { kind: 'assignments', lesson_id: lesson.id, reviewer_id: access.users.teacher, content: { title: `Задание ${suffix}`, instruction: 'Объясните маршрут робота.', criteria: 'Объяснён каждый шаг.', due_on: '2099-12-31', allow_late: false, format: 'text', program_version_id: version.id, template_key: 'l1' } });

  await invite(admin, parent, parentEmail, 'parent', parentCard.id);
  await invite(admin, student, studentEmail, 'student', child.id);
  await expect(student.getByRole('heading', { name: 'Кабинет ученика' })).toBeVisible();
  await student.getByRole('button', { name: 'Открыть задание', exact: true }).click();
  await student.getByLabel('Текст ответа').fill('Робот проверяет препятствие перед каждым шагом.');
  await student.getByRole('button', { name: 'Отправить попытку' }).click();
  await expect(student.getByRole('status').filter({ hasText: 'отправлена на проверку' })).toBeVisible();
  const attempts = await api(student, 'GET', `/family/students/${child.id}/learning?kind=attempts&assignment_id=${assignment.id}`);
  const attempt = attempts.items[0];
  await teacher.goto(`/#/learning/${attempt.id}`);
  await teacher.getByLabel('Действие', { exact: true }).selectOption('approve');
  await teacher.getByLabel('Причина решения').fill('Решение принято: каждый шаг объяснён.');
  await teacher.getByRole('button', { name: 'Сохранить решение' }).click();
  await expect.poll(async () => (await api(teacher, 'GET', `/learning/${attempt.id}`)).state).toBe('accepted');
  await student.getByRole('button', { name: 'Обновить задание' }).click();
  await expect(student.getByText('Принято', { exact: true })).toBeVisible();
  await fits(student);
  await student.screenshot({ path: info.outputPath('student-real.png'), fullPage: true });

  let report = await api(curator, 'POST', `/students/${child.id}/family-reports`, { from: today, to: today, generate: true, reviewer_id: access.users.education_leader, next_focus: 'Добавить проверку границ.' }, 201);
  await curator.goto(`/#/family-reports/${report.id}`);
  await curator.getByRole('button', { name: 'Изменить отчёт' }).click();
  await curator.getByLabel('Название отчёта').fill(`Итоги ${suffix}`);
  await curator.getByLabel('Текст для семьи').fill('Анна объяснила алгоритм и успешно выполнила работу.');
  await curator.getByRole('button', { name: 'Сохранить черновик', exact: true }).click();
  await expect(curator.getByRole('status').filter({ hasText: 'Черновик отчёта сохранён.' })).toBeVisible();
  await curator.getByRole('button', { name: 'Отправить на проверку', exact: true }).click();
  await reviewer.goto(`/#/family-reports/${report.id}`);
  await reviewer.getByRole('button', { name: 'Утвердить', exact: true }).click();
  await reviewer.getByRole('button', { name: 'Опубликовать семье', exact: true }).click();
  await expect(reviewer.getByRole('status').filter({ hasText: 'Отчёт опубликован семье.' })).toBeVisible();
  await parent.goto('/#/family');
  await parent.getByLabel('Ученик', { exact: true }).selectOption(String(sibling.id));
  await expect(parent.getByText('Опубликованных отчётов пока нет.')).toBeVisible();
  await parent.getByLabel('Ученик', { exact: true }).selectOption(String(child.id));
  await parent.getByRole('button', { name: `Открыть отчёт: Итоги ${suffix}` }).click();
  await expect(parent.getByRole('dialog').getByText('Анна объяснила алгоритм и успешно выполнила работу.')).toBeVisible();
  await fits(parent); await parent.screenshot({ path: info.outputPath('family-report-real.png') });
  await parent.keyboard.press('Escape');
  await parent.goto('/#/notifications');
  await expect(parent.getByRole('link', { name: 'Открыть в кабинете' }).first()).toBeVisible();
  await mail(parent, parentEmail, 'В вашем кабинете новое уведомление.');

  await parent.goto('/#/my-requests');
  await parent.getByLabel('Ребёнок (необязательно)').selectOption(String(child.id));
  await parent.getByLabel('Тема', { exact: true }).fill(`Вопрос ${suffix}`);
  await parent.getByLabel('Текст обращения').fill('Какое следующее задание?');
  await parent.getByRole('button', { name: 'Отправить обращение' }).click();
  await expect(parent.getByRole('status').filter({ hasText: 'Обращение отправлено.' })).toBeVisible();
  const requestID = parent.url().split('/').at(-1)!;
  await curator.goto(`/#/family-requests/${requestID}`);
  await curator.getByRole('button', { name: 'Взять в работу' }).click();
  await curator.getByLabel('Новое сообщение').fill('Следующее задание появится после занятия.');
  await curator.getByRole('button', { name: 'Ответить и отметить ответ' }).click();
  await expect(curator.getByRole('status').filter({ hasText: 'Сообщение отправлено.' })).toBeVisible();
  await parent.getByRole('button', { name: 'Обновить обращение', exact: true }).click();
  await expect(parent.getByText('Следующее задание появится после занятия.', { exact: true })).toBeVisible();

  const now = Date.now();
  let event = await api(admin, 'POST', '/events', { title: `Мастер-класс ${suffix}`, type: 'Встреча', description: 'Локальный пилот', organizer: 'AIQ', responsible_ids: [access.users.teacher], time_zone: 'Europe/Moscow', location: 'Зал', capacity: 2, starts_at: new Date(now + 86400000 * 7).toISOString(), ends_at: new Date(now + 86400000 * 7 + 3600000).toISOString(), registration_starts_at: new Date(now - 86400000).toISOString(), registration_ends_at: new Date(now + 86400000 * 6).toISOString(), conditions: 'Бесплатно', required_documents: '' }, 201);
  event = await api(admin, 'POST', `/events/${event.id}/actions`, { action: 'publish', revision: event.revision });
  await parent.goto(`/#/events/${event.id}`);
  await parent.getByLabel('Участник', { exact: true }).selectOption(String(child.id));
  await parent.getByRole('button', { name: 'Подать заявку' }).click();
  await expect(parent.getByRole('status').filter({ hasText: 'Участие: Заявка подана.' })).toBeVisible();
  await admin.goto(`/#/events/${event.id}`);
  await admin.getByRole('button', { name: 'Подтвердить участие' }).click();
  await expect(admin.getByRole('status').filter({ hasText: 'Участие: Подтверждено.' })).toBeVisible();
  await fits(admin);

  // Actual access checks, including a parent knowing a foreign child's ID.
  await api(parent, 'GET', `/family/students/${outsider.id}/parent-home`, undefined, 404);
  const reset = await newPage();
  await reset.goto('/#/forgot-password');
  await reset.getByLabel('Email', { exact: true }).fill(parentEmail);
  await reset.getByRole('button', { name: 'Отправить ссылку' }).click();
  const resetBody = await mail(reset, parentEmail, '/#/reset-password?');
  expect(resetBody).toContain('30 минут');
  const resetLink = resetBody.match(/http:\/\/localhost:5174\/#\/reset-password\?token=[^\s]+/)![0];
  const newPassword = `${access.password}New`;
  await reset.goto(resetLink);
  await reset.getByLabel('Новый пароль', { exact: true }).fill(newPassword);
  await reset.getByLabel('Повторите новый пароль', { exact: true }).fill(newPassword);
  await reset.getByRole('button', { name: 'Сохранить новый пароль' }).click();
  await expect(reset.getByRole('heading', { name: 'Пароль изменён' })).toBeVisible();
  await api(parent, 'GET', '/auth/me', undefined, 401);
  await reset.getByRole('link', { name: 'Войти', exact: true }).click();
  await login(reset, parentEmail, newPassword);
  await api(admin, 'PUT', `/students/${child.id}/parents/${parentCard.id}/access`, { confirmed: false }, 204);
  await api(reset, 'GET', `/family/reports/${report.id}`, undefined, 404);
 } finally { for (const context of contexts) await context.close(); }
});
