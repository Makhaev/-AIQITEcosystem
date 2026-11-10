import {expect,test,type Page} from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import {mkdir} from 'node:fs/promises';

async function fixture(page:Page,partialFailure=false) {
  let role='curator',saved=false,published=false,createCount=0,patchCount=0,failed=false;
  const files:{id:number;name:string;size:number;created_at:string}[]=[]; const uploads:string[]=[];
  const metric={attendance:{present:0,late:0,absent:0,excused:0,unmarked:0,total:0,percent:null},assignments:{total:0,assigned:0,submitted:0,reviewed:0,overdue:0,percent:null}};
  const progress={period:{from:'2026-11-01',to:'2026-11-05'},previous_period:{from:'2026-10-27',to:'2026-10-31'},current:metric,previous:metric,monthly:[]};
  const parent={id:4,name:'Анна Иванова',relationship:'Мать',is_primary:true,status:'active',phone:'+79001112233',email:'parent@example.test',has_account:true};
  let report={id:20,student_id:10,student_name:'Иван Иванов',author_name:'Елена Петрова',period_from:'2026-11-01',period_to:'2026-11-05',summary:'Иван увереннее участвует в обсуждениях и подкрепляет аргументы примерами.',recommendations:'Обсуждайте небольшие истории и задавайте уточняющие вопросы.',goals:'Подготовить самостоятельное выступление.'};
  const view=()=>({...report,files,published_at:published?'2026-11-05T09:00:00Z':null,snapshot:published?progress:{},recipients:role==='curator'&&published?[parent]:[],feedback:[],can_feedback:role==='parent'&&published});
  await page.route('**/api/v1/**',async route=>{
    const path=new URL(route.request().url()).pathname.replace('/api/v1',''),method=route.request().method();
    const json=(value:unknown,status=200)=>route.fulfill({json:value,status});
    if(path==='/auth/me')return json({id:3,name:'Елена Петрова',email:'curator@example.test',roles:[role],blocked:false,must_change_password:false});
    if(path==='/auth/csrf')return json({csrf_token:'report-files-fixture'});
    if(path==='/features')return json({student360:true});
    if(path==='/curator/students/10')return json({student:{id:10,name:'Иван Иванов',status:'active',birth_date:null},parents:[parent],groups:[],schedule:[],attendance:[],assignments:[],teacher_reports:[],progress});
    if(path==='/curator-reports'&&method==='POST'){createCount++;saved=true;report={...report,...route.request().postDataJSON()};return json({id:20},201);}
    if(path==='/curator-reports')return json({items:saved?[view()]:[],total:saved?1:0});
    if(path==='/curator-reports/20'&&method==='PATCH'){patchCount++;return json({id:20});}
    if(path==='/curator-reports/20')return json(view());
    if(path==='/curator-reports/20/files'&&method==='POST'){
      expect(route.request().headers()['content-type']).toContain('multipart/form-data; boundary=');
      expect(route.request().headers()['x-csrf-token']).toBe('report-files-fixture');
      const name=route.request().postDataBuffer()!.toString('utf8').match(/filename="([^"]+)"/)![1];uploads.push(name);
      if(partialFailure&&files.length===1&&!failed){failed=true;return json({message:'Не удалось сохранить файл.'},500);}
      const file={id:files.length+1,name,size:2048,created_at:'2026-11-05T09:00:00Z'};files.push(file);return json(file,201);
    }
    if(path.startsWith('/curator-reports/20/files/')&&method==='DELETE'){const id=Number(path.split('/').at(-1));files.splice(files.findIndex(f=>f.id===id),1);return route.fulfill({status:204});}
    if(path==='/curator-reports/20/publish'){published=true;return json({id:20,recipient_count:1});}
    return json({message:'Нет данных.'},404);
  });
  return {parent:()=>{role='parent';},counts:()=>({createCount,patchCount,uploads:[...uploads]})};
}
async function editor(page:Page) {
  await page.goto('/#/curator-students/10');await page.getByRole('tab',{name:'Отчёты и цели',exact:true}).click();
  await page.getByRole('button',{name:'Подготовить отчёт родителям',exact:true}).click();
  await page.getByLabel('Итог периода для родителей',{exact:true}).fill('Иван увереннее участвует в обсуждениях и подкрепляет аргументы примерами.');
}
async function accessible(page:Page) {
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  expect((await new AxeBuilder({page}).withTags(['wcag2a','wcag2aa','wcag21aa']).analyze()).violations).toEqual([]);
}

