import { useEffect, useState, type FormEvent } from 'react';
import { message, request } from '../api';
import { Badge, Button, Card, EmptyState, Input, Notice, PageHeading, Pagination, Select, Table, type Column } from '../components/ui';
import { day, ProgressView, type Progress } from './CuratorReports';

function useMethodist<T>(path: string) {
  const [data,setData]=useState<T|null>(null); const [error,setError]=useState(''); const [version,setVersion]=useState(0);
  useEffect(()=>{const controller=new AbortController();setData(null);setError('');void request<T>(path,'GET',undefined,controller.signal).then(value=>{if(!controller.signal.aborted)setData(value);}).catch(err=>{if(!controller.signal.aborted)setError(message(err));});return()=>controller.abort();},[path,version]);
  return {data,error,retry:()=>setVersion(v=>v+1)};
}
function RemoteState({data,error,retry}:{data:unknown;error:string;retry:()=>void}) {return <><Notice tone="error">{error}</Notice>{error?<Button variant="secondary" onClick={retry}>Повторить загрузку</Button>:!data&&<p role="status">Загружаем учебные данные…</p>}</>;}
type Group={id:number;name:string;program_id:number;program_name:string;teacher_name?:string;starts_on?:string;ends_on?:string|null};
type Student={id:number;name:string;status:string;groups:Group[]};
type Report={id:number;student_id:number;student_name:string;author_name:string;summary:string;published_at:string|null;created_at:string;lesson_topic?:string;lesson_date?:string;program_name?:string;program_id?:number;period_from?:string;period_to?:string;recommendations?:string;goals?:string;snapshot?:Partial<Progress>};

export function MethodistImplementationNav({ current }: { current: string }) {
  return <nav className="methodist-sections" aria-label="Реализация программ">{[
    ['methodist-programs', 'Программы и группы'], ['methodist-students', 'Прогресс учеников'],
    ['methodist-teacher-reports', 'Отчёты педагогов'], ['methodist-curator-reports', 'Отчёты кураторов'],
  ].map(([route, label]) => <a key={route} href={`#/${route}`} aria-current={current === route ? 'page' : undefined}>{label}</a>)}</nav>;
}

