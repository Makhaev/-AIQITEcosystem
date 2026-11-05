import { expect, test, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

const metric = { attendance: { present: 2, late: 0, absent: 1, excused: 1, unmarked: 0, total: 4, percent: 50 }, assignments: { total: 4, assigned: 1, submitted: 1, reviewed: 2, overdue: 1, percent: 50 } };
const progress = { period: { from: '2026-10-06', to: '2026-11-04' }, previous_period: { from: '2026-09-06', to: '2026-10-05' }, current: metric, previous: metric, monthly: [{ month: '2026-11', present: 2, absent: 1, excused: 1, unmarked: 0 }] };
async function fixture(page: Page) {
  let role = 'curator'; let fail = false; let saved = false; let published = false; const feedback: {id:number;parent_name:string;message:string;created_at:string}[] = [];
  const report = () => ({ id: 20, student_id: 10, student_name: 'Подопечный куратора', author_name: 'Куратор AIQ', period_from: '2026-10-06', period_to: '2026-11-04', summary: 'Учебный прогресс за месяц.', recommendations: 'Обсуждать задания дома.', goals: 'Завершить текущую работу.', snapshot: published ? progress : {}, published_at: published ? '2026-11-04T10:00:00Z' : null, recipients: published && role === 'curator' ? [{ id: 4, name: 'Родитель ученика' }] : [], feedback, can_feedback: role === 'parent' && published });
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url()); const path = url.pathname.replace('/api/v1', ''); const method = route.request().method(); const json = (body: unknown, status = 200) => route.fulfill({ json: body, status });
    if (path === '/features') return json({ student360: true });
    if (path === '/auth/csrf') return json({ csrf_token: 'fixture' });
    if (path === '/auth/me') return json({ id: 3, name: 'Куратор AIQ', roles: [role], email: 'curator@example.test', blocked: false, must_change_password: false });
    if (path === '/curator/workspace') return json({ attention: [{id:10,name:"Подопечный куратора",attention:{missing_family:false,absences:1,overdue:1}}], counts: { attention: 1, groups: 1, students: 1, parents: 1, missing_family: 0, draft_reports: saved && !published ? 1 : 0, parent_messages: feedback.length }, messages: feedback.map(item => ({...item,report_id:20,student_name:"Подопечный куратора"})) });
    if (path === '/curator/groups') return json({ items: [{ id: 8, name: 'Группа развития', program_name: 'Коммуникация', teacher_name: 'Педагог AIQ', status: 'active', student_count: 1 }], total: 1 });
    if (path === '/curator/students') return json({ group: url.searchParams.has('group_id') ? { id: 8, name: 'Группа развития' } : null, items: url.searchParams.get('search') ? [] : [{ id: 10, name: 'Подопечный куратора', status: 'active', birth_date: '2013-05-10', parents: [{ id: 4, name: 'Родитель ученика', relationship: 'Мать', is_primary: true }] }], total: url.searchParams.get('search') ? 0 : 1 });
    if (path === '/curator/students/999') return json({ code: 'not_found', message: 'Запись не найдена или недоступна.' }, 404);
    if (path === '/curator/students/10') {
      if (fail) return json({ code: 'internal_error', message: 'Не удалось загрузить прогресс.' }, 500);
      return json({ schedule: [{id:31,topic:'Ближайшее обсуждение',group_name:'Группа развития',teacher_name:'Педагог AIQ',starts_at:'2026-11-05T09:00:00Z',ends_at:'2026-11-05T10:00:00Z',location:'Кабинет 3'}], attendance: [{id:32,lesson_topic:'Прошедшее обсуждение',group_name:'Группа развития',starts_at:'2026-11-03T09:00:00Z',status:'absent',comment:'Связаться с семьёй'}], student: { id: 10, name: 'Подопечный куратора', status: 'active', birth_date: '2013-05-10' }, parents: [{ id: 4, name: 'Родитель ученика', relationship: 'Мать', is_primary: true, status: 'active', phone: '+79001112233', email: 'family@example.test', has_account: true }], groups: [{ id: 8, name: 'Группа развития', program_name: 'Коммуникация', teacher_name: 'Педагог AIQ', starts_on: '2026-09-01', ends_on: null }], progress, assignments: [{ id: 11, title: 'Аргументы', lesson_topic: 'Обсуждение', teacher_name: 'Педагог AIQ', status: 'reviewed', answer: 'Два аргумента.', feedback: 'Работа принята.' }], teacher_reports: [{ id: 12, lesson_topic: 'Обсуждение', teacher_name: 'Педагог AIQ', summary: 'Ученик проявил инициативу.', published_at: null }] });
    }
    if (path === '/curator-reports' && method === 'POST') { saved = true; return json({ id: 20 }, 201); }
    if (path === '/curator-reports') return json({ items: saved && (published || role === 'curator') ? [report()] : [], total: saved ? 1 : 0 });
    if (path === '/curator-reports/20/publish') { expect(route.request().postDataJSON().parent_ids).toEqual([4]); published = true; return json({ id: 20, path: '/#/family-report/20', recipient_count: 1 }); }
    if (path === '/curator-reports/20/feedback') { feedback.push({ id: 30, parent_name: 'Родитель ученика', message: route.request().postDataJSON().message, created_at: '2026-11-04T11:00:00Z' }); return json({ id: 30 }, 201); }
    if (path === '/curator-reports/20' && (published || role === 'curator')) return json(report());
    return json({ code: 'not_found', message: 'Запись не найдена или недоступна.' }, 404);
  });
  return { role: (value: string) => { role = value; }, fail: (value: boolean) => { fail = value; }, seed: () => { saved = true; published = true; } };
}
async function accessible(page: Page) { expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true); expect((await new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa']).analyze()).violations).toEqual([]); }
test('curator connects student, family, progress, report publication and parent feedback', async ({ page }) => {
  const api = await fixture(page); await page.goto('/#/curation');
  await expect(page.getByRole('heading', { name: 'Кабинет куратора', exact: true })).toBeVisible(); await expect(page.getByLabel('Мои подопечные', { exact: true })).toHaveText('1'); await accessible(page);
  await page.goto('/#/curator-groups'); await accessible(page); await page.getByRole('link', { name: 'Группа развития', exact: true }).click(); await expect(page.getByRole('table', { name: 'Подопечные куратора' })).toContainText('Родитель ученика');
  await page.getByRole('link', { name: 'Подопечный куратора', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Подопечный куратора', exact: true })).toBeVisible(); await expect(page.getByLabel('Посещаемость за период')).toHaveText('50%'); await expect(page.getByLabel('Принятые работы за период')).toHaveText('2 / 4'); await expect(page.getByText('Комментарий педагога: Работа принята.')).toBeVisible(); await accessible(page);
  await page.getByRole('button', { name: 'Подготовить отчёт родителям' }).click();
  await page.getByLabel('Итог периода для родителей').fill('Учебный прогресс за месяц.'); await page.getByLabel('Рекомендации семье').fill('Обсуждать задания дома.'); await page.getByRole('button', { name: 'Сохранить черновик', exact: true }).click();
  await expect(page.getByText('Черновик — родителям не виден')).toBeVisible(); await page.getByRole('button', { name: 'Выбрать родителей и опубликовать' }).click();
  await expect(page.getByRole('button', { name: 'Опубликовать для выбранных родителей' })).toBeDisabled(); await page.getByRole('checkbox', { name: 'Родитель ученика' }).check(); await accessible(page); await page.getByRole('button', { name: 'Опубликовать для выбранных родителей' }).click();
  await expect(page.getByText('Отчёт опубликован и доступен выбранным родителям в кабинете.')).toBeVisible();
  await page.getByRole('button', {name:'Расписание',exact:true}).click(); await expect(page.getByText('Ближайшее обсуждение', {exact:true})).toBeVisible(); await page.getByRole('button', {name:'Посещаемость',exact:true}).click(); await expect(page.getByText('Связаться с семьёй', {exact:true})).toBeVisible();
  api.role('parent'); await page.goto('/#/family-report/20'); await page.reload(); await expect(page.getByRole('heading', { name: 'Отчёт куратора', exact: true })).toBeVisible(); await expect(page.getByText('Данные зафиксированы при публикации.', { exact: false })).toBeVisible();
  await page.getByLabel('Ответ куратору').fill('Будем выполнять задания вместе.'); await page.getByRole('button', { name: 'Отправить ответ куратору' }).click(); await expect(page.getByText('Будем выполнять задания вместе.', { exact: true })).toBeVisible(); await expect(page.getByRole('link', { name: 'К карточке ученика' })).toHaveCount(0); await accessible(page);
  api.role('curator'); await page.reload(); await page.getByRole('button',{name:'Уведомления',exact:true}).click(); await expect(page.getByRole('dialog')).toContainText('Будем выполнять задания вместе.'); await accessible(page); await page.getByRole('dialog').getByRole('link',{name:'Открыть отчёт',exact:true}).click(); await expect(page.getByRole('dialog')).toHaveCount(0);
});
test('curator foreign student, loading failure and other roles are handled', async ({ page }) => {
  const api = await fixture(page); await page.goto('/#/curator-students/999'); await expect(page.getByRole('alert')).toContainText('Запись не найдена'); await expect(page.getByRole('button', { name: 'Подготовить отчёт родителям' })).toHaveCount(0);
  api.fail(true); await page.goto('/#/curator-students/10'); await expect(page.getByRole('alert')).toContainText('Не удалось загрузить прогресс'); api.fail(false); await page.getByRole('button', { name: 'Повторить загрузку', exact: true }).click(); await expect(page.getByRole('heading', { name: 'Подопечный куратора', exact: true })).toBeVisible();
  api.role('teacher'); await page.reload(); await expect(page.getByRole('heading', { name: 'Раздел недоступен' })).toBeVisible(); await accessible(page);
});

test('curator navigation, attention, profile and mobile drawer use real sections only', async ({page},info) => {
  await fixture(page); await page.goto('/#/curation');
  await expect(page.getByText('Пропуски за 30 дней: 1', {exact:true})).toBeVisible();
  await expect(page.getByText('Просроченные задания: 1', {exact:true})).toBeVisible();
  await page.getByRole('link',{name:'Все ученики, требующие внимания (1)',exact:true}).click();
  await expect(page.getByRole('checkbox',{name:'Требуют внимания',exact:true})).toBeChecked();
  const mobile=info.project.name.includes('mobile');
  if(mobile) await page.getByRole('button',{name:'Меню',exact:true}).click();
  const menu=mobile?page.getByRole('dialog'):page.locator('.sidebar');
  const nav=menu.getByRole('navigation',{name:'Основная навигация'});
  await expect(nav.getByRole('link')).toHaveText(['Обзор','Мои ученики','Мои группы','Расписание','Отчёты родителям']);
  await expect(nav.getByRole('link',{name:'Обращения',exact:true})).toHaveCount(0);
  await expect(nav.getByRole('link',{name:'Мои задачи',exact:true})).toHaveCount(0);
  if(mobile){await expect.poll(async () => (await menu.boundingBox())?.x).toBe(0); const box=await menu.boundingBox();expect(box?.height).toBe(page.viewportSize()!.height);}
  await accessible(page); await menu.getByRole('link',{name:'Куратор AIQ Мой профиль'}).click();
  await expect(page.getByRole('heading',{name:'Мой профиль',exact:true})).toBeVisible();
  await expect(page.getByText('curator@example.test',{exact:true})).toBeVisible();
  await page.getByRole('link',{name:'Настройки аккаунта',exact:true}).last().click();
  await expect(page.getByRole('heading',{name:'Мой аккаунт',exact:true})).toBeVisible();
  await page.getByRole('button',{name:'Уведомления',exact:true}).click();
  await expect(page.getByRole('dialog')).toContainText('Ответов родителей пока нет.');
  await page.getByRole('button',{name:'Закрыть окно',exact:true}).click();
  await page.goto('/#/curation'); await page.screenshot({path:`../.cache/curator-overview-${info.project.name}.png`,fullPage:true});
});
