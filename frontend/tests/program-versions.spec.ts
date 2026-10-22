import { expect, test, type Page } from '@playwright/test';

async function fixture(page: Page) {
  const v = { id: 2, program_id: 1, number: 2, status: 'draft', revision: 1, created_by: 7, submitted_by: null, approved_by: null, legacy_import: false, document: { name: 'Исходная версия', description: '', sections: [] }, events: [] };
  const saves: { revision: number; document: typeof v.document }[] = [];
  let conflict = true;
  await page.route('**/api/v1/**', route => {
    const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
    if (path === '/auth/csrf') return route.fulfill({ json: { csrf_token: 'fixture' } });
    if (path === '/auth/me') return route.fulfill({ json: { id: 7, name: 'Методист', email: 'methodologist@example.test', roles: ['methodologist'], must_change_password: false, blocked: false } });
    if (path === '/programs/1/versions/2') {
      if (route.request().method() === 'PUT') {
        const input = route.request().postDataJSON();
        saves.push(input);
        if (conflict) {
          conflict = false; v.revision = 2; v.document.name = 'Правки коллеги';
          return route.fulfill({ status: 409, json: { code: 'stale_revision', message: 'Версия уже изменена.', field_errors: {} } });
        }
        v.document = input.document; v.revision++;
      }
      return route.fulfill({ json: v });
    }
    if (path === '/programs') return route.fulfill({ json: { items: [], total: 0, page: 1, page_size: 20 } });
    return route.fulfill({ status: 404, json: { code: 'not_found', message: 'Не найдено' } });
  });
  await page.goto('/#/programs/1/versions/2');
  await expect(page.getByLabel('Название версии')).toHaveValue('Исходная версия');
  return { saves };
}

test('version conflict preserves edits until explicit reload and then uses the current revision', async ({ page }) => {
  const state = await fixture(page);
  await page.getByLabel('Название версии').fill('Мои правки');
  await page.getByRole('button', { name: 'Сохранить черновик', exact: true }).click();
  await expect(page.getByLabel('Название версии')).toHaveValue('Мои правки');
  const reload = page.getByRole('button', { name: 'Загрузить актуальную версию' });
  await expect(reload).toBeVisible();
  page.once('dialog', d => d.dismiss());
  await reload.click();
  await expect(page.getByLabel('Название версии')).toHaveValue('Мои правки');
  page.once('dialog', d => d.accept());
  await reload.click();
  await expect(page.getByLabel('Название версии')).toHaveValue('Правки коллеги');
  await page.getByLabel('Название версии').fill('Согласованное название');
  await page.getByRole('button', { name: 'Сохранить черновик', exact: true }).click();
  await expect(page.getByText('Черновик сохранён.', { exact: true })).toBeVisible();
  expect(state.saves.map(s => s.revision)).toEqual([1, 2]);
  expect(state.saves[1].document.name).toBe('Согласованное название');
});

test('navigation asks before discarding a draft including the main menu', async ({ page }) => {
  await fixture(page);
  await page.getByLabel('Название версии').fill('Несохранённая версия');
  if (await page.getByRole('button', { name: 'Меню', exact: true }).isVisible()) await page.getByRole('button', { name: 'Меню', exact: true }).click();
  const link = page.getByRole('link', { name: 'Программы', exact: true }).filter({ visible: true });
  page.once('dialog', d => d.dismiss());
  await link.click();
  await expect(page).toHaveURL(/#\/programs\/1\/versions\/2$/);
  page.once('dialog', d => d.accept());
  await link.click();
  await expect(page).toHaveURL(/#\/programs$/);
});
