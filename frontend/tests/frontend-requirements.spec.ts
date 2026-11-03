import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

const empty = { items: [], total: 0, page: 1, page_size: 20, unread: 0 };
const user = (role: string) => ({ id: 1, name: 'Пользователь', email: 'user@example.test', roles: [role], blocked: false, must_change_password: false });
const report = { id: 4, student_id: 7, title: 'Итоги обучения', body: 'Опубликованные результаты', revision: 1, version: 1, published: true, files: [] };
const event = { id: 11, title: 'Встреча центра', status: 'published', type: 'Встреча', description: 'Описание', organizer: 'AIQ', responsibles: [], starts_at: '2027-04-01T10:00:00Z', ends_at: '2027-04-01T12:00:00Z', time_zone: 'Europe/Moscow', location: 'Зал', capacity: 20, registration_open: true, registration_starts_at: null, registration_ends_at: null, conditions: '', required_documents: '', revision: 1, can_edit: false, allowed_actions: [] };
function deferred() { let resolve!: () => void; const promise = new Promise<void>(r => { resolve = r; }); return { promise, resolve }; }
async function accessible(page: Page) {
  await page.evaluate(() => document.fonts.ready);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
}

for (const screen of [
  { route: 'events', path: '/events', role: 'parent', retry: 'Обновить события', empty: 'Доступных событий нет.' },
  { route: 'notifications', path: '/notifications', role: 'parent', retry: 'Обновить уведомления', empty: 'Нет доступных уведомлений.' },
  { route: 'family-reports', path: '/family-reports', role: 'education_leader', retry: 'Обновить отчёты', empty: 'Нет доступных отчётов.' },
  { route: 'my-requests', path: '/family/requests', role: 'parent', retry: 'Обновить обращения', empty: 'Нет доступных обращений.' },
  { route: 'family', path: '/family/students', role: 'parent', retry: 'Обновить', empty: 'Нет доступных учеников.' },
  { route: 'users', path: '/users', role: 'admin', retry: 'Повторить загрузку', empty: 'Пользователей пока нет' },
]) {
  test(`${screen.route}: loading, error, keyboard retry and empty state`, async ({ page }) => {
    const gate = deferred(); let fail = true;
    await page.route('**/api/v1/**', async route => {
      const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
      if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'csrf' } });
      if (path === '/auth/me') return route.fulfill({ json: user(screen.role) });
      if (path === screen.path) {
        await gate.promise;
        return fail ? route.fulfill({ status: 503, json: { message: 'Временная ошибка загрузки.' } }) : route.fulfill({ json: empty });
      }
      return route.fulfill({ json: empty });
    });
    await page.goto(`/#/${screen.route}`);
    await expect(page.getByRole('status').filter({ hasText: /Загру/ }).first()).toBeVisible();
    gate.resolve();
    await expect(page.getByRole('alert')).toContainText('Временная ошибка загрузки.');
    fail = false;
    const retry = page.getByRole('button', { name: screen.retry, exact: true });
    await retry.focus(); await page.keyboard.press('Enter');
    await expect(page.getByText(screen.empty, { exact: false })).toBeVisible();
    await expect(page.getByRole('alert')).toHaveCount(0);
    await accessible(page);
  });
}

test('published report loads fresh data, retries, traps and restores keyboard focus', async ({ page }, info) => {
  const gate = deferred(); let fail = true;
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'csrf' } });
    if (path === '/auth/me') return route.fulfill({ json: user('parent') });
    if (path === '/family/students') return route.fulfill({ json: { ...empty, items: [{ id: 7, name: 'Анна' }], total: 1 } });
    if (path === '/family/students/7/reports') return route.fulfill({ json: { ...empty, items: [report], total: 1 } });
    if (path === '/family/students/7/notifications') return route.fulfill({ json: [] });
    if (path === '/family/reports/4') {
      await gate.promise;
      return fail ? route.fulfill({ status: 503, json: { message: 'Не удалось открыть отчёт.' } }) : route.fulfill({ json: { ...report, body: 'Актуальная опубликованная версия', files: [{ id: 9, name: 'ДлинноеИмяФайла'.repeat(12) + '.pdf', url: '/api/v1/family/reports/4/files/9' }] } });
    }
    return route.fulfill({ status: 404, json: { message: 'Нет данных.' } });
  });
  await page.goto('/#/family');
  await page.getByLabel('Раздел обучения').selectOption('reports');
  const open = page.getByRole('button', { name: 'Открыть отчёт: Итоги обучения' });
  await open.focus(); await page.keyboard.press('Enter');
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByRole('status')).toHaveText('Загрузка отчёта…');
  gate.resolve();
  await expect(dialog.getByRole('alert')).toContainText('Не удалось открыть отчёт.');
  fail = false;
  await dialog.getByRole('button', { name: 'Повторить загрузку отчёта' }).click();
  await expect(dialog.getByText('Актуальная опубликованная версия')).toBeVisible();
  expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
  for (let i = 0; i < 6; i++) { await page.keyboard.press('Tab'); expect(await page.evaluate(() => !!document.activeElement?.closest('dialog'))).toBe(true); }
  await accessible(page);
  await page.screenshot({ path: info.outputPath('published-report.png') });
  await page.keyboard.press('Escape'); await expect(open).toBeFocused();
});

