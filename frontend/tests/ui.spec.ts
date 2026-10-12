import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { mkdir } from 'node:fs/promises';

const admin = { id: 1, name: 'Администратор AIQ', email: 'admin@example.test', roles: ['admin'], blocked: false, must_change_password: false, created_at: '2026-10-12T08:00:00Z', updated_at: '2026-10-12T08:00:00Z' };
const student = { id: 10, name: 'Тестовый ученик с длинной фамилией', birth_date: '2013-05-10', status: 'active', curator_id: 2, curator_name: 'Тестовый куратор' };
const teacher = { id: 3, name: 'Тестовый педагог', specialization: 'Коммуникация', phone: '+7 900 000-00-00', status: 'active' };
const group = { id: 4, name: 'Группа развития', program_id: 6, program_name: 'Программа AIQ', teacher_id: teacher.id, teacher_name: teacher.name, capacity: 12, active_count: 1, status: 'active', enrollments: [{ id: 5, student_id: student.id, student_name: student.name, starts_on: '2026-09-01', ends_on: null }] };
const program = { id: 6, name: 'Программа AIQ', description: 'Развитие мышления и командного взаимодействия.', status: 'active', modules: [{ id: 7, program_id: 6, name: 'Аргументация', position: 1, description: 'Практика аргументации' }] };
const lesson = { id: 8, topic: 'Думаем и обсуждаем', group_name: group.name, teacher_name: teacher.name, teacher_user_id: admin.id, starts_at: '2020-01-01T08:00:00Z', ends_at: '2020-01-01T09:00:00Z', location: 'Учебный класс', status: 'scheduled' };
const task = { id: 9, title: 'Подготовить материалы к занятию', description: 'Рабочие листы для группы', assignee_name: teacher.name, due_at: '2026-10-15T09:00:00Z', status: 'in_progress' };
const parent = { id: 11, name: 'Тестовый родитель', phone: '+7 900 000-00-01', email: 'parent@example.test', status: 'active', relationship: 'Родитель', is_primary: true };
const data: Record<string, unknown[]> = { teachers: [teacher], students: [student], parents: [parent], groups: [group], programs: [program], lessons: [lesson], tasks: [task], 'audit-logs': [{ id: 12, occurred_at: '2026-10-12T08:00:00Z', actor_name: admin.name, action: 'Создан пользователь', object_type: 'Пользователь', object_id: 2 }] };

