import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

for (const role of ['admin', 'methodist', 'leader']) {
  test(`module files upload, download and deletion: ${role}`, async ({ page }) => {
    const files: { id: number; name: string; size: number; created_at: string }[] = [];
    const program = { id: 5, name: 'Обучение', status: 'active', description: 'Учебная программа', modules: [{ id: 6, name: 'Диалог', description: 'Практика общения', position: 1 }] };
    await page.route('**/api/v1/**', async route => {
      const path = new URL(route.request().url()).pathname.replace('/api/v1', '');
      const method = route.request().method();
      const json = (value: unknown, status = 200) => route.fulfill({ json: value, status });
      if (path === '/auth/me') return json({ id: 1, name: 'Пользователь', email: 'test@example.test', roles: [role], blocked: false, must_change_password: false });
      if (path === '/auth/csrf') return json({ csrf_token: 'files-fixture' });
      if (path === '/features') return json({ student360: true });
      if (path === '/programs/5') return json(program);
      if (path === '/programs/5/modules/6/files') {
        if (method === 'POST') {
          expect(route.request().headers()['content-type']).toContain('multipart/form-data; boundary=');
          expect(route.request().headers()['x-csrf-token']).toBe('files-fixture');
          files.push({ id: 8, name: 'Материалы для длинного названия модуля.txt', size: 25, created_at: '2026-11-05T09:00:00Z' });
          return json(files[0], 201);
        }
        return json({ items: files });
      }
      if (path === '/programs/5/modules/6/files/8' && method === 'DELETE') { files.splice(0); return route.fulfill({ status: 204 }); }
      return json({ message: 'Нет данных.' }, 404);
    });
    await page.goto('/#/programs/5');
    if (role === 'methodist') {
      if (page.viewportSize()!.width < 1024) await page.getByRole('button', {name:'Содержание программы',exact:true}).click();
      await page.getByRole('button', {name:'1. Диалог',exact:true}).click();
    } else await page.getByRole('button', {name:'Файлы модуля Диалог',exact:true}).click();
    await expect(page.getByText('Файлов пока нет.', {exact:true})).toBeVisible();
    if (role === 'leader') { await expect(page.getByLabel('Файл для модуля', {exact:true})).toHaveCount(0); return; }
    await page.getByLabel('Файл для модуля', {exact:true}).setInputFiles({ name:'Материалы.txt',mimeType:'text/plain',buffer:Buffer.from('Практика общения') });
    await page.getByRole('button', {name:'Загрузить файл',exact:true}).click();
    const link = page.getByRole('link', {name:'Материалы для длинного названия модуля.txt',exact:true});
    await expect(link).toHaveAttribute('href','/api/v1/programs/5/modules/6/files/8');
    await expect(page.getByText('Файл добавлен в модуль.', {exact:true})).toBeVisible();
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    expect((await new AxeBuilder({page}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze()).violations).toEqual([]);
    await page.getByRole('button', {name:'Удалить файл Материалы для длинного названия модуля.txt',exact:true}).click();
    await page.getByRole('group', {name:'Удалить файл из модуля?',exact:true}).getByRole('button', {name:'Удалить файл',exact:true}).click();
    await expect(link).toHaveCount(0); await expect(page.getByText('Файл удалён из модуля.', {exact:true})).toBeVisible();
  });
}