test('curator chooses multiple attachments and publishes readable materials to parent',async({page},info)=>{
  const api=await fixture(page);await editor(page);
  const name='План развития ученика и подробные рекомендации на следующий месяц.pdf';
  await page.getByLabel('Выбрать файлы для отчёта',{exact:true}).setInputFiles([{name,mimeType:'application/pdf',buffer:Buffer.from('work')},{name:'Рекомендации.txt',mimeType:'text/plain',buffer:Buffer.from('notes')}]);
  await expect(page.getByRole('list',{name:'Выбранные вложения'}).getByRole('listitem')).toHaveCount(2);
  await accessible(page);await mkdir('../docs/frontend/report-files',{recursive:true});
  await page.locator('.report-dropzone').screenshot({path:`../docs/frontend/report-files/curator-${info.project.use.viewport!.width}.png`});
  await page.getByRole('list',{name:'Выбранные вложения'}).screenshot({path:`../docs/frontend/report-files/curator-files-${info.project.use.viewport!.width}.png`});
  await page.getByRole('button',{name:'Сохранить черновик',exact:true}).click();
  await expect(page.getByText('Вложений: 2',{exact:true})).toBeVisible();expect(api.counts().createCount).toBe(1);
  await page.getByRole('button',{name:'Выбрать родителей и опубликовать',exact:true}).click();
  await expect(page.getByRole('region',{name:'Файлы отчёта',exact:true})).toContainText(name);
  await page.getByRole('checkbox',{name:'Анна Иванова',exact:true}).check();
  await page.getByRole('button',{name:'Опубликовать для выбранных родителей',exact:true}).click();
  api.parent();await page.goto('/#/family-report/20');await page.reload();
  const materials=page.getByRole('region',{name:'Файлы отчёта',exact:true});
  await expect(materials.getByRole('link',{name,exact:true})).toHaveAttribute('href','/api/v1/curator-reports/20/files/1');
  await expect(page.getByLabel('Выбрать файлы для отчёта',{exact:true})).toHaveCount(0);
  await expect(page.getByRole('button',{name:/Удалить вложение/})).toHaveCount(0);await accessible(page);
  await materials.screenshot({path:`../docs/frontend/report-files/parent-${info.project.use.viewport!.width}.png`});
});
test('partial upload retry keeps text and does not duplicate drafts or successful files',async({page})=>{
  const api=await fixture(page,true);await editor(page);
  await page.getByLabel('Выбрать файлы для отчёта',{exact:true}).setInputFiles([{name:'first.txt',mimeType:'text/plain',buffer:Buffer.from('first')},{name:'second.txt',mimeType:'text/plain',buffer:Buffer.from('second')}]);
  await page.getByRole('button',{name:'Сохранить черновик',exact:true}).click();
  await expect(page.getByRole('alert')).toContainText('Текст черновика сохранён');
  await expect(page.getByLabel('Итог периода для родителей',{exact:true})).not.toBeEmpty();
  await expect(page.getByRole('list',{name:'Выбранные вложения'}).getByRole('listitem')).toHaveCount(1);
  await page.getByRole('button',{name:'Сохранить черновик',exact:true}).click();
  await expect(page.getByText('Вложений: 2',{exact:true})).toBeVisible();
  expect(api.counts()).toEqual({createCount:1,patchCount:1,uploads:['first.txt','second.txt','second.txt']});
});
test('invalid files are rejected and saved draft attachments can be removed',async({page})=>{
  await fixture(page);await editor(page);
  const input=page.getByLabel('Выбрать файлы для отчёта',{exact:true});
  await input.setInputFiles({name:'empty.txt',mimeType:'text/plain',buffer:Buffer.alloc(0)});
  await expect(page.getByRole('alert')).toContainText('непустой файл');
  await input.setInputFiles({name:'work.txt',mimeType:'text/plain',buffer:Buffer.from('work')});
  await page.getByRole('button',{name:'Сохранить черновик',exact:true}).click();await expect(page.getByText('Вложений: 1',{exact:true})).toBeVisible();
  await page.getByRole('button',{name:'Изменить черновик',exact:true}).click();
  await page.getByRole('button',{name:'Удалить вложение work.txt',exact:true}).click();
  await page.getByRole('group',{name:'Удалить вложение из черновика?',exact:true}).getByRole('button',{name:'Удалить вложение',exact:true}).click();
  await expect(page.getByRole('region',{name:'Файлы отчёта',exact:true})).toHaveCount(0);await accessible(page);
});
