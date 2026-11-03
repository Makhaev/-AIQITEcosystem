import { FamilyReportContent, type ReportComposition } from './FamilyReportContent';
import { FamilyReportNotifications } from './FamilyReportNotifications';
import { PublishedReport } from './PublishedReport';
import { FamilyRequests } from './FamilyRequests';
import { useEffect, useState } from 'react';
import { StudentHome, StudentAssignment, StudyContent, type StudyRecord } from './StudentPortal';
import { ParentHome, EventContent, type FamilyEvent } from './ParentPortal';
import { message, request } from '../api';
import { Button, Card, Input, Notice, PageHeading, Pagination, Select } from '../components/ui';

type Page<T> = { items: T[]; total: number; page: number; page_size: number };
type Pupil = { id: number; name: string; own?: boolean };
export type FamilyFile = { id: number; name: string; url: string };
export type FamilyReport = ReportComposition & { id: number; title: string; body: string; revision: number; published: boolean; files: FamilyFile[] };
export type FamilyRequest = { id: number; subject: string; body: string; reply: string; revision: number };
type Record = StudyRecord;
type Lesson = { id: number; topic: string; starts_at: string; ends_at: string; status: string; attendance: string };
const states: { [key: string]: string } = { scheduled: 'Запланировано', planned: 'Запланировано', active: 'В работе', achieved: 'Достигнуто', cancelled: 'Отменено', assigned: 'Назначено', completed: 'Выполнено', submitted: 'На проверке', accepted: 'Принято', revision: 'На доработке', reviewed: 'Проверено', closed: 'Закрыто', present: 'Присутствовал', absent: 'Отсутствовал', excused: 'Уважительная причина', late: 'Опоздал', unmarked: 'Не отмечен' };
export function FamilyAttachments({ files }: { files: FamilyFile[] }) { return <ul>{files.filter(f => f.url).map(f => <li key={f.id}><a href={f.url} download>{f.name}</a></li>)}</ul>; }
export function FamilyPortal({ studentView = false }: { studentView?: boolean }) {
 const [pupils, setPupils] = useState<Page<Pupil> | null>(null); const [pupilPage, setPupilPage] = useState(1); const [sid, setSid] = useState(''); const [search, setSearch] = useState(''); const [error, setError] = useState(''); const [reload, setReload] = useState(0);
 useEffect(() => { const c = new AbortController(); setPupils(null); setError(''); request<Page<Pupil>>(`/family/students?page=${pupilPage}&search=${encodeURIComponent(search)}`, 'GET', undefined, c.signal).then(v => { if (!c.signal.aborted) { setPupils(v); if(studentView) { const own=v.items.find(p=>p.own); setSid(own ? String(own.id) : String(v.items[0]?.id || '')); } else { setSid(current => v.items.some(p => String(p.id) === current) ? current : String(v.items[0]?.id || '')); } } }).catch(e => { if (!c.signal.aborted) { setSid(''); setError(message(e)); } }); return () => c.abort(); }, [pupilPage, search, reload, studentView]);
 const singleOwn=studentView && pupils?.total===1 && pupils.items[0]?.own;
 return <><PageHeading title={studentView ? "Кабинет ученика" : "Кабинет родителя"} description={studentView ? "Мои занятия, задания и результаты обучения." : "Занятия, учебные работы и опубликованные сведения доступных учеников."} /><Notice tone="error">{error}</Notice><Card>
 {singleOwn ? <><p>{pupils.items[0].name}</p><Button variant="secondary" onClick={()=>setReload(v=>v+1)}>Обновить кабинет</Button></> : <>
 <form onSubmit={e => { e.preventDefault(); setSearch(String(new FormData(e.currentTarget).get('search') || '')); setPupilPage(1); setSid(''); setReload(v => v + 1); }} className="filters"><Input name="search" label="Поиск ученика" maxLength={100} /><Button type="submit">Найти</Button><Button variant="secondary" onClick={() => { setSid(''); setReload(v => v + 1); }}>Обновить</Button></form>
 {pupils ? <><Select label="Ученик" value={sid} onChange={e => setSid(e.target.value)}><option value="">Выберите ученика</option>{pupils.items.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</Select>{!pupils.items.length && <p>Нет доступных учеников. Для родителя связь подтверждает администратор.</p>}<Pagination page={pupilPage} total={pupils.total} pageSize={20} onChange={p => { setPupilPage(p); setSid(''); }} /></> : !error && <p role="status">Загрузка…</p>}
 </>}
 </Card>{pupils && sid && <FamilyLearning key={`${sid}-${reload}`} sid={sid} own={Boolean(pupils?.items.find(p=>String(p.id)===sid)?.own)} />}</>;
}
function FamilyLearning({ sid, own }: { sid: string; own: boolean }) {
 const [reportID, setReportID] = useState<number | null>(null);
 const [assignment,setAssignment]=useState<number|null>(null);
 const [lessonPeriod, setLessonPeriod] = useState({from: '', to: ''});
 const [section, setSection] = useState('home'); const [page, setPage] = useState(1); const [search, setSearch] = useState(''); const [data, setData] = useState<Page<Record | Lesson | FamilyReport | FamilyRequest | FamilyEvent> | null>(null); const [progress, setProgress] = useState<{ goals: number; achieved_goals: number; assignments: number; completed_assignments: number } | null>(null); const [error, setError] = useState(''); const [version, setVersion] = useState(0);
 useEffect(() => { if(section==='home'||section==='requests'||assignment!==null)return; const c = new AbortController(); setData(null); setProgress(null); setError(''); const learning = ['assignments', 'attempts', 'goals', 'observations', 'projects'].includes(section); const path = `/family/students/${sid}/${learning ? 'learning' : section}?page=${page}&search=${encodeURIComponent(search)}${learning ? `&kind=${section}` : ''}${section === 'lessons' && lessonPeriod.from ? `&from=${lessonPeriod.from}&to=${lessonPeriod.to}` : ''}`;
 request<any>(path, 'GET', undefined, c.signal).then(v => { if (!c.signal.aborted) { if (section === 'progress') setProgress(v); else setData(v); } }).catch(e => { if (!c.signal.aborted) setError(message(e)); }); return () => c.abort(); }, [sid, section, page, search, version, assignment, lessonPeriod]);
 if(assignment!==null)return <StudentAssignment key={assignment} id={assignment} sid={sid} onBack={()=>{setAssignment(null);requestAnimationFrame(()=>document.querySelector<HTMLElement>('[data-learning-section]')?.focus())}}/>;
 return <div className="stack"><Card><Select data-learning-section label="Раздел обучения" value={section} onChange={e => { setData(null); setProgress(null); setError(''); setSection(e.target.value); setPage(1); setSearch(''); setLessonPeriod({from: '', to: ''}); }}>
 {Object.entries({ home: 'Главная', ...(own ? {projects: 'Мои проекты'} : {events: 'Доступные события'}), lessons: 'Занятия', assignments: 'Задания', attempts: own ? 'Мои работы' : 'Работы ребёнка', goals: 'Цели', observations: 'Обратная связь', progress: 'Прогресс', reports: 'Отчёты', requests: 'Мои обращения' }).map(([v, label]) => <option key={v} value={v}>{label}</option>)}
 </Select>{section!=='home' && <form key={section} className="filters" onSubmit={e => { e.preventDefault(); const values = new FormData(e.currentTarget); const from=String(values.get('from') || ''); const to=String(values.get('to') || ''); if(section==='lessons' && ((!!from !== !!to) || from>to)){setError('Укажите обе даты в правильном порядке.');return;} setData(null);setProgress(null);setError('');setSearch(String(values.get('search') || ''));setLessonPeriod({from,to}); setPage(1); }}><Input label="Поиск по названию" name="search" maxLength={100} />{section==='lessons' && <><Input label="Расписание с" name="from" type="date"/><Input label="Расписание по" name="to" type="date"/></>}<Button type="submit">Найти</Button><Button variant="secondary" onClick={() => setVersion(v => v + 1)}>Обновить данные</Button></form>}</Card>
 {section==='home' ? (own ? <StudentHome key={version} sid={sid} onAssignment={setAssignment} onSection={s=>{setSection(s);setPage(1);setSearch('')}}/> : <ParentHome sid={sid} onSection={s=>{setData(null);setProgress(null);setError('');setSection(s);setPage(1);setSearch('')}}/>) : section==='requests' ? <FamilyRequests sid={sid}/> : <>
 <Notice tone="error">{error}</Notice>{!data && !progress && !error && <p role="status">Загрузка…</p>}
 {section === 'reports' && data && !error && <FamilyReportNotifications key={`${sid}-${version}`} sid={sid} />}
 {progress && <Card><h2>Личный прогресс</h2><p>Достигнутые цели: {progress.achieved_goals} из {progress.goals}.</p><p>Выполненные задания: {progress.completed_assignments} из {progress.assignments}.</p><p>Это количество записей, а не оценка уровня навыков.</p></Card>}
 {data?.items.map(v => <Card key={`${'kind' in v ? v.kind : section}-${v.id}`}>{'content' in v ? <><h2>{v.content.title || 'Учебная запись'}</h2><StudyContent record={v}/>{own && v.kind==='assignments' && <Button onClick={()=>setAssignment(v.id)}>Открыть задание</Button>}{own && v.parent && <Button variant="secondary" onClick={()=>setAssignment(v.parent!.id)}>Открыть задание</Button>}</> : 'kind' in v ? <><h2>{v.title}</h2><EventContent event={v}/></> : 'topic' in v ? <><h2>{v.topic}</h2><p>{new Date(v.starts_at).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow' })} — {new Date(v.ends_at).toLocaleTimeString('ru-RU', { timeZone: 'Europe/Moscow' })} (Москва)</p><p>{states[v.status] || v.status} · {states[v.attendance] || v.attendance}</p></> : 'subject' in v ? <><h2>{v.subject}</h2><p style={{ whiteSpace: 'pre-wrap' }}>{v.body}</p><p>Ответ: {v.reply || 'Ожидается'}</p></> : <><h2>{v.title}</h2><FamilyReportContent report={v} /><FamilyAttachments files={v.files} /><Button variant="secondary" onClick={() => setReportID(v.id)} aria-label={`Открыть отчёт: ${v.title}`}>Открыть отчёт</Button></>}</Card>)}
 {data && <>{!data.items.length && <Card><p>Нет доступных записей.</p></Card>}<Pagination page={page} total={data.total} pageSize={20} onChange={setPage} /></>}

 </>}
 {reportID !== null && <PublishedReport id={reportID} onClose={() => setReportID(null)} />}
 </div>;
}
