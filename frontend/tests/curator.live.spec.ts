import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

const base = process.env.PILOT_BASE_URL || 'http://127.0.0.1:5175';
const temp = 'CuratorOnly-Temporary-2026'; const secret = 'CuratorOnly-New-Password-2026';
async function login(page: Page, email: string, password: string) { await page.goto(`${base}/#/login`); await page.getByLabel('Email', { exact: true }).fill(email); await page.getByLabel('Пароль', { exact: true }).fill(password); await page.getByRole('button', { name: 'Войти', exact: true }).click(); await expect(page.locator('.app-header')).toBeVisible(); }
async function api(page: Page, path: string, method = 'GET', data?: unknown) { const csrf = await (await page.request.get(`${base}/api/v1/auth/csrf`)).json(); const response = await page.request.fetch(`${base}/api/v1${path}`, { method, data, headers: { Origin: base, 'X-CSRF-Token': csrf.csrf_token } }); expect(response.ok(), `${method} ${path}: ${response.status()}`).toBe(true); return response.status() === 204 ? undefined : response.json(); }
async function accessible(page: Page) { expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true); expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]); }

test('real curator: assigned family, progress, draft, delivery and parent response', async ({ page, browser }, info) => {
  test.skip(!process.env.PILOT_ADMIN_EMAIL || !process.env.PILOT_ADMIN_PASSWORD, 'Requires isolated acceptance servers');
  await login(page, process.env.PILOT_ADMIN_EMAIL!, process.env.PILOT_ADMIN_PASSWORD!);
  const tag = `${Date.now()}-${info.project.name}`; const errors: string[] = [];
  const curator = await api(page, '/users', 'POST', { name: `Куратор ${tag}`, email: `curator-${tag}@example.test`, password: temp, roles: ['curator'] });
  const child = await api(page, '/students', 'POST', { name: `Подопечный ${tag}` });
  const parent = await api(page, '/parents', 'POST', { name: `Родитель ${tag}`, email: `parent-${tag}@example.test` });
  await api(page, `/students/${child.id}/parents`, 'POST', { parent_id: parent.id, relationship: 'Мать', is_primary: true });
  const teacher = await api(page, '/teachers', 'POST', { name: `Педагог ${tag}` });
  const program = await api(page, '/programs', 'POST', { name: `Развитие ${tag}`, status: 'active' });
  const group = await api(page, '/groups', 'POST', { name: `Группа ${tag}`, program_id: program.id, teacher_id: teacher.id, capacity: 10, status: 'active' });
  await page.goto(`${base}/#/groups/${group.id}`);
  await page.getByRole('button', { name: 'Изменить группу', exact: true }).click();
  await page.getByRole('dialog').getByLabel('Куратор группы', { exact: true }).selectOption(String(curator.id));
  await page.getByRole('dialog').getByRole('button', { name: 'Сохранить', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect((await api(page, `/groups/${group.id}`)).curator_id).toBe(curator.id);
  await api(page, `/groups/${group.id}/enrollments`, 'POST', { student_id: child.id, starts_on: '2000-01-01' });
  const lesson = await api(page, '/lessons', 'POST', { group_id: group.id, teacher_id: teacher.id, topic: 'Совместное обсуждение', starts_at: new Date(Date.now() - 7200_000).toISOString(), ends_at: new Date(Date.now() - 3600_000).toISOString() });
  await api(page, `/lessons/${lesson.id}/attendance`);
  await api(page, `/lessons/${lesson.id}/attendance`, 'PUT', { items: [{ student_id: child.id, status: 'present', comment: 'Внутренняя заметка педагога' }] });
  await api(page, `/lessons/${lesson.id}/complete`, 'POST');
  await api(page, '/assignments', 'POST', { lesson_id: lesson.id, student_id: child.id, title: 'Аргументы', instructions: 'Привести два аргумента.' });
  const invitation = await api(page, '/invitations', 'POST', { role: 'parent', profile_id: parent.id, name: parent.name, email: `parent-${tag}@example.test` });
  const curatorContext = await browser.newContext({ viewport: page.viewportSize()! }); const parentContext = await browser.newContext({ viewport: page.viewportSize()! });
  const curatorPage = await curatorContext.newPage(); const parentPage = await parentContext.newPage();
  for (const p of [curatorPage, parentPage]) p.on('pageerror', error => errors.push(error.message));
  try {
    await login(curatorPage, `curator-${tag}@example.test`, temp);
    await curatorPage.getByLabel('Временный пароль', { exact: true }).fill(temp); await curatorPage.getByLabel('Новый пароль', { exact: true }).fill(secret); await curatorPage.getByLabel('Повторите новый пароль', { exact: true }).fill(secret); await curatorPage.getByRole('button', { name: 'Сохранить пароль', exact: true }).click();
    await expect(curatorPage.getByRole('button', { name: 'Войти', exact: true })).toBeVisible(); await login(curatorPage, `curator-${tag}@example.test`, secret);
    await expect(curatorPage.getByRole('heading', { name: 'Кабинет куратора', exact: true })).toBeVisible(); await expect(curatorPage.getByLabel('Мои подопечные', { exact: true })).toHaveText('1');
    await curatorPage.goto(`${base}/#/curator-groups`); await curatorPage.getByRole('link', { name: group.name, exact: true }).click(); await curatorPage.getByRole('link', { name: child.name, exact: true }).click();
    await expect(curatorPage.getByRole('heading', { name: child.name, exact: true })).toBeVisible(); await curatorPage.getByRole('tab',{name:'Прогресс',exact:true}).click(); await expect(curatorPage.getByLabel('Посещаемость за период')).toHaveText('100%'); await expect(curatorPage.getByLabel('Принятые работы за период')).toHaveText('0 / 1'); await accessible(curatorPage);
    await curatorPage.screenshot({ path: `../.cache/curator-student-${info.project.name}.png`, fullPage: true });
    await curatorPage.getByRole('tab',{name:'Отчёты и цели',exact:true}).click();
    await curatorPage.getByRole('button', { name: 'Подготовить отчёт родителям' }).click(); await curatorPage.getByLabel('Итог периода для родителей').fill('Ученик регулярно посещает занятия, текущая работа ещё выполняется.'); await curatorPage.getByLabel('Рекомендации семье').fill('Обсудить два аргумента дома.'); await curatorPage.getByLabel('Цели на следующий период').fill('Закончить задание и обсудить результат с педагогом.'); await curatorPage.getByLabel('Выбрать файлы для отчёта', {exact:true}).setInputFiles({name:'Рекомендации куратора.txt',mimeType:'text/plain',buffer:Buffer.from('Материалы для домашнего обсуждения.\n')}); await curatorPage.getByRole('button', { name: 'Сохранить черновик', exact: true }).click();
    await expect(curatorPage.getByText('Черновик — родителям не виден')).toBeVisible();
    await expect(curatorPage.getByText('Вложений: 1',{exact:true})).toBeVisible();
    await parentPage.goto(invitation.url); await parentPage.getByLabel('Новый пароль', { exact: true }).fill(secret); await parentPage.getByLabel('Повторите пароль', { exact: true }).fill(secret); await parentPage.getByRole('button', { name: 'Установить пароль' }).click(); await expect(parentPage.getByText('Пароль установлен.', { exact: false })).toBeVisible(); await login(parentPage, `parent-${tag}@example.test`, secret);
    await expect(parentPage.getByText('Отчётов куратора пока нет.', { exact: true })).toBeVisible();
    await curatorPage.getByRole('button', { name: 'Выбрать родителей и опубликовать' }).click(); await curatorPage.getByRole('checkbox', { name: parent.name, exact: true }).check(); await curatorPage.getByRole('button', { name: 'Опубликовать для выбранных родителей' }).click(); await expect(curatorPage.getByText('Отчёт опубликован и доступен выбранным родителям в кабинете.')).toBeVisible();
    await curatorPage.getByRole('link', { name: 'Открыть отчёт и обратную связь' }).click(); const shared = curatorPage.url();
    await parentPage.getByRole('button', { name: 'Выйти', exact: true }).click();
    await parentPage.goto(shared); await parentPage.getByLabel('Email', { exact: true }).fill(`parent-${tag}@example.test`); await parentPage.getByLabel('Пароль', { exact: true }).fill(secret); await parentPage.getByRole('button', { name: 'Войти', exact: true }).click();
    await expect(parentPage.getByRole('heading', { name: 'Отчёт куратора', exact: true })).toBeVisible(); await expect(parentPage.getByLabel('Посещаемость за период')).toHaveText('100%'); await expect(parentPage.getByText('Внутренняя заметка педагога', { exact: false })).toHaveCount(0);
    const attachment=parentPage.getByRole('link',{name:'Рекомендации куратора.txt',exact:true});
    await expect(attachment).toBeVisible();
    const attachmentResponse=await parentPage.request.get(`${base}${await attachment.getAttribute('href')}`);
    expect(attachmentResponse.status()).toBe(200);
    expect(await attachmentResponse.body()).toEqual(Buffer.from('Материалы для домашнего обсуждения.\n'));
    await expect(parentPage.getByRole('button',{name:/Удалить вложение/})).toHaveCount(0);
    await parentPage.getByLabel('Ответ куратору').fill('Спасибо, обсудим задание вместе дома.'); await parentPage.getByRole('button', { name: 'Отправить ответ куратору' }).click(); await expect(parentPage.getByText('Ваш ответ передан куратору.')).toBeVisible(); await accessible(parentPage);
    await parentPage.screenshot({ path: `../.cache/curator-parent-${info.project.name}.png`, fullPage: true });
    await curatorPage.goto(`${base}/#/curation`); await expect(curatorPage.getByText('Спасибо, обсудим задание вместе дома.', { exact: true })).toBeVisible(); await expect(curatorPage.getByLabel('Ответы родителей', { exact: true })).toHaveText('1');
    expect(errors).toEqual([]);
  } finally { await curatorContext.close(); await parentContext.close(); }
});
