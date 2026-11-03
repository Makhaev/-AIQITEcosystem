import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { eventInstant, eventLocal } from '../src/screens/eventTime';

test.use({ timezoneId: 'America/Los_Angeles' });
const initial = { id: 11, title: 'Мастер-класс робототехники', type: 'Мастер-класс', description: 'Собираем первого робота', organizer: 'AIQ', responsible_ids: [8], responsibles: [{ id: 8, name: 'Анна Педагог', active: true }], starts_at: '2027-04-01T05:00:00Z', ends_at: '2027-04-01T07:00:00Z', time_zone: 'Asia/Yekaterinburg', location: 'Зал 2', capacity: 20, registration_starts_at: '2027-03-01T04:00:00Z', registration_ends_at: '2027-03-31T13:00:00Z', conditions: 'Для детей от 10 лет', required_documents: 'Согласие представителя', revision: 1, status: 'draft', registration_open: false, can_edit: true, allowed_actions: ['publish'], published_at: null as string | null, registration_closed_at: null as string | null, completed_at: null as string | null };
async function mock(page: Page, role = 'admin') {
  let card = { ...initial }, conflict = false, denied = false;
  const writes: { path: string; body: Record<string, unknown> }[] = [];
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'csrf' } });
    if (path === '/auth/me') return route.fulfill({ json: { id: 1, name: 'Пользователь', email: 'test@example.test', roles: [role], must_change_password: false, blocked: false } });
    if (path === '/events/staff') {
      expect(role).toBe('admin');
      return route.fulfill({ json: { items: initial.responsibles, total: 1, page: 1, page_size: 20 } });
    }
    if (path.startsWith('/events')) {
      if (denied) return route.fulfill({ status: 403, json: { message: 'Доступ изменён.' } });
      if (path.endsWith('/registrations')) return route.fulfill({ json: { items: [], total: 0, page: 1, page_size: 20, occupied: 0, can_manage: role === 'admin', self_registration_min_age: 0 } });
      if (route.request().method() !== 'GET') {
        expect(route.request().headers()['x-csrf-token']).toBe('csrf');
        const body = route.request().postDataJSON(); writes.push({ path, body });
        if (conflict) return route.fulfill({ status: 409, json: { message: 'Событие изменилось. Обновите карточку перед повтором.' } });
        if (path.endsWith('/actions')) {
          expect(body.revision).toBe(card.revision);
          card = body.action === 'publish' ? { ...card, status: 'published', published_at: '2026-10-29T13:00Z', revision: card.revision + 1, allowed_actions: ['close_registration'] } : { ...card, status: 'registration_closed', registration_closed_at: '2026-10-29T13:01Z', revision: card.revision + 1, allowed_actions: [] };
        } else card = { ...card, ...body, revision: card.revision + 1 };
        return route.fulfill({ status: path === '/events' ? 201 : 200, json: card });
      }
      if (path === '/events') return route.fulfill({ json: { items: role === 'admin' ? [card] : [{ ...card, status: 'published', can_edit: false, allowed_actions: [] }], total: 1, page: 1, page_size: 20 } });
      return route.fulfill({ json: role === 'admin' ? card : { ...card, status: 'published', can_edit: false, allowed_actions: [] } });
    }
    return route.fulfill({ json: { items: [], total: 0 } });
  });
  return { writes, conflict: () => { conflict = true; }, deny: () => { denied = true; } };
}

