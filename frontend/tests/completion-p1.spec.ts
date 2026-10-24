import { expect, test, type Page } from '@playwright/test';

const admin = { id: 1, name: 'Администратор примера', email: 'admin@example.test', roles: ['admin'], blocked: false, must_change_password: false };
const envelope = (items: unknown[]) => ({ items, total: items.length, page: 1, page_size: 20 });
async function auth(page: Page, role = 'admin') {
  await page.route('**/api/v1/auth/**', route => route.fulfill({ json: route.request().url().endsWith('/csrf') ? { csrf_token: 'example' } : { ...admin, roles: [role] } }));
}
async function mobileFits(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
}

test('contact match failure retries; duplicate warning requires explicit confirmation and rechecks edited contacts', async ({ page }) => {
  await auth(page); let checks = 0, saves = 0;
  await page.route('**/api/v1/parents**', route => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/report')) return route.fulfill({ json: { current: null, history: [], can_edit: false } });
    if (path.endsWith('/contact-matches')) {
      checks++;
      if (checks === 1) return route.fulfill({ status: 503, json: { code: 'unavailable', message: 'Проверка контактов временно недоступна', field_errors: {}, request_id: 'example' } });
      return route.fulfill({ json: envelope([{ id: 8, name: 'Совпадение в архиве', status: 'archived', matched_fields: ['email'] }]) });
    }
    if (route.request().method() === 'POST') { saves++; return route.fulfill({ status: 201, json: { id: 9, ...route.request().postDataJSON() } }); }
    return route.fulfill({ json: envelope([]) });
  });
  await page.goto('/#/parents'); await page.getByRole('button', { name: 'Добавить родителя' }).click();
  const dialog = page.getByRole('dialog'); await dialog.getByLabel('Имя и фамилия').fill('Новый представитель'); await dialog.getByLabel('Email', { exact: true }).fill('duplicate@example.test');
  await dialog.getByRole('button', { name: 'Сохранить родителя', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('Проверка контактов временно недоступна'); expect(saves).toBe(0);
  await dialog.getByRole('button', { name: 'Повторить проверку', exact: true }).click();
  await expect(dialog.getByRole('link', { name: 'Совпадение в архиве' })).toBeVisible(); await expect(dialog.getByText(/Возможный дубль/)).toBeVisible(); expect(saves).toBe(0);
  await dialog.getByLabel('Email', { exact: true }).fill('another@example.test');
  await expect(dialog.getByRole('button', { name: 'Подтвердить сохранение отдельно' })).toHaveCount(0);
  await dialog.getByRole('button', { name: 'Сохранить родителя', exact: true }).click();
  await expect(dialog.getByRole('button', { name: 'Подтвердить сохранение отдельно' })).toBeVisible(); expect(checks).toBe(3);
  await mobileFits(page); await dialog.getByRole('button', { name: 'Подтвердить сохранение отдельно' }).click();
  await expect(dialog).not.toBeVisible(); expect(saves).toBe(1); expect(checks).toBe(3);
});

async function lessonFixture(page: Page, completed = false, role = 'admin', versionedModule = false) {
  await auth(page, role);
  const l = { id: 8, group_id: 4, group_name: 'Группа примера', program_id: 6, program_name: 'Программа в истории', module_id: versionedModule ? null : 7, version_module_key: versionedModule ? 'm2-module' : '', module_name: 'Модуль в истории', topic: 'Сохранённая тема', teacher_id: 3, teacher_name: 'Педагог примера', teacher_user_id: 2, starts_at: '2026-10-01T07:00:00Z', ends_at: '2026-10-01T08:00:00Z', location: 'Учебный класс', status: completed ? 'completed' : 'scheduled', roster_initialized_at: '2026-10-01T06:00:00Z', snapshot_state: completed ? 'captured' : 'not_captured' };
  const enrollments = [{ id: 5, student_id: 10, student_name: 'Прежний участник', starts_on: '2026-09-01', ends_on: '2026-10-02' }, { id: 6, student_id: 11, student_name: 'Новый участник', starts_on: '2026-10-03', ends_on: null }];
  let participants = [{ id: 20, enrollment_id: 5, student_id: 10, name: 'Прежний участник', status: 'present', comment: '' }];
  const versions: unknown[] = []; let fail = true; let failLookup = false; let submitted: Record<string, unknown> | null = null;
  await page.route('**/api/v1/groups?**', route => route.fulfill({ json: envelope([{ id: 4, name: l.group_name, program_id: 6, teacher_id: 3 }]) }));
  await page.route('**/api/v1/teachers?**', route => route.fulfill({ json: envelope([{ id: 3, name: l.teacher_name }]) }));
  await page.route('**/api/v1/programs/6', route => route.fulfill({ json: { modules: [{ id: 7, name: l.module_name }] } }));
  await page.route('**/api/v1/lessons?**', route => route.fulfill({ json: envelope([l]) }));
  await page.route('**/api/v1/groups/4', route => {
    if (failLookup) { failLookup = false; return route.fulfill({ status: 503, json: { code: 'unavailable', message: 'Состав временно недоступен.' } }); }
    return route.fulfill({ json: { id: 4, enrollments } });
  });
  await page.route('**/api/v1/lessons/8**', route => {
    const path = new URL(route.request().url()).pathname;
    if (path.endsWith('/report')) return route.fulfill({ json: { current: null, history: [], can_edit: false } });
    if (path.endsWith('/attendance')) return route.fulfill({ json: { lesson_id: 8, lesson_date: l.starts_at.slice(0, 10), initialized_at: l.roster_initialized_at, has_marks: participants.some(e => e.status !== 'unmarked'), items: participants } });
    if (path.endsWith('/snapshot-history')) return route.fulfill({ json: envelope(versions) });
    if (path.endsWith('/reschedule') || path.endsWith('/snapshot')) {
      submitted = route.request().postDataJSON();
      if (fail) { fail = false; return route.fulfill({ status: 409, json: { code: 'conflict', message: 'Проверьте конфликт и повторите сохранение.', field_errors: {}, request_id: 'example' } }); }
      if (path.endsWith('/reschedule')) {
        Object.assign(l, { starts_at: submitted!.starts_at, ends_at: submitted!.ends_at });
        participants = enrollments.filter(e => (submitted!.enrollment_ids as number[]).includes(e.id)).map(e => ({ id: 21, enrollment_id: e.id, student_id: e.student_id, name: e.student_name, status: 'unmarked', comment: '' }));
      } else {
        versions.push({ id: 1, actor_id: 1, program_name: l.program_name, module_name: l.module_name, topic: l.topic, captured_at: l.ends_at, preserved_at: l.ends_at, state: 'captured', reason: submitted!.reason });
        Object.assign(l, { program_name: submitted!.program_name, module_name: submitted!.module_name, topic: submitted!.topic });
      }
    }
    return route.fulfill({ json: l });
  });
  return { submitted: () => submitted, lesson: l, failNextLookup: () => { failLookup = true; } };
}

test('explicit reschedule previews date roster, requires confirmation, preserves input after conflict', async ({ page }) => {
  const state = await lessonFixture(page); await page.goto('/#/lessons/8');
  await page.getByRole('button', { name: 'Перенести с обновлением состава' }).click(); const dialog = page.getByRole('dialog');
  await dialog.getByLabel('Новое начало (МСК)').fill('2026-10-03T10:00'); await dialog.getByLabel('Новое окончание (МСК)').fill('2026-10-03T11:00');
  await expect(dialog.getByLabel('Новый участник', { exact: true })).toBeChecked(); await expect(dialog.getByLabel('Прежний участник', { exact: true })).toHaveCount(0);
  await dialog.getByLabel('Причина переноса').fill('Изменение учебного дня');
  const submit = dialog.getByRole('button', { name: 'Подтвердить перенос' }); await submit.click(); expect(state.submitted()).toBeNull();
  await dialog.getByLabel('Подтверждаю перенос и выбранный состав').check(); await submit.click();
  await expect(dialog.getByRole('alert')).toContainText('Проверьте конфликт'); await expect(dialog.getByLabel('Причина переноса')).toHaveValue('Изменение учебного дня');
  await mobileFits(page); await submit.click(); await expect(dialog).not.toBeVisible();
  expect(state.submitted()).toMatchObject({ starts_at: '2026-10-03T07:00:00.000Z', enrollment_ids: [6] });
  await expect(page.getByLabel('Посещаемость: Новый участник')).toHaveValue('unmarked');
});

test('reschedule confirmation resets when dates, participants or reason change', async ({ page }) => {
  const state = await lessonFixture(page);
  await page.goto('/#/lessons/8');
  await page.getByRole('button', { name: 'Перенести с обновлением состава' }).click();
  const dialog = page.getByRole('dialog');
  const confirm = dialog.getByLabel('Подтверждаю перенос и выбранный состав');
  await expect(dialog.getByLabel('Прежний участник', { exact: true })).toBeChecked();
  await dialog.getByLabel('Причина переноса').fill('Согласован перенос');
  await confirm.check();
  await dialog.getByLabel('Новое начало (МСК)').fill('2026-10-02T23:30');
  await expect(confirm).not.toBeChecked();
  await expect(dialog.getByLabel('Прежний участник', { exact: true })).toBeChecked();
  await dialog.getByLabel('Новое окончание (МСК)').fill('2026-10-02T23:45');
  await confirm.check();
  await dialog.getByLabel('Новое окончание (МСК)').fill('2026-10-02T23:55');
  await expect(confirm).not.toBeChecked();
  await confirm.check();
  await dialog.getByLabel('Прежний участник', { exact: true }).uncheck();
  await expect(confirm).not.toBeChecked();
  await confirm.check();
  await dialog.getByLabel('Причина переноса').fill('Уточнённая причина');
  await expect(confirm).not.toBeChecked();
  await dialog.getByRole('button', { name: 'Подтвердить перенос' }).click();
  expect(state.submitted()).toBeNull();
  await confirm.check();
  await dialog.getByLabel('Новое начало (МСК)').fill('2026-10-03T00:00');
  await dialog.getByLabel('Новое окончание (МСК)').fill('2026-10-03T01:00');
  await expect(confirm).not.toBeChecked();
  await expect(dialog.getByLabel('Новый участник', { exact: true })).toBeChecked();
  await expect(dialog.getByLabel('Прежний участник', { exact: true })).toHaveCount(0);
  await confirm.check();
  await dialog.getByRole('button', { name: 'Подтвердить перенос' }).click();
  expect(state.submitted()).toMatchObject({ starts_at: '2026-10-02T21:00:00.000Z', enrollment_ids: [6], reason: 'Уточнённая причина' });
  await mobileFits(page);
});

test('reschedule roster lookup failure blocks confirmation and supports retry', async ({ page }) => {
  const state = await lessonFixture(page); state.failNextLookup();
  await page.goto('/#/lessons/8');
  await page.getByRole('button', { name: 'Перенести с обновлением состава' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('alert')).toContainText('Состав временно недоступен');
  await expect(dialog.getByLabel('Подтверждаю перенос и выбранный состав')).toBeDisabled();
  await expect(dialog.getByRole('button', { name: 'Подтвердить перенос' })).toBeDisabled();
  expect(state.submitted()).toBeNull();
  await dialog.getByRole('button', { name: 'Повторить загрузку состава' }).click();
  await expect(dialog.getByLabel('Прежний участник', { exact: true })).toBeChecked();
  await expect(dialog.getByLabel('Подтверждаю перенос и выбранный состав')).toBeEnabled();
  await expect(dialog.getByLabel('Подтверждаю перенос и выбранный состав')).not.toBeChecked();
  await mobileFits(page);
});

test('snapshot correction retries and exposes previous version only through admin action', async ({ page }) => {
  await lessonFixture(page, true); await page.goto('/#/lessons/8');
  await expect(page.getByText('Программа в истории', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Исторический снимок', exact: true }).click(); let dialog = page.getByRole('dialog');
  await expect(dialog.getByText('Предыдущих версий пока нет')).toBeVisible();
  await dialog.getByLabel('Название программы в истории').fill('Уточнённая программа'); await dialog.getByLabel('Тема в истории').fill('Уточнённая тема'); await dialog.getByLabel('Причина исправления снимка').fill('Уточнение названий');
  await dialog.getByRole('button', { name: 'Сохранить исправление снимка' }).click(); await expect(dialog.getByRole('alert')).toContainText('Проверьте конфликт');
  await mobileFits(page); await dialog.getByRole('button', { name: 'Сохранить исправление снимка' }).click(); await expect(dialog).not.toBeVisible();
  await expect(page.getByRole('heading', { name: 'Уточнённая тема' })).toBeVisible(); await expect(page.getByText('Уточнённая программа', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Исторический снимок', exact: true }).click(); dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('listitem')).toContainText('Программа в истории · Модуль в истории · Сохранённая тема');
  await expect(dialog.getByRole('listitem')).toContainText('Причина исправления: Уточнение названий');
});

test('legacy snapshot warning follows a teacher from schedule to journal', async ({ page }) => {
  const state = await lessonFixture(page, true, 'teacher');
  state.lesson.snapshot_state = 'legacy_unknown';
  await page.goto('/#/lessons');
  await expect(page.getByRole('table', { name: 'Расписание', exact: true })).toContainText('Исторический снимок отсутствует; названия из текущих справочников.');
  await page.getByRole('link', { name: 'Сохранённая тема', exact: true }).click();
  await expect(page.getByText('Исторический снимок отсутствует. Названия показаны из текущих справочников.')).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Журнал посещаемости' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Исторический снимок', exact: true })).toHaveCount(0);
  await mobileFits(page);
});

test('snapshot correction preserves a module supplied by an M2 version', async ({ page }) => {
  const state = await lessonFixture(page, true, 'admin', true);
  await page.goto('/#/lessons/8');
  await page.getByRole('button', { name: 'Исторический снимок', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByLabel('Название модуля в истории')).toHaveValue('Модуль в истории');
  await dialog.getByLabel('Название модуля в истории').fill('Уточнённый модуль M2');
  await dialog.getByLabel('Причина исправления снимка').fill('Уточнение модуля');
  await dialog.getByRole('button', { name: 'Сохранить исправление снимка' }).click();
  await expect(dialog.getByRole('alert')).toContainText('Проверьте конфликт');
  await dialog.getByRole('button', { name: 'Сохранить исправление снимка' }).click();
  await expect(dialog).not.toBeVisible();
  expect(state.submitted()).toMatchObject({ module_name: 'Уточнённый модуль M2' });
});

test('reopened lesson keeps historical topic and module locked in ordinary editor', async ({ page }) => {
  const state = await lessonFixture(page, true);
  state.lesson.status = 'scheduled';
  await page.goto('/#/lessons/8');
  await page.getByRole('button', { name: 'Изменить занятие', exact: true }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByLabel('Тема', { exact: true })).toHaveAttribute('readonly', '');
  await expect(dialog.getByLabel('Модуль', { exact: true })).toBeDisabled();
  await mobileFits(page);
});