async function api(page: Page, signedIn = true, currentUser = admin) {
  let loggedIn = signedIn;
  let teacherRecords = [{ ...teacher, email: 'teacher@example.test', user_id: 2, created_at: admin.created_at, updated_at: admin.updated_at }];
  const familyRecords = { students: [{ ...student }], parents: [{ ...parent }] };
  let familyLinks = [{ student_id: student.id, parent_id: parent.id, relationship: 'Родитель', is_primary: true }];
  let nextFamilyID = 30;
  const programRecords = [{ ...program, modules: program.modules.map(value => ({ ...value })), created_at: admin.created_at, updated_at: admin.updated_at }];
  let nextProgramID = 40, nextModuleID = 50;
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()); const path = url.pathname.replace('/api/v1/', ''); const method = route.request().method();
    const json = (body: unknown, status = 200) => route.fulfill({ status, json: body });
    if (path === 'auth/csrf') return json({ csrf_token: 'fixture-csrf' });
    if (path === 'auth/login') { loggedIn = true; return json({ user: currentUser, csrf_token: 'fixture-csrf-after-login' }); }
    if (path === 'auth/me') return loggedIn ? json(currentUser) : json({ code: 'unauthorized', message: 'Необходимо войти в систему.', field_errors: {} }, 401);
    if (path === 'auth/logout' || path === 'auth/change-password') { loggedIn = false; return route.fulfill({ status: 204 }); }
    if (path === 'users') {
      if (method === 'POST') return json({ ...admin, ...route.request().postDataJSON(), id: 2, roles: ['teacher'], must_change_password: true }, 201);
      const size = Number(url.searchParams.get('page_size') ?? 20);
      if (url.searchParams.get('role') === 'teacher') return json({ items: [{ ...admin, id: 2, name: teacher.name, email: 'teacher@example.test', roles: ['teacher'] }], total: 1, page: 1, page_size: size });
      if (url.searchParams.get('role') === 'curator') return json({ items: [{ ...admin, id: 2, name: student.curator_name, email: 'curator@example.test', roles: ['curator'] }], total: 1, page: 1, page_size: size });
      return json({ items: Array.from({ length: size === 100 ? 2 : 1 }, (_, index) => ({ ...admin, id: index + 1, email: index ? 'teacher@example.test' : admin.email })), total: size === 100 ? 2 : 21, page: Number(url.searchParams.get('page') ?? 1), page_size: size });
    }
    if (path.match(/^users\/\d+$/)) return json({ ...admin, ...route.request().postDataJSON() });
    if (path === 'teachers') {
      if (method === 'POST') { const value = { ...teacher, ...route.request().postDataJSON(), id: 30, created_at: admin.created_at, updated_at: admin.updated_at }; teacherRecords.push(value); return json(value, 201); }
      const search = (url.searchParams.get('search') ?? '').toLowerCase(); const status = url.searchParams.get('status');
      const values = teacherRecords.filter(value => value.name.toLowerCase().includes(search) && (!status || value.status === status) && (currentUser.roles.some(role => role === 'admin' || role === 'leader') || value.user_id === currentUser.id));
      return json({ items: values, total: values.length, page: Number(url.searchParams.get('page') ?? 1), page_size: Number(url.searchParams.get('page_size') ?? 20) });
    }
    if (path.match(/^teachers\/\d+$/)) {
      const id = Number(path.split('/')[1]); const value = teacherRecords.find(value => value.id === id);
      if (!value) return json({ code: 'not_found', message: 'Педагог не найден.', field_errors: {} }, 404);
      if (method === 'PATCH') { const updated = { ...value, ...route.request().postDataJSON() }; teacherRecords = teacherRecords.map(item => item.id === id ? updated : item); return json(updated); }
      return json(value);
    }
    const family = path.match(/^(students|parents)(?:\/(\d+))?(?:\/parents(?:\/(\d+))?)?$/);
    if (family) {
      const kind = family[1] as 'students' | 'parents'; const id = Number(family[2]); const parentID = Number(family[3]);
      type FixturePerson = { id: number; name: string; status: string; phone?: string; email?: string; curator_id?: number | null; curator_name?: string | null };
      const records: FixturePerson[] = familyRecords[kind];
      if (path.includes('/parents') && kind === 'students') {
        const body = route.request().postDataJSON(); const key = parentID || body.parent_id;
        if (body.is_primary && familyLinks.some(value => value.student_id === id && value.parent_id !== key && value.is_primary)) return json({ code: 'primary_contact_exists', message: 'У ученика уже есть основной контакт. Сначала снимите этот признак у текущего контакта.', field_errors: {} }, 409);
        if (method === 'POST' && familyLinks.some(value => value.student_id === id && value.parent_id === key)) return json({ code: 'link_exists', message: 'Этот родитель уже связан с учеником.', field_errors: {} }, 409);
        const value = { ...body, student_id: id, parent_id: key };
        if (method === 'POST') familyLinks.push(value); else familyLinks = familyLinks.map(item => item.student_id === id && item.parent_id === key ? { ...item, ...value } : item);
        return json(value, method === 'POST' ? 201 : 200);
      }
      if (!id && method === 'POST') { const value = { ...route.request().postDataJSON(), id: nextFamilyID++, ...(kind === 'students' ? { curator_name: route.request().postDataJSON().curator_id ? student.curator_name : null } : {}) }; records.push(value); return json(value, 201); }
      if (!id) {
        const search = (url.searchParams.get('search') ?? '').toLowerCase(); const status = url.searchParams.get('status');
        const values = records.filter(value => [value.name, value.phone, value.email].some(text => text?.toLowerCase().includes(search)) && (!status || value.status === status));
        return json({ items: values, total: values.length, page: Number(url.searchParams.get('page') ?? 1), page_size: Number(url.searchParams.get('page_size') ?? 20) });
      }
      const value = records.find(item => item.id === id); if (!value) return json({ code: 'not_found', message: 'Карточка не найдена.', field_errors: {} }, 404);
      if (method === 'PATCH') { Object.assign(value, route.request().postDataJSON()); return json(value); }
      if (kind === 'students') return json({ ...value, parents: familyLinks.filter(item => item.student_id === id).map(item => ({ ...familyRecords.parents.find(person => person.id === item.parent_id), ...item })) });
      return json({ ...value, children: familyLinks.filter(item => item.parent_id === id).map(item => ({ ...familyRecords.students.find(person => person.id === item.student_id), ...item })) });
    }
    if (path === 'groups/4') return json(group);
    const programRoute = path.match(/^programs(?:\/(\d+))?(?:\/modules(?:\/(\d+))?)?$/);
    if (programRoute) {
      const id = Number(programRoute[1]); const moduleID = Number(programRoute[2]);
      if (!id && method === 'POST') { const value = { ...program, ...route.request().postDataJSON(), id: nextProgramID++, modules: [], created_at: admin.created_at, updated_at: admin.updated_at }; programRecords.push(value); return json(value, 201); }
      if (!id) { const search = (url.searchParams.get('search') ?? '').toLowerCase(); const status = url.searchParams.get('status'); const values = programRecords.filter(value => value.name.toLowerCase().includes(search) && (!status || value.status === status) && (currentUser.roles.some(role => role === 'admin' || role === 'leader') || value.id === program.id)); return json({ items: values, total: values.length, page: Number(url.searchParams.get('page') ?? 1), page_size: Number(url.searchParams.get('page_size') ?? 20) }); }
      const value = programRecords.find(item => item.id === id); if (!value) return json({ code: 'not_found', message: 'Программа или модуль не найдены.', field_errors: {} }, 404);
      if (path.includes('/modules')) {
        if (method === 'GET') return json(value.modules.find(item => item.id === moduleID));
        if (value.status === 'archived') return json({ code: 'program_archived', message: 'Для изменения модулей сначала восстановите программу из архива.', field_errors: {} }, 409);
        const body = route.request().postDataJSON();
        if (value.modules.some(item => item.position === body.position && item.id !== moduleID)) return json({ code: 'module_position_exists', message: 'В этой программе уже есть модуль с таким номером. Выберите свободный номер.', field_errors: {} }, 409);
        if (method === 'POST') { const module = { ...body, id: nextModuleID++, program_id: id }; value.modules.push(module); value.modules.sort((a, b) => a.position - b.position); return json(module, 201); }
        const module = value.modules.find(item => item.id === moduleID); if (!module) return json({ code: 'not_found', message: 'Программа или модуль не найдены.', field_errors: {} }, 404); Object.assign(module, body); value.modules.sort((a, b) => a.position - b.position); return json(module);
      }
      if (method === 'PATCH') Object.assign(value, route.request().postDataJSON());
      return json(value);
    }
    if (path === 'lessons/8') return json(lesson);
    if (path === 'lessons/8/attendance') return method === 'GET' ? json({ items: [{ student_id: student.id, name: student.name, status: 'unmarked', comment: '' }] }) : route.fulfill({ status: 204 });
    if (path === 'dashboard') return json({ active_students: 1, active_groups: 1, active_teachers: 1, lessons_today: 0, attendance_percent: 66.67, overdue_tasks: 0 });
    const kind = path.split('/')[0];
    if (data[kind]) return method === 'GET' ? json({ items: data[kind], total: data[kind].length, page: 1, page_size: 20 }) : json({ id: 25, ...route.request().postDataJSON() }, 201);
    return json({ code: 'not_found', message: 'Маршрут не найден.', field_errors: {} }, 404);
  });
}
async function accessible(page: Page) {
  const result = await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze();
  expect(result.violations.map(item => ({ id: item.id, nodes: item.nodes.map(node => ({ target: node.target, reason: node.failureSummary })) }))).toEqual([]);
}
async function fits(page: Page) {
  await page.evaluate(() => document.fonts.ready);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  const tooSmall = await page.locator('p, label, input, select, button, td, th, .badge, .navigation a').evaluateAll(elements => elements.filter(element => element.getClientRects().length && parseFloat(getComputedStyle(element).fontSize) < 16).map(element => element.textContent));
  expect(tooSmall).toEqual([]);
}
async function screenshot(page: Page, name: string, project: string) {
  await mkdir('../docs/frontend/screenshots', { recursive: true });
  await page.screenshot({ path: `../docs/frontend/screenshots/${name}-${project}.png`, fullPage: await page.getByRole('dialog').count() === 0 });
}

