import { expect, test } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

for (const format of ['text','link','file']) {
 test(`student home and ${format} submission preserve attempt history`,async({page},info)=>{
  let assignment={id:11,student_id:7,kind:'assignments',state:'assigned',revision:2,can_submit:true,content:{title:'Робот и алгоритм',instruction:'Объясни маршрут робота.',criteria:'Объяснение каждого шага',format,due_on:'2099-10-30',allow_late:false},files:[{id:9,name:'Инструкция.pdf',url:'/api/v1/family/learning/11/files/9'}],sources:[]};
  const attempts:any[]=[{id:21,student_id:7,kind:'attempts',state:'revision',revision:2,created_at:'2026-10-26T10:00:00Z',content:{title:'Первая работа',answer:'Мой первый ответ',format:'text'},feedback:'Добавь условие остановки.',files:[],sources:[],parent:{id:11,title:'Робот и алгоритм'}}];
  let received:any;let revoked=false;
  await page.route('**/api/v1/**',route=>{
   const url=new URL(route.request().url());const p=url.pathname.replace('/api/v1','');
   if(p==='/auth/me')return route.fulfill({json:{id:3,name:'Анна',email:'anna@example.test',roles:['student'],must_change_password:false,blocked:false}});
   if(p==='/auth/csrf')return route.fulfill({json:{csrf_token:'csrf'}});
   if(p==='/family/students')return route.fulfill({json:{items:[{id:7,name:'Анна',own:true}],total:1}});
   if(revoked)return route.fulfill({status:404,json:{message:'Доступ отозван.'}});
   if(p==='/family/students/7/home')return route.fulfill({json:{next_lesson:{topic:'Маршруты робота',starts_at:'2099-10-27T12:00:00Z',ends_at:'2099-10-27T13:00:00Z'},programs:[{title:'Робототехника',version:2,percent:50,state:'ready'}],blocks:{assignments:[assignment],goals:[],attempts,projects:[],observations:[]},totals:{assignments:1,goals:0,attempts:attempts.length,projects:0,observations:0}}});
   if(p==='/family/learning/11')return route.fulfill({json:assignment});
   if(p==='/family/students/7/learning'){expect(url.searchParams.get('assignment_id')).toBe('11');return route.fulfill({json:{items:attempts,total:attempts.length}})}
   if(p==='/family/assignments/11/attempts'){
    received=route.request().postDataJSON();expect(received.revision).toBe(2);expect(route.request().headers()['x-csrf-token']).toBe('csrf');
    attempts.unshift({id:22,student_id:7,kind:'attempts',state:'submitted',revision:1,created_at:'2026-10-26T11:00:00Z',content:{title:'Новая попытка',answer:received.answer,format},files:[],sources:[],parent:{id:11,title:assignment.content.title}});
    return route.fulfill({status:201,json:{id:22}});
   }
   return route.fulfill({status:404,json:{message:'Не найдено'}});
  });
  await page.goto('/');
  await expect(page.getByRole('heading',{name:'Кабинет ученика'})).toBeVisible();
  await expect(page.getByRole('heading',{name:'Маршруты робота'})).toBeVisible();
  await expect(page.getByRole('progressbar')).toHaveAttribute('value','50');
  if(format==='text'){
   expect((await new AxeBuilder({page}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze()).violations).toEqual([]);
   await page.screenshot({path:info.outputPath('student-home.png'),fullPage:true});
  }
  await page.getByRole('button',{name:'Открыть задание',exact:true}).click();
  await expect(page.getByRole('link',{name:'Инструкция.pdf'})).toBeVisible();
  await expect(page.getByText('Добавь условие остановки.',{exact:false})).toBeVisible();
  if(format==='text')await page.getByLabel('Текст ответа').fill('Добавил условие остановки.');
  else if(format==='link')await page.getByLabel('Ссылка на работу').fill('https://example.test/robot');
  else await page.getByLabel('Файл работы').setInputFiles({name:'robot.txt',mimeType:'text/plain',buffer:Buffer.from('Robot answer')});
  await page.getByRole('button',{name:'Отправить попытку'}).click();
  await expect(page.getByText('Попытка №22 отправлена на проверку.')).toBeVisible();
  await expect(page.getByRole('heading',{name:'Первая работа'})).toBeVisible();
  await expect(page.getByRole('heading',{name:'Новая попытка'})).toBeVisible();
  if(format==='file')expect(Buffer.from(received.file.data,'base64').toString()).toBe('Robot answer');
  else expect(received.answer).toBe(format==='text'?'Добавил условие остановки.':'https://example.test/robot');
  expect((await new AxeBuilder({page}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze()).violations).toEqual([]);
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  await page.screenshot({path:info.outputPath(`student-${format}.png`),fullPage:true});
  assignment={...assignment,state:'completed',can_submit:false};
  await page.getByRole('button',{name:'Обновить задание'}).click();
  await expect(page.getByRole('button',{name:'Отправить попытку'})).toHaveCount(0);
  revoked=true;await page.getByRole('button',{name:'Обновить задание'}).click();
  await expect(page.getByRole('alert')).toContainText('Доступ отозван');
  await expect(page.getByRole('heading',{name:'Первая работа'})).toHaveCount(0);
  await page.getByRole('button', {name:'Назад к обучению'}).click();
  await expect(page.getByLabel('Раздел обучения')).toBeFocused();
 });
}