test('administrator creates a complete card in its time zone and changes statuses', async ({ page }, info) => {
  const api = await mock(page);
  await page.goto('/#/events');
  await page.getByRole('link', { name: 'Создать событие', exact: true }).click();
  await page.getByLabel('Название', { exact: true }).fill(initial.title);
  await page.getByLabel('Тип события', { exact: true }).fill(initial.type);
  await page.getByLabel('Описание', { exact: true }).fill(initial.description);
  await page.getByLabel('Организатор', { exact: true }).fill(initial.organizer);
  await page.getByLabel('Анна Педагог', { exact: true }).check();
  await page.getByLabel('Часовой пояс', { exact: true }).fill('Asia/Yekaterinburg');
  await page.getByLabel('Начало события', { exact: true }).fill('2027-04-01T10:00');
  await page.getByLabel('Окончание события', { exact: true }).fill('2027-04-01T12:00');
  await page.getByLabel('Начало регистрации', { exact: true }).fill('2027-03-01T09:00');
  await page.getByLabel('Окончание регистрации', { exact: true }).fill('2027-03-31T18:00');
  await page.getByLabel('Место', { exact: true }).fill(initial.location);
  await page.getByLabel('Вместимость', { exact: true }).fill('20');
  await page.getByLabel('Условия участия', { exact: true }).fill(initial.conditions);
  await page.getByLabel('Необходимые документы', { exact: true }).fill(initial.required_documents);
  await page.getByRole('button', { name: 'Сохранить черновик', exact: true }).click();
  await expect(page).toHaveURL(/#\/events\/11$/);
  await expect(page.getByRole('status').filter({ hasText: 'Черновик события создан.' })).toBeVisible();
  expect(api.writes[0].body).toMatchObject({ starts_at: '2027-04-01T05:00:00.000Z', ends_at: '2027-04-01T07:00:00.000Z', time_zone: 'Asia/Yekaterinburg', responsible_ids: [8], capacity: 20 });
  await expect(page.getByText(/Начало:.*10:00:00/)).toBeVisible();
  await page.getByRole('button', { name: 'Опубликовать', exact: true }).click();
  await expect(page.getByText('Опубликовано', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Закрыть регистрацию', exact: true }).click();
  await expect(page.getByText('Регистрация закрыта', { exact: true })).toBeVisible();
  expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.getByRole('heading', { name: initial.title, exact: true }).click();
  await page.screenshot({ path: info.outputPath('event-card.png'), fullPage: true });
});

test('conflicting edits preserve input and denied refresh clears the card', async ({ page }) => {
  const api = await mock(page); await page.goto('/#/events/11');
  await page.getByRole('button', { name: 'Редактировать событие' }).click();
  await expect(page.getByLabel('Начало события', { exact: true })).toHaveValue('2027-04-01T10:00');
  await page.getByLabel('Описание', { exact: true }).fill('Несохранённое описание');
  api.conflict();
  await page.getByRole('button', { name: 'Сохранить черновик' }).click();
  await expect(page.getByRole('alert')).toContainText('Событие изменилось');
  await expect(page.getByLabel('Описание', { exact: true })).toHaveValue('Несохранённое описание');
  await expect(page.getByRole('button', { name: 'Сохранить черновик' })).toBeDisabled();
  api.deny(); await page.getByRole('button', { name: 'Загрузить актуальную карточку' }).click();
  await expect(page.getByRole('alert')).toContainText('Доступ изменён');
  await expect(page.getByText(initial.title, { exact: true })).toHaveCount(0);
  await expect(page.getByLabel('Описание', { exact: true })).toHaveCount(0);
});

test('family reads published information with no management controls', async ({ page }) => {
  await mock(page, 'parent');await page.goto('/#/events');
  await expect(page.getByRole('link', { name: 'Создать событие' })).toHaveCount(0);
  await expect(page.getByLabel('Статус события').locator('option[value="draft"]')).toHaveCount(0);
  await page.getByRole('link', { name: initial.title, exact: true }).click();
  await expect(page.getByRole('heading', { name: initial.title, exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Редактировать событие' })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Опубликовать' })).toHaveCount(0);
  await expect(page.getByText(/Необходимые документы: Согласие/)).toBeVisible();
  await page.goto('/#/events/new');await expect(page.getByRole('heading', { name: 'Раздел недоступен' })).toBeVisible();
});

test('time zone conversion rejects DST gaps and repeated hours', async () => {
  expect(eventInstant('2027-04-01T10:00', 'Asia/Yekaterinburg')).toBe('2027-04-01T05:00:00.000Z');
  expect(eventInstant('2027-04-01T10:00', 'Asia/Kathmandu')).toBe('2027-04-01T04:15:00.000Z');
  expect(eventLocal('2027-04-01T05:00:00Z', 'Asia/Yekaterinburg')).toBe('2027-04-01T10:00:00');
  expect(() => eventInstant('2027-03-28T02:30', 'Europe/Berlin')).toThrow('Такого местного времени нет');
  expect(() => eventInstant('2027-10-31T02:30', 'Europe/Berlin')).toThrow('Это время повторяется');
});