test('brand palette, Cyrillic font, login keyboard and contrast', async ({ page }, info) => {
  await api(page, false); await page.goto('/'); await expect(page.getByRole('heading', { name: 'Вход в AIQ Core' })).toBeVisible();
  const palette = await page.evaluate(() => { const style = getComputedStyle(document.documentElement); return ['--aiq-green', '--aiq-deep-green', '--aiq-ink', '--aiq-warm-white', '--aiq-soft-green', '--aiq-white'].map(key => { const value = style.getPropertyValue(key).trim().toUpperCase(); return /^#[0-9A-F]{3}$/.test(value) ? '#' + [...value.slice(1)].map(character => character + character).join('') : value; }); });
  expect(palette).toEqual(['#087F3D', '#05652F', '#1D1D1B', '#F7F8F6', '#EAF5EE', '#FFFFFF']);
  expect(await page.locator('body').evaluate(element => getComputedStyle(element).fontFamily)).toContain('Noto Sans');
  await page.keyboard.press('Tab'); await expect(page.getByRole('textbox', { name: 'Email' })).toBeFocused();
  await page.keyboard.type('admin@example.test'); await page.keyboard.press('Tab'); await expect(page.getByLabel('Пароль', { exact: true })).toBeFocused();
  await page.keyboard.type('TestOnly-Password-1234'); await fits(page); await accessible(page); await screenshot(page, 'login', info.project.name);
  await page.keyboard.press('Enter'); await expect(page.getByRole('heading', { name: 'Пользователи', exact: true })).toBeVisible();
});

test('users, pagination and modal keyboard focus', async ({ page }, info) => {
  await api(page); await page.goto('/#/users'); await expect(page.getByRole('table', { name: 'Пользователи AIQ' })).toBeVisible(); await fits(page); await accessible(page); await screenshot(page, 'users', info.project.name);
  await page.getByRole('button', { name: 'Следующая страница' }).click(); await expect(page.getByText('Страница 2 из 2')).toBeVisible();
  const add = page.getByRole('button', { name: 'Добавить пользователя' }); await add.click(); const dialog = page.getByRole('dialog', { name: 'Новый пользователь' }); await expect(dialog).toBeVisible(); await expect(page.getByLabel('Имя и фамилия')).toBeFocused();
  for (let index = 0; index < 14; index++) { await page.keyboard.press('Tab'); expect(await page.evaluate(() => !!document.activeElement?.closest('dialog'))).toBe(true); }
  await fits(page); await accessible(page); await screenshot(page, 'user-modal', info.project.name); await page.keyboard.press('Escape'); await expect(dialog).not.toBeVisible(); await expect(add).toBeFocused();
  await add.click(); await page.getByLabel('Имя и фамилия').fill('Новый педагог'); await page.getByLabel('Email', { exact: true }).fill('new@example.test'); await page.getByLabel('Временный пароль', { exact: true }).fill('TestOnly-Teacher-1234'); await page.getByRole('button', { name: 'Создать пользователя' }).click(); await expect(page.getByRole('status').filter({ hasText: 'Пользователь создан' })).toBeVisible();
});

test('all specified screens, details and attendance fit 360 px', async ({ page }) => {
  await api(page);
  for (const [path, title] of [['dashboard', 'Обзор'], ['teachers', 'Педагоги'], ['students', 'Ученики'], ['parents', 'Родители'], ['groups', 'Группы'], ['programs', 'Программы'], ['lessons', 'Расписание'], ['tasks', 'Задачи'], ['audit', 'История действий'], ['account', 'Мой аккаунт'], ['students/10', student.name], ['groups/4', group.name], ['programs/6', program.name], ['lessons/8', lesson.topic]]) {
    await page.goto(`/#/${path}`); await expect(page.getByRole('heading', { name: title, exact: true }).first()).toBeVisible(); await expect(page.getByText('Загружаем данные…')).not.toBeVisible(); await fits(page); await accessible(page);
  }
  await page.getByRole('button', { name: 'Все присутствовали' }).click(); await expect(page.getByLabel(`Посещаемость: ${student.name}`)).toHaveValue('present'); await page.getByRole('button', { name: 'Сохранить посещаемость' }).click(); await expect(page.getByRole('status').filter({ hasText: 'Посещаемость сохранена' })).toBeVisible();
});

test('education forms and unavailable modules show honest states', async ({ page }) => {
  await api(page); await page.goto('/#/groups'); await page.getByRole('button', { name: 'Добавить: группа' }).click(); await expect(page.getByRole('dialog')).toBeVisible(); await expect(page.getByLabel('Программа', { exact: true })).toBeEnabled(); await fits(page); await accessible(page); await page.keyboard.press('Escape');
  await page.route('**/api/v1/tasks?**', route => route.fulfill({ status: 404, json: { code: 'not_found', message: 'Маршрут не найден.', field_errors: {} } })); await page.goto('/#/tasks'); await expect(page.getByText('Этот раздел готовится к запуску. Данные пока недоступны.')).toBeVisible(); await expect(page.getByRole('button', { name: 'Добавить: задача' })).toBeDisabled(); await fits(page); await accessible(page);
});

test('mobile menu, active navigation and keyboard skip link', async ({ page }) => {
  await api(page); await page.goto('/#/users'); await expect(page.getByRole('heading', { name: 'Пользователи', exact: true })).toBeVisible();
  if (page.viewportSize()!.width < 1024) { await page.getByRole('button', { name: 'Меню', exact: true }).click(); await expect(page.getByRole('dialog', { name: 'Разделы AIQ' })).toBeVisible(); await fits(page); await accessible(page); await page.getByRole('link', { name: 'Расписание', exact: true }).click(); await expect(page.getByRole('heading', { name: 'Расписание', exact: true }).first()).toBeVisible(); await expect(page.getByRole('dialog')).not.toBeVisible(); }
  else { await page.getByRole('link', { name: 'Перейти к содержимому' }).focus(); await page.keyboard.press('Enter'); await expect(page.locator('#main-content')).toBeFocused(); await expect(page.getByRole('link', { name: 'Пользователи', exact: true })).toHaveAttribute('aria-current', 'page'); }
});

test('error messages, confirmation and password validation remain accessible', async ({ page }) => {
  await api(page); await page.goto('/#/users');
  await page.getByRole('button', { name: 'Заблокировать пользователя Администратор AIQ' }).click();
  await expect(page.getByRole('dialog', { name: 'Заблокировать пользователя?' })).toBeVisible(); await fits(page); await accessible(page); await page.keyboard.press('Escape');
  await page.getByRole('button', { name: 'Добавить пользователя' }).click();
  await page.route('**/api/v1/users', route => route.request().method() === 'POST' ? route.fulfill({ status: 409, json: { code: 'email_exists', message: 'Такой email уже используется.', field_errors: { email: 'Укажите другой email.' } } }) : route.fallback());
  await page.getByLabel('Имя и фамилия').fill('Тестовый педагог'); await page.getByLabel('Email', { exact: true }).fill(admin.email); await page.getByLabel('Временный пароль', { exact: true }).fill('TestOnly-Teacher-1234'); await page.getByRole('button', { name: 'Создать пользователя' }).click();
  await expect(page.getByRole('alert')).toHaveText('Такой email уже используется.'); await expect(page.getByLabel('Email', { exact: true })).toHaveAttribute('aria-invalid', 'true'); await fits(page); await accessible(page); await page.keyboard.press('Escape');
  await page.goto('/#/account'); await page.getByLabel('Текущий пароль', { exact: true }).fill('TestOnly-Old-1234'); await page.getByLabel('Новый пароль', { exact: true }).fill('TestOnly-New-1234'); await page.getByLabel('Повторите новый пароль', { exact: true }).fill('TestOnly-Different-1234'); await page.getByRole('button', { name: 'Сохранить пароль' }).click(); await expect(page.getByText('Новые пароли не совпадают.')).toBeVisible(); await fits(page); await accessible(page);
});

test('teachers support create, edit, detail, literal search and archive', async ({ page }, info) => {
  await api(page); await page.goto('/#/teachers'); await expect(page.getByRole('table', { name: 'Педагоги AIQ' })).toBeVisible(); await fits(page); await accessible(page); await screenshot(page, 'teachers', info.project.name);
  await page.getByRole('button', { name: `Открыть профиль ${teacher.name}` }).click(); await expect(page.getByRole('dialog', { name: teacher.name })).toBeVisible(); await fits(page); await accessible(page); await page.keyboard.press('Escape');
  await page.getByRole('button', { name: 'Добавить педагога' }).click(); await expect(page.getByLabel('Учётная запись', { exact: true })).toBeEnabled();
  await page.getByLabel('Имя и фамилия').fill('Новый педагог'); await page.getByLabel('Телефон', { exact: true }).fill('+7 900 123-45-67'); await page.getByLabel('Email', { exact: true }).fill('new-teacher@example.test'); await page.getByLabel('Специализация', { exact: true }).fill('Командная работа'); await fits(page); await accessible(page); await screenshot(page, 'teacher-modal', info.project.name);
  await page.getByRole('button', { name: 'Сохранить педагога' }).click(); await expect(page.getByRole('status').filter({ hasText: 'Профиль педагога сохранён' })).toBeVisible();
  await page.getByLabel('Поиск по ФИО').fill('НОВЫЙ'); await page.getByRole('button', { name: 'Применить' }).click(); await expect(page.getByRole('button', { name: `Изменить педагога ${teacher.name}` })).not.toBeVisible();
  await page.getByRole('button', { name: 'Изменить педагога Новый педагог' }).click(); await page.getByLabel('Специализация', { exact: true }).fill('Мышление'); await page.getByRole('button', { name: 'Сохранить педагога' }).click(); await expect(page.getByText('Мышление', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Архивировать педагога Новый педагог' }).click(); await expect(page.getByRole('dialog', { name: 'Перенести педагога в архив?' })).toBeVisible(); await accessible(page); await page.getByRole('button', { name: 'Перенести в архив', exact: true }).click(); await expect(page.getByRole('table', { name: 'Педагоги AIQ' }).getByText('В архиве', { exact: true })).toBeVisible(); await fits(page); await accessible(page);
});

test('teacher and leader screens are read only, curator has no teacher directory', async ({ page }) => {
  for (const role of ['leader', 'teacher', 'curator']) {
    await page.unrouteAll(); await api(page, true, { ...admin, id: 2, roles: [role] }); await page.goto('/#/teachers'); await page.reload();
    if (role === 'curator') { await expect(page.getByRole('heading', { name: 'Раздел недоступен', exact: true })).toBeVisible(); }
    else { await expect(page.getByRole('table', { name: 'Педагоги AIQ' })).toBeVisible(); await expect(page.getByRole('button', { name: 'Добавить педагога' })).not.toBeVisible(); await expect(page.getByRole('button', { name: `Изменить педагога ${teacher.name}` })).not.toBeVisible(); }
    await fits(page); await accessible(page);
  }
});

test('families support contacts, cards, shared parents, links and archival', async ({ page }, info) => {
  await api(page); await page.goto('/#/parents'); await expect(page.getByRole('table', { name: 'Родители AIQ' })).toBeVisible(); await fits(page); await accessible(page); await screenshot(page, 'parents', info.project.name);
  const addParent = page.getByRole('button', { name: 'Добавить родителя', exact: true }); await addParent.click();
  let dialog = page.getByRole('dialog'); await expect(dialog.getByLabel('Имя и фамилия')).toBeFocused();
  await dialog.getByLabel('Имя и фамилия').fill('Новый родитель'); await dialog.getByRole('button', { name: 'Сохранить родителя' }).click();
  await expect(dialog.getByRole('alert')).toContainText('Укажите хотя бы один контакт'); await expect(dialog.getByLabel('Телефон', { exact: true })).toHaveAttribute('aria-invalid', 'true'); await fits(page); await accessible(page);
  await dialog.getByLabel('Email', { exact: true }).fill('new-parent@example.test'); await dialog.getByRole('button', { name: 'Сохранить родителя' }).click(); await expect(page.getByRole('link', { name: 'Новый родитель', exact: true })).toBeVisible();
  await page.getByLabel('Поиск по ФИО или контакту').fill('NEW-PARENT@EXAMPLE.TEST'); await page.getByRole('button', { name: 'Применить', exact: true }).click(); await expect(page.getByRole('link', { name: parent.name, exact: true })).not.toBeVisible(); await expect(page.getByRole('link', { name: 'Новый родитель', exact: true })).toBeVisible();
  await page.goto('/#/students'); await expect(page.getByRole('table', { name: 'Ученики AIQ' })).toBeVisible(); await fits(page); await accessible(page); await screenshot(page, 'students', info.project.name);
  const add = page.getByRole('button', { name: 'Добавить ученика', exact: true }); await add.click(); dialog = page.getByRole('dialog');
  await expect(dialog.getByLabel('Куратор', { exact: true })).toBeEnabled(); await dialog.getByLabel('Имя и фамилия').fill('Новый ученик'); await dialog.getByLabel('Куратор', { exact: true }).selectOption('2');
  for (let index = 0; index < 12; index++) { await page.keyboard.press('Tab'); expect(await page.evaluate(() => !!document.activeElement?.closest('dialog'))).toBe(true); }
  await fits(page); await accessible(page); await screenshot(page, 'student-modal', info.project.name); await page.keyboard.press('Escape'); await expect(add).toBeFocused();
  await add.click(); dialog = page.getByRole('dialog'); await expect(dialog.getByLabel('Куратор', { exact: true })).toBeEnabled(); await dialog.getByLabel('Имя и фамилия').fill('Новый ученик'); await dialog.getByLabel('Дата рождения').fill('2014-06-10'); await dialog.getByLabel('Куратор', { exact: true }).selectOption('2'); await dialog.getByRole('button', { name: 'Сохранить ученика' }).click();
  await page.getByRole('link', { name: 'Новый ученик', exact: true }).click(); await expect(page.getByRole('heading', { name: 'Новый ученик', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Связать с родителем', exact: true }).click(); dialog = page.getByRole('dialog'); await expect(dialog.getByLabel('Родитель', { exact: true })).toBeEnabled();
  await dialog.getByLabel('Родитель', { exact: true }).selectOption('30'); await dialog.getByLabel('Тип представительства').fill('Опекун'); await dialog.getByLabel('Основной контакт', { exact: true }).selectOption('true'); await fits(page); await accessible(page); await screenshot(page, 'parent-link-modal', info.project.name);
  await dialog.getByRole('button', { name: 'Сохранить связь' }).click(); await expect(page.getByRole('table', { name: 'Родители ученика' }).getByText('Основной контакт', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Связать с родителем', exact: true }).click(); dialog = page.getByRole('dialog'); await expect(dialog.getByLabel('Родитель', { exact: true })).toBeEnabled(); await dialog.getByLabel('Родитель', { exact: true }).selectOption('11'); await dialog.getByLabel('Основной контакт', { exact: true }).selectOption('true'); await dialog.getByRole('button', { name: 'Сохранить связь' }).click();
  await expect(dialog.getByRole('alert')).toContainText('У ученика уже есть основной контакт'); await fits(page); await accessible(page); await dialog.getByLabel('Основной контакт', { exact: true }).selectOption('false'); await dialog.getByRole('button', { name: 'Сохранить связь' }).click();
  await page.getByRole('button', { name: 'Изменить связь Новый родитель', exact: true }).click(); dialog = page.getByRole('dialog'); await dialog.getByLabel('Тип представительства').fill('Законный представитель'); await dialog.getByRole('button', { name: 'Сохранить связь' }).click(); await expect(page.getByText('Законный представитель', { exact: true })).toBeVisible(); await fits(page); await accessible(page); await screenshot(page, 'student-card', info.project.name);
  await page.getByRole('link', { name: parent.name, exact: true }).click(); await expect(page.getByRole('table', { name: 'Дети родителя' }).getByRole('link', { name: 'Новый ученик', exact: true })).toBeVisible(); await expect(page.getByRole('table', { name: 'Дети родителя' }).getByRole('link', { name: student.name, exact: true })).toBeVisible(); await fits(page); await accessible(page); await screenshot(page, 'parent-card', info.project.name);
  await page.goto('/#/students'); await page.getByRole('button', { name: 'Архивировать ученика Новый ученик' }).click(); await expect(page.getByRole('dialog', { name: 'Перенести карточку в архив?' })).toBeVisible(); await page.getByRole('button', { name: 'Перенести в архив', exact: true }).click(); await expect(page.getByRole('table', { name: 'Ученики AIQ' }).getByText('В архиве', { exact: true })).toBeVisible();
  await page.getByRole('link', { name: 'Новый ученик', exact: true }).click(); await expect(page.getByRole('table', { name: 'Родители ученика' })).toBeVisible(); await expect(page.getByRole('button', { name: 'Связать с родителем', exact: true })).not.toBeVisible();
  await page.getByRole('button', { name: 'Изменить карточку' }).click(); dialog = page.getByRole('dialog'); await expect(dialog.getByLabel('Куратор', { exact: true })).toBeEnabled(); await dialog.getByLabel('Статус', { exact: true }).selectOption('active'); await dialog.getByLabel('Дата рождения').fill(''); await dialog.getByRole('button', { name: 'Сохранить ученика' }).click(); await expect(page.getByRole('button', { name: 'Связать с родителем', exact: true })).toBeVisible();
  await page.goto('/#/parents'); await page.getByRole('button', { name: 'Архивировать родителя Новый родитель' }).click(); await page.getByRole('button', { name: 'Перенести в архив', exact: true }).click(); await page.getByRole('link', { name: 'Новый родитель', exact: true }).click(); await expect(page.getByRole('table', { name: 'Дети родителя' })).toBeVisible(); await fits(page); await accessible(page);
});

test('family cards show read-only controls and ID errors accessibly', async ({ page }) => {
  for (const role of ['leader', 'curator']) {
    await page.unrouteAll(); await api(page, true, { ...admin, id: 2, roles: [role] }); await page.goto('/#/students'); await page.reload();
    await expect(page.getByRole('table', { name: 'Ученики AIQ' })).toBeVisible(); await expect(page.getByRole('button', { name: 'Добавить ученика', exact: true })).not.toBeVisible();
    await page.getByRole('link', { name: student.name, exact: true }).click(); await expect(page.getByRole('table', { name: 'Родители ученика' })).toBeVisible(); await expect(page.getByRole('button', { name: 'Изменить карточку' })).not.toBeVisible(); await expect(page.getByRole('button', { name: 'Связать с родителем', exact: true })).not.toBeVisible(); await fits(page); await accessible(page);
  }
  await page.goto('/#/students/99999'); await expect(page.getByRole('alert')).toContainText('Карточка не найдена'); await fits(page); await accessible(page);
});

test('programs support status, ordered modules, conflict, editing and archive', async ({ page }, info) => {
  await api(page); await page.goto('/#/programs'); await expect(page.getByRole('table', { name: 'Программы AIQ' })).toBeVisible(); await fits(page); await accessible(page); await screenshot(page, 'programs', info.project.name);
  const add = page.getByRole('button', { name: 'Добавить программу', exact: true }); await add.click(); let dialog = page.getByRole('dialog'); await expect(dialog.getByLabel('Название', { exact: true })).toBeFocused();
  await dialog.getByLabel('Название', { exact: true }).fill('Новая программа'); await dialog.getByLabel('Описание', { exact: true }).fill('Первая строка\nВторая строка'); await expect(dialog.getByLabel('Статус', { exact: true })).toHaveValue('draft'); await fits(page); await accessible(page); await screenshot(page, 'program-modal', info.project.name);
  await page.keyboard.press('Escape'); await expect(add).toBeFocused(); await add.click(); dialog = page.getByRole('dialog'); await dialog.getByLabel('Название', { exact: true }).fill('Новая программа'); await dialog.getByLabel('Описание', { exact: true }).fill('Первая строка\nВторая строка'); await dialog.getByRole('button', { name: 'Сохранить программу' }).click();
  await page.getByLabel('Поиск по названию').fill('НОВАЯ'); await page.getByRole('button', { name: 'Применить', exact: true }).click(); await expect(page.getByRole('link', { name: program.name, exact: true })).not.toBeVisible(); await page.getByRole('link', { name: 'Новая программа', exact: true }).click(); await expect(page.locator('.badge').filter({ hasText: /^Черновик$/ })).toBeVisible();
  await page.getByRole('button', { name: 'Добавить модуль', exact: true }).click(); dialog = page.getByRole('dialog'); await dialog.getByLabel('Название', { exact: true }).fill('Второй модуль'); await dialog.getByLabel('Описание', { exact: true }).fill('Описание второго модуля'); await dialog.getByLabel('Порядковый номер').fill('2'); await fits(page); await accessible(page); await screenshot(page, 'module-modal', info.project.name); await dialog.getByRole('button', { name: 'Сохранить модуль' }).click();
  await page.getByRole('button', { name: 'Добавить модуль', exact: true }).click(); dialog = page.getByRole('dialog'); await dialog.getByLabel('Название', { exact: true }).fill('Первый модуль'); await dialog.getByLabel('Порядковый номер').fill('2'); await dialog.getByRole('button', { name: 'Сохранить модуль' }).click(); await expect(dialog.getByRole('alert')).toContainText('уже есть модуль с таким номером'); await fits(page); await accessible(page); await dialog.getByLabel('Порядковый номер').fill('1'); await dialog.getByRole('button', { name: 'Сохранить модуль' }).click();
  const table = page.getByRole('table', { name: 'Модули программы', exact: true }); await expect(table.locator('tbody tr').first()).toContainText('Первый модуль'); await expect(table.locator('tbody tr').last()).toContainText('Второй модуль');
  await page.getByRole('button', { name: 'Изменить модуль Второй модуль', exact: true }).click(); dialog = page.getByRole('dialog'); await dialog.getByLabel('Название', { exact: true }).fill('Изменённый модуль'); await dialog.getByLabel('Порядковый номер').fill('3'); await dialog.getByRole('button', { name: 'Сохранить модуль' }).click(); await expect(table.locator('tbody tr').last()).toContainText('Изменённый модуль');
  await page.getByRole('button', { name: 'Изменить программу', exact: true }).click(); dialog = page.getByRole('dialog'); await dialog.getByLabel('Статус', { exact: true }).selectOption('active'); await dialog.getByRole('button', { name: 'Сохранить программу' }).click(); await expect(page.locator('.badge').filter({ hasText: /^Активна$/ })).toBeVisible(); await fits(page); await accessible(page); await screenshot(page, 'program-card', info.project.name);
  await page.goto('/#/programs'); await page.getByRole('button', { name: 'Архивировать программу Новая программа', exact: true }).click(); await expect(page.getByRole('dialog', { name: 'Перенести программу в архив?' })).toBeVisible(); await fits(page); await accessible(page); await page.getByRole('button', { name: 'Перенести в архив', exact: true }).click(); await page.getByRole('link', { name: 'Новая программа', exact: true }).click(); await expect(page.locator('.badge').filter({ hasText: /^В архиве$/ })).toBeVisible(); await expect(page.getByRole('table', { name: 'Модули программы', exact: true })).toBeVisible(); await expect(page.getByRole('button', { name: 'Добавить модуль', exact: true })).not.toBeVisible();
  await page.getByRole('button', { name: 'Изменить программу', exact: true }).click(); dialog = page.getByRole('dialog'); await dialog.getByLabel('Статус', { exact: true }).selectOption('active'); await dialog.getByRole('button', { name: 'Сохранить программу' }).click(); await expect(page.getByRole('button', { name: 'Добавить модуль', exact: true })).toBeVisible();
});

test('program read-only roles and unavailable IDs remain accessible', async ({ page }) => {
  for (const role of ['leader', 'teacher', 'curator']) {
    await page.unrouteAll(); await api(page, true, { ...admin, id: 2, roles: [role] }); await page.goto('/#/programs'); await page.reload();
    await expect(page.getByRole('table', { name: 'Программы AIQ' })).toBeVisible(); await expect(page.getByRole('button', { name: 'Добавить программу', exact: true })).not.toBeVisible(); await page.getByRole('link', { name: program.name, exact: true }).click();
    await expect(page.getByRole('table', { name: 'Модули программы', exact: true })).toBeVisible(); await expect(page.getByRole('button', { name: 'Добавить модуль', exact: true })).not.toBeVisible(); await expect(page.getByRole('button', { name: 'Изменить программу', exact: true })).not.toBeVisible(); await fits(page); await accessible(page);
  }
  await page.goto('/#/programs/99999'); await expect(page.getByRole('alert')).toContainText('Программа или модуль не найдены'); await fits(page); await accessible(page);
});