export function MethodistWorkspace() {
  const remote=useMethodist<{counts:{students:number;programs:number;teacher_reports:number;curator_reports:number};recent_reports:{student_id:number;student_name:string;kind:string;summary:string;created_at:string}[]}>('/methodist/workspace');
  return <><PageHeading title="Кабинет методиста" description="Учебная ситуация учеников, программы и отчёты педагогов и кураторов."/><div className="stack"><RemoteState {...remote}/>{remote.data&&<><div className="stats">{([
    ['students','Активные ученики','#/methodist-students'],['programs','Активные программы','#/programs'],['teacher_reports','Отчёты педагогов','#/methodist-teacher-reports'],['curator_reports','Отчёты кураторов','#/methodist-curator-reports'],
  ] as const).map(([key,label,href])=><Card key={key}><h2><a href={href}>{label}</a></h2><span className="stat-value" aria-label={label}>{remote.data!.counts[key]}</span></Card>)}</div><Card><h2>Последние отчёты</h2>{remote.data.recent_reports.length?remote.data.recent_reports.map((report,index)=><article className="pilot-list-row" key={`${report.kind}:${index}`}><h3><a href={`#/methodist-students/${report.student_id}`}>{report.student_name}</a></h3><p className="muted">{report.kind==='teacher'?'Отчёт педагога':'Отчёт куратора'} · {new Date(report.created_at).toLocaleString('ru-RU',{timeZone:'Europe/Moscow'})} МСК</p><p className="pilot-prose">{report.summary}</p></article>):<p>Отчётов пока нет.</p>}</Card></>}</div></>;
}
export function MethodistStudents() {
  const [page,setPage]=useState(1); const [filters,setFilters]=useState('status=active');
  const remote=useMethodist<{items:Student[];total:number}>(`/methodist/students?page=${page}&page_size=20&${filters}`);
  const columns:Column<Student>[]=[{key:'name',title:'Ученик',render:s=><a href={`#/methodist-students/${s.id}`}>{s.name}</a>},{key:'groups',title:'Группы и программы',render:s=>s.groups.length?s.groups.map(g=><p key={g.id}>{g.name} · <a href={`#/programs/${g.program_id}`}>{g.program_name}</a></p>):'Текущих групп нет'},{key:'status',title:'Статус',render:s=><Badge>{s.status==='active'?'Активен':'В архиве'}</Badge>},{key:'action',title:'Учебная ситуация',render:s=><a href={`#/methodist-students/${s.id}`}>Прогресс и отчёты</a>}];
  function filter(event:FormEvent<HTMLFormElement>){event.preventDefault();const data=new FormData(event.currentTarget);const params=new URLSearchParams();for(const key of ['search','status'])if(data.get(key))params.set(key,String(data.get(key)));setFilters(params.toString());setPage(1);}
  return <><PageHeading title="Ученики" description="Прогресс, текущие группы, программы и отчёты по каждому ученику."/><form className="filters" onSubmit={filter}><Input label="Поиск ученика" name="search" maxLength={200}/><Select label="Статус" name="status" defaultValue="active"><option value="">Все статусы</option><option value="active">Активен</option><option value="archived">В архиве</option></Select><Button type="submit" variant="secondary">Найти</Button></form><div className="stack"><RemoteState {...remote}/>{remote.data&&<Card className="table-card">{remote.data.items.length?<Table caption="Ученики методиста" columns={columns} items={remote.data.items} rowKey={s=>s.id}/>:<EmptyState title="Ученики не найдены" description="Измените поиск или статус."/>}<Pagination page={page} pageSize={20} total={remote.data.total} onChange={setPage}/></Card>}</div></>;
}
type TeachingProgram = { id: number; group_name: string; program_id: number; program_name: string; teacher_name: string | null; module_count: number; student_count: number; lesson_count: number; completed_lessons: number };
export function MethodistPrograms() {
  const [page, setPage] = useState(1);
  const [search, setSearch] = useState('');
  const remote = useMethodist<{ items: TeachingProgram[]; total: number }>(`/methodist/programs?${new URLSearchParams({ page: String(page), page_size: '20', search })}`);
  const columns: Column<TeachingProgram>[] = [
    { key: 'program', title: 'Программа', render: row => <a href={`#/programs/${row.program_id}`}>{row.program_name}</a> },
    { key: 'teacher', title: 'Педагог', render: row => row.teacher_name || 'Педагог не назначен' },
    { key: 'group', title: 'Группа', render: row => row.group_name },
    { key: 'students', title: 'Активные ученики', render: row => row.student_count },
    { key: 'modules', title: 'Модули', render: row => row.module_count },
    { key: 'lessons', title: 'Занятия проведены / всего', render: row => `${row.completed_lessons} / ${row.lesson_count}` },
  ];
  return <><PageHeading title="Реализация программ" description="Программы действующих групп, педагоги и проведённые занятия. Отменённые занятия исключены." />
    <form className="filters" onSubmit={event => { event.preventDefault(); setSearch(String(new FormData(event.currentTarget).get('search') || '').trim()); setPage(1); }}>
      <Input label="Программа, педагог или группа" name="search" maxLength={200} /><Button type="submit" variant="secondary">Найти</Button>
    </form><div className="stack"><RemoteState {...remote} />{remote.data && <Card className="table-card"><div className="table-heading"><h2>Программы действующих групп</h2><span className="muted">Всего: {remote.data.total}</span></div>
      {remote.data.items.length ? <Table caption="Программы педагогов" columns={columns} items={remote.data.items} rowKey={row => row.id} /> : <EmptyState title="Программы не найдены" description="Измените поиск. Здесь отображаются программы действующих групп." />}
      <Pagination page={page} pageSize={20} total={remote.data.total} onChange={setPage} />
    </Card>}<a href="#/programs">Все программы и модули</a></div></>;
}
function defaultPeriod(){const date=new Date();const format=(d:Date)=>new Intl.DateTimeFormat('sv-SE',{timeZone:'Europe/Moscow'}).format(d);return{from:format(new Date(date.getTime()-29*86400_000)),to:format(date)};}
export function MethodistStudent({id}:{id:string}) {
  const [period,setPeriod]=useState(defaultPeriod);
  const remote=useMethodist<{student:{id:number;name:string;status:string};groups:Group[];progress:Progress;assignments:{id:number;title:string;status:string;answer:string;feedback:string;teacher_name:string}[]}>(`/methodist/students/${id}?${new URLSearchParams(period)}`);
  const data=remote.data;
  return <><a className="back-link" href="#/methodist-students">← Ученики</a><PageHeading title={data?.student.name||'Учебная ситуация ученика'} description="Методический просмотр прогресса, программы и отчётов."/><form className="filters" onSubmit={event=>{event.preventDefault();const input=new FormData(event.currentTarget);setPeriod({from:String(input.get('from')),to:String(input.get('to'))});}}><Input label="Период с" name="from" type="date" required defaultValue={period.from}/><Input label="Период по" name="to" type="date" required defaultValue={period.to}/><Button type="submit" variant="secondary">Показать период</Button></form><div className="stack"><RemoteState {...remote}/>{data&&<><Card><h2>Программы и группы</h2><Badge>{data.student.status==='active'?'Активен':'В архиве'}</Badge>{data.groups.length?data.groups.map((g,index)=><article className="pilot-list-row" key={`${g.id}:${index}`}><h3>{g.name}</h3><a href={`#/programs/${g.program_id}`}>{g.program_name}</a><p>{g.teacher_name||'Педагог не назначен'}</p>{g.starts_on&&<p>{day(g.starts_on)} — {g.ends_on?day(g.ends_on):'обучается'}</p>}</article>):<p>Зачислений пока нет.</p>}</Card><Card><h2>Прогресс ученика</h2><ProgressView value={data.progress}/></Card><Card><h2>Задания и результаты</h2>{data.assignments.length?data.assignments.map(task=><article className="pilot-list-row" key={task.id}><h3>{task.title}</h3><p>{task.teacher_name} · {{assigned:'К выполнению',submitted:'На проверке',reviewed:'Принято'}[task.status]||task.status}</p>{task.answer&&<p className="pilot-prose">Ответ ученика: {task.answer}</p>}{task.feedback&&<p className="pilot-prose">Комментарий педагога: {task.feedback}</p>}</article>):<p>Заданий за период нет.</p>}</Card><MethodistReports curator={false} studentID={id} period={period} embedded/><MethodistReports curator studentID={id} period={period} embedded/></>}</div></>;
}
export function MethodistReports({curator,studentID,period,embedded=false}:{curator:boolean;studentID?:string;period?:{from:string;to:string};embedded?:boolean}) {
  const [page,setPage]=useState(1);const [dates,setDates]=useState('');
  const selection=studentID||new URLSearchParams(window.location.hash.split('?')[1]).get('student_id');
  const params=new URLSearchParams(period||dates);params.set('page',String(page));params.set('page_size','20');if(selection)params.set('student_id',selection);
  const remote=useMethodist<{items:Report[];total:number}>(`/methodist/${curator?'curator':'teacher'}-reports?${params}`);
  useEffect(()=>setPage(1),[period?.from,period?.to,studentID]);
  const title=curator?'Отчёты кураторов':'Отчёты педагогов';
  const content=<div className="stack"><RemoteState {...remote}/>{remote.data&&<>{remote.data.items.length?remote.data.items.map(report=><Card key={report.id}><h3>{embedded?report.author_name:<a href={`#/methodist-students/${report.student_id}`}>{report.student_name}</a>}</h3><p className="muted">??????: {new Date(report.created_at).toLocaleString('ru-RU',{timeZone:'Europe/Moscow'})} ???</p><p className="muted">{curator?'Куратор':'Педагог'}: {report.author_name}</p>{report.period_from&&report.period_to&&<p>{day(report.period_from)} — {day(report.period_to)}</p>}{report.lesson_topic&&<p>{report.lesson_topic} · {report.lesson_date&&day(report.lesson_date)}</p>}{report.program_id&&<p><a href={`#/programs/${report.program_id}`}>{report.program_name}</a></p>}<Badge tone={report.published_at?'success':'warning'}>{report.published_at?'Опубликован родителям':'Черновик — родителям не виден'}</Badge><p className="pilot-prose">{report.summary}</p>{report.recommendations&&<><h3>Рекомендации</h3><p className="pilot-prose">{report.recommendations}</p></>}{report.goals&&<><h3>Цели</h3><p className="pilot-prose">{report.goals}</p></>}{report.published_at&&report.snapshot?.current&&<details><summary>Показатели на момент публикации</summary><ProgressView value={report.snapshot as Progress} snapshot/></details>}</Card>):<Card><p>Отчётов за выбранный период нет.</p></Card>}<Pagination page={page} pageSize={20} total={remote.data.total} onChange={setPage}/></>}</div>;
  if(embedded)return <section className="stack" aria-label={title}><h2>{title}</h2>{content}</section>;
  return <><PageHeading title={title} description="Учебные отчёты, включая черновики. Публикация родителям выполняется автором отчёта."/><form className="filters" onSubmit={event=>{event.preventDefault();const data=new FormData(event.currentTarget);const p=new URLSearchParams();for(const key of ['from','to','search','publication'])if(data.get(key))p.set(key,String(data.get(key)));setDates(p.toString());setPage(1);}}><Input label="Ученик или автор отчёта" name="search" maxLength={200}/><Select label="Публикация" name="publication"><option value="">Все отчёты</option><option value="draft">Черновики</option><option value="published">Опубликованные</option></Select><Input label="Период с" name="from" type="date"/><Input label="Период по" name="to" type="date"/><Button type="submit" variant="secondary">Применить</Button></form>{content}</>;
}