test('report editor keeps text after failure, locks fields while saving and confirms success', async ({ page }, info) => {
  const gate = deferred(); let fail = true; let current = { ...report, published: false, state: 'draft', actions: ['edit'], snapshot: { student_name: 'Анна', from: '2026-10-01', to: '2026-10-31', collected_at: '2026-11-02T09:00:00Z', attendance: { present: 0, late: 0, absent: 0, excused: 0, total: 0, percent: null }, elements: [], works: [] } };
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'csrf' } });
    if (path === '/auth/me') return route.fulfill({ json: user('curator') });
    if (path === '/family-reports') return route.fulfill({ json: { ...empty, items: [current], total: 1 } });
    if (path === '/family-reports/4') {
      await gate.promise;
      if (fail) return route.fulfill({ status: 503, json: { message: 'Сохранение временно недоступно.' } });
      current = { ...current, ...route.request().postDataJSON(), revision: 2 };
      return route.fulfill({ json: current });
    }
    return route.fulfill({ json: empty });
  });
  await page.goto('/#/family-reports');
  await page.getByRole('button', { name: 'Изменить отчёт' }).click();
  await expect(page.getByRole('heading', { name: 'Изменение отчёта' })).toBeFocused();
  await page.getByLabel('Текст для семьи').fill('Текст, который нельзя потерять.');
  await page.getByRole('button', { name: 'Сохранить черновик', exact: true }).click();
  await expect(page.getByLabel('Текст для семьи')).toBeDisabled();
  gate.resolve();
  await expect(page.getByRole('alert')).toContainText('Сохранение временно недоступно.');
  await expect(page.getByLabel('Текст для семьи')).toHaveValue('Текст, который нельзя потерять.');
  fail = false;
  await page.getByRole('button', { name: 'Сохранить черновик', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: 'Черновик отчёта сохранён.' })).toBeVisible();
  await accessible(page);
  await page.screenshot({ path: info.outputPath('report-editor.png'), fullPage: true });
});

test('administrator participant list handles candidate failure and confirms status change', async ({ page }, info) => {
  let candidateFailure = true; let status = 'requested'; let rev = 1;
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'csrf' } });
    if (path === '/auth/me') return route.fulfill({ json: user('admin') });
    if (path === '/events/11') return route.fulfill({ json: event });
    if (path.endsWith('/candidates')) return candidateFailure ? route.fulfill({ status: 503, json: { message: 'Поиск участников недоступен.' } }) : route.fulfill({ json: empty });
    if (path.endsWith('/actions')) { status = 'confirmed'; rev++; return route.fulfill({ json: { id: 5, status } }); }
    if (path.endsWith('/registrations')) return route.fulfill({ json: { ...empty, can_manage: true, occupied: 1, self_registration_min_age: 0, total: 1, items: [{ id: 5, student_id: 7, name: 'Анна', status, revision: rev, created_at: '2026-11-02T09:00:00Z', allowed_actions: status === 'requested' ? ['confirmed'] : [] }] } });
    return route.fulfill({ json: empty });
  });
  await page.goto('/#/events/11');
  await expect(page.getByRole('heading', { name: 'Участники события' })).toBeVisible();
  await expect(page.getByRole('alert')).toContainText('Поиск участников недоступен.');
  candidateFailure = false;
  await page.getByRole('button', { name: 'Повторить поиск участников' }).click();
  await expect(page.getByText('Нет доступных учеников для записи.')).toBeVisible();
  await page.getByRole('button', { name: 'Подтвердить участие' }).focus(); await page.keyboard.press('Enter');
  await expect(page.getByRole('status').filter({ hasText: 'Участие: Подтверждено.' })).toBeVisible();
  await accessible(page);
  await page.screenshot({ path: info.outputPath('participants-admin.png'), fullPage: true });
});

for (const role of ['parent', 'student']) {
  test(`${role} lands at home after keyboard login`, async ({ page }) => {
    let signedIn = false;
    await page.route('**/api/v1/**', route => {
      const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
      if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'csrf' } });
      if (path === '/auth/login') { signedIn = true; return route.fulfill({ json: { user: user(role) } }); }
      if (path === '/auth/me') return signedIn ? route.fulfill({ json: user(role) }) : route.fulfill({ status: 401, json: {} });
      return route.fulfill({ json: empty });
    });
    await page.goto('/');
    await page.getByLabel('Email', { exact: true }).fill('user@example.test');
    await page.getByLabel('Пароль', { exact: true }).fill('Password-12345');
    await page.keyboard.press('Enter');
    await expect(page).toHaveURL(/#\/family$/);
    await expect(page.getByRole('heading', { name: role === 'parent' ? 'Кабинет родителя' : 'Кабинет ученика' })).toBeFocused();
  });
}

test('missing invitation shows recovery instructions without sending an empty token', async ({ page }) => {
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname;
    expect(path).not.toContain('/invitations/inspect');
    return path.endsWith('/auth/csrf') ? route.fulfill({ json: { csrf_token: 'csrf' } }) : route.fulfill({ status: 401, json: {} });
  });
  await page.goto('/#/invitation');
  await expect(page.getByRole('alert')).toContainText('В ссылке нет токена приглашения.');
  await expect(page.getByRole('button', { name: 'Принять приглашение' })).toHaveCount(0);
  await accessible(page);
});
