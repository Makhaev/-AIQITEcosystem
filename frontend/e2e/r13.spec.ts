import { test, expect, type Page } from '@playwright/test';
import { readFileSync } from 'node:fs';

test('R13 clean database: real browser workflow and 360px attendance',async({browser,baseURL})=>{
 if(process.env.R13_TEST_MODE!=='synthetic'||!process.env.R13_FIXTURE)throw Error('Explicit synthetic fixture required');
 const fixture=JSON.parse(readFileSync(process.env.R13_FIXTURE,'utf8'));
 expect(fixture.kind).toBe('e2e');expect(fixture.database).toMatch(/^aiq_r13_e2e_[a-f0-9]+$/);
 const password=fixture.password, changed=password+'-new';
 const contexts=[];const unexpected:string[]=[];
 async function pageFor(width=1440){const context=await browser.newContext({baseURL,viewport:{width,height:900}});contexts.push(context);const page=await context.newPage();page.on('pageerror',e=>unexpected.push(e.name));return page;}
 async function login(page:Page,email:string,pass=password,forced=false){
  await page.goto('/');await page.getByLabel('Email',{exact:true}).fill(email);await page.getByLabel('Пароль',{exact:true}).fill(pass);await page.getByRole('button',{name:'Войти',exact:true}).click();
  if(forced){await page.getByLabel('Временный пароль',{exact:true}).fill(pass);await page.getByLabel('Новый пароль',{exact:true}).fill(changed);await page.getByLabel('Повторите новый пароль',{exact:true}).fill(changed);const result=page.waitForResponse(r=>r.url().endsWith('/auth/change-password'));await page.getByRole('button',{name:'Сохранить пароль',exact:true}).click();expect((await result).status()).toBe(204);await expect(page.getByLabel('Email',{exact:true})).toBeVisible();await login(page,email,changed);}
  await expect(page.getByRole('button',{name:'Выйти',exact:true})).toBeVisible();
 }
 async function go(page:Page,path:string){await page.goto('/#/'+path);}
 async function save(page:Page,path:string,button:string,method='POST'){
  const response=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/v1'+path&&r.request().method()===method);
  await page.getByRole('dialog').getByRole('button',{name:button,exact:true}).click();const res=await response;expect(res.status(),path).toBe(method==='POST'?201:200);return res.json();
 }
 async function api(page:Page,path:string){return page.evaluate(async path=>{const r=await fetch('/api/v1'+path);return {status:r.status,data:await r.json()};},path);}
 async function writeAPI(page:Page,path:string,method:string,body:unknown){
  const result=await page.evaluate(async({path,method,body})=>{
   const csrf=await(await fetch('/api/v1/auth/csrf')).json();
   const response=await fetch('/api/v1'+path,{method,headers:{'Content-Type':'application/json','X-CSRF-Token':csrf.csrf_token},body:JSON.stringify(body)});
   return {status:response.status,data:await response.json()};
  },{path,method,body});
  expect(result.status,path).toBe(method==='POST'&& !path.endsWith('/actions')?201:200);return result.data;
 }
 // No page.route, route.fulfill, request interception or fixture API responses.
 const admin=await pageFor();await login(admin,'admin@example.test');
 const users:Record<string,any>={};
 for(const [role,label]of [['teacher','Педагог'],['curator','Куратор'],['leader','Руководитель'],['methodologist','Методист'],['education_leader','Руководитель образования']]){
  await go(admin,'users');await admin.getByRole('button',{name:'Добавить пользователя',exact:true}).click();const d=admin.getByRole('dialog');
  await d.getByLabel('Имя и фамилия',{exact:true}).fill('R13 '+role);await d.getByLabel('Email',{exact:true}).fill(role+'@example.test');
  for(const checkbox of await d.locator('input[name=roles]').all())await checkbox.uncheck();await d.getByLabel(label,{exact:true}).check();await d.getByLabel('Временный пароль',{exact:true}).fill(password);
  users[role]=await save(admin,'/users','Создать пользователя');
 }
 await go(admin,'teachers');await admin.getByRole('button',{name:'Добавить педагога',exact:true}).click();
 let d=admin.getByRole('dialog');await d.getByLabel('Имя и фамилия',{exact:true}).fill('R13 teacher profile');await d.getByLabel('Учётная запись',{exact:true}).selectOption(String(users.teacher.id));const teacher=await save(admin,'/teachers','Сохранить педагога');
 await go(admin,'students');await admin.getByRole('button',{name:'Добавить ученика',exact:true}).click();d=admin.getByRole('dialog');await d.getByLabel('Имя и фамилия',{exact:true}).fill('R13 pupil');await d.getByLabel('Куратор',{exact:true}).selectOption(String(users.curator.id));const student=await save(admin,'/students','Сохранить ученика');
 await admin.getByRole('button',{name:'Добавить ученика',exact:true}).click();d=admin.getByRole('dialog');await d.getByLabel('Имя и фамилия',{exact:true}).fill('R13 foreign pupil');const foreign=await save(admin,'/students','Сохранить ученика');
 await go(admin,'parents');await admin.getByRole('button',{name:'Добавить родителя',exact:true}).click();d=admin.getByRole('dialog');await d.getByLabel('Имя и фамилия',{exact:true}).fill('R13 parent');await d.getByLabel('Телефон',{exact:true}).fill('+79991234567');const parent=await save(admin,'/parents','Сохранить родителя');
 await go(admin,`students/${student.id}`);await admin.getByRole('button',{name:'Связать с родителем',exact:true}).click();d=admin.getByRole('dialog');await d.getByLabel('Родитель',{exact:true}).selectOption(String(parent.id));await d.locator('input[name=relationship]').fill('Родитель');await save(admin,`/students/${student.id}/parents`,'Сохранить связь');
 await go(admin,'programs');await admin.getByRole('button',{name:'Добавить программу',exact:true}).click();d=admin.getByRole('dialog');await d.getByLabel('Название',{exact:true}).fill('R13 program');await d.getByLabel('Статус',{exact:true}).selectOption('active');const program=await save(admin,'/programs','Сохранить программу');
 await go(admin,`programs/${program.id}`);await admin.getByRole('button',{name:'Добавить модуль',exact:true}).click();d=admin.getByRole('dialog');await d.getByLabel('Название',{exact:true}).fill('R13 module');const module=await save(admin,`/programs/${program.id}/modules`,'Сохранить модуль');
 const methodologist=await pageFor();await login(methodologist,'methodologist@example.test',password,true);
 const reviewer=await pageFor();await login(reviewer,'education_leader@example.test',password,true);
 const versionsPath=`/programs/${program.id}/versions`;
 let version=await writeAPI(methodologist,versionsPath,'POST',{});
 version=await writeAPI(methodologist,`${versionsPath}/${version.id}`,'PUT',{revision:version.revision,document:{
  name:'R13 program',description:'Core regression',sections:[{key:'section',name:'Раздел',modules:[{key:'module',name:'Модуль',description:'',lessons:[
   {key:'template',topic:'Занятие',duration_minutes:45,goals:'Цели',outline:'Конспект',exercises:'Упражнения',equipment:'Оборудование',materials:'Материалы',criteria:'Критерии'},
  ]}]}],
 }});
 version=await writeAPI(methodologist,`${versionsPath}/${version.id}/actions`,'POST',{revision:version.revision,action:'submit'});
 version=await writeAPI(reviewer,`${versionsPath}/${version.id}/actions`,'POST',{revision:version.revision,action:'publish'});
 await go(admin,'groups');await admin.getByRole('button',{name:'Добавить: группа',exact:true}).click();d=admin.getByRole('dialog');await d.getByLabel('Название',{exact:true}).fill('R13 group');await d.getByLabel('Программа',{exact:true}).selectOption(String(program.id));await d.getByLabel('Опубликованная версия',{exact:true}).selectOption(String(version.id));await d.getByLabel('Основной педагог',{exact:true}).selectOption(String(teacher.id));await d.getByLabel('Вместимость',{exact:true}).fill('10');await d.getByLabel('Статус',{exact:true}).selectOption('active');const group=await save(admin,'/groups','Сохранить');
 await go(admin,`groups/${group.id}`);await admin.getByRole('button',{name:'Зачислить ученика',exact:true}).click();d=admin.getByRole('dialog');await d.getByLabel('Ученик',{exact:true}).selectOption(String(student.id));await d.locator('input[name=starts_on]').fill('2020-01-01');await save(admin,`/groups/${group.id}/enrollments`,'Зачислить');
 const lessons=[];
 for(let i=0;i<2;i++){
  await go(admin,'lessons');await admin.getByRole('button',{name:'Добавить занятие',exact:true}).click();d=admin.getByRole('dialog');await d.getByLabel('Группа',{exact:true}).selectOption(String(group.id));await d.getByLabel('Модуль',{exact:true}).selectOption(String(module.id));await d.getByLabel('Тема',{exact:true}).fill('R13 lesson '+i);
  const date=new Date(Date.now()-86400000);const local=(hour:number)=>new Date(Date.UTC(date.getUTCFullYear(),date.getUTCMonth(),date.getUTCDate(),hour)).toISOString().slice(0,16);
  await d.getByLabel('Начало (МСК)',{exact:true}).fill(local(10+i));await d.getByLabel('Окончание (МСК)',{exact:true}).fill(local(11+i));lessons.push(await save(admin,'/lessons','Сохранить занятие'));
 }
 const teacherPage=await pageFor();await login(teacherPage,'teacher@example.test',password,true);await go(teacherPage,`lessons/${lessons[0].id}`);
 await teacherPage.getByRole('button',{name:'Все присутствовали',exact:true}).click();let response=teacherPage.waitForResponse(r=>r.request().method()==='PUT'&&r.url().endsWith('/attendance'));await teacherPage.getByRole('button',{name:'Сохранить посещаемость',exact:true}).click();expect((await response).status()).toBe(204);
 await teacherPage.getByRole('button',{name:'Завершить занятие',exact:true}).click();response=teacherPage.waitForResponse(r=>r.url().endsWith('/complete'));await teacherPage.getByRole('dialog').getByRole('button',{name:'Подтвердить',exact:true}).click();expect((await response).status()).toBe(200);
 const curator=await pageFor();await login(curator,'curator@example.test',password,true);await go(curator,`students/${student.id}`);await expect(curator.getByRole('heading',{name:'R13 pupil',exact:true})).toBeVisible();
 const overview=await api(curator,`/students/${student.id}/overview`);expect(overview.status).toBe(200);expect(overview.data.full_access).toBe(true);expect((await api(curator,`/students/${foreign.id}/overview`)).status).toBe(404);
 const leader=await pageFor();await login(leader,'leader@example.test',password,true);await go(leader,'dashboard');await expect(leader.getByRole('heading',{name:'Обзор',exact:true})).toBeVisible();const dashboard=await api(leader,'/dashboard');expect(dashboard.status).toBe(200);expect(dashboard.data.active_students).toBe(2);
 await go(leader,'tasks');await leader.getByRole('button',{name:'Добавить: задача',exact:true}).click();d=leader.getByRole('dialog');await d.getByLabel('Название',{exact:true}).fill('R13 linked task');await d.locator('textarea[name=description]').fill('R13 expected result');await d.getByLabel('Ответственный',{exact:true}).selectOption(String(users.curator.id));await d.getByLabel('Срок (МСК)',{exact:true}).fill(new Date(Date.now()+86400000).toISOString().slice(0,16));await d.getByLabel('Связь с записью',{exact:true}).selectOption('student');await d.getByLabel('Связанная запись',{exact:true}).selectOption(String(student.id));const task=await save(leader,'/tasks','Сохранить задачу');
 await go(curator,'tasks');await curator.getByRole('button',{name:'В работу',exact:true}).click();await curator.getByRole('button',{name:'Завершить',exact:true}).click();await expect(curator.locator('.table-card').getByText('Выполнена',{exact:true}).first()).toBeVisible();expect((await api(curator,`/tasks/${task.id}`)).data.status).toBe('done');
 const mobile=await pageFor(360);await login(mobile,'teacher@example.test',changed);await go(mobile,`lessons/${lessons[1].id}`);await mobile.getByRole('button',{name:'Все присутствовали',exact:true}).click();response=mobile.waitForResponse(r=>r.request().method()==='PUT'&&r.url().endsWith('/attendance'));await mobile.getByRole('button',{name:'Сохранить посещаемость',exact:true}).click();expect((await response).status()).toBe(204);expect(await mobile.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 await go(admin,'audit');await expect(admin.getByRole('heading',{name:'История действий',exact:true})).toBeVisible();
 for(const action of ['lesson.completed','attendance.updated','task.created']){const audit=await api(admin,'/audit-logs?action='+action);expect(audit.status).toBe(200);expect(audit.data.total).toBeGreaterThan(0);}
 expect(unexpected).toEqual([]);for(const context of contexts)await context.close();
});
