import { FamilyReportContent } from './FamilyReportContent';
import { PublishedReport } from './PublishedReport';
import { useEffect, useState, type FormEvent } from 'react';
import { message, request } from '../api';
import { Button, Card, Input, Notice } from '../components/ui';
import { FamilyAttachments, type FamilyReport, type FamilyRequest } from './FamilyPortal';
import { StudyContent, type StudyRecord } from './StudentPortal';

export type FamilyEvent = { id: number; kind: 'lesson' | 'assignment'; title: string; starts_at?: string; ends_at?: string; due_on?: string };
type Lesson = { id: number; topic: string; starts_at: string; ends_at: string };
type ParentHomeData = {
 programs: { title: string; version: number; percent: number | null }[];
 blocks: { [kind: string]: StudyRecord[] }; totals: { [kind: string]: number };
 parent: {
  attendance: { from: string; to: string; present: number; late: number; absent: number; excused: number; total: number; percent: number | null };
  lessons: Lesson[]; reports: FamilyReport[]; requests: FamilyRequest[]; events: FamilyEvent[]; totals: { [kind: string]: number };
 };
};
const date = (value: string) => new Date(value).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow' });
const percent = (value: number) => `${value.toLocaleString('ru-RU', { maximumFractionDigits: 1 })}%`;
export function EventContent({ event }: { event: FamilyEvent }) {
 return <><p>{event.kind === 'lesson' ? 'Занятие' : 'Срок задания'}</p>{event.starts_at ? <p>{date(event.starts_at)}{event.ends_at && ` — ${date(event.ends_at)}`} (Москва)</p> : <p>Сдать до конца дня: {event.due_on?.split('-').reverse().join('.')} (Москва)</p>}</>;
}

export function ParentHome({ sid, onSection }: { sid: string; onSection: (kind: string) => void }) {
 const [reportID, setReportID] = useState<number | null>(null);
 const [home, setHome] = useState<ParentHomeData | null>(null);
 const [error, setError] = useState('');
 const [period, setPeriod] = useState({ from: '', to: '' });
 const [reload, setReload] = useState(0);
 useEffect(() => {
  const controller = new AbortController();
  setHome(null); setError('');
  const query = new URLSearchParams(period.from ? period : {});
  request<ParentHomeData>(`/family/students/${sid}/parent-home?${query}`, 'GET', undefined, controller.signal)
   .then(value => { if (!controller.signal.aborted) setHome(value); })
   .catch(e => { if (!controller.signal.aborted) setError(message(e)); });
  return () => controller.abort();
 }, [sid, period, reload]);
 function applyPeriod(e: FormEvent<HTMLFormElement>) {
  e.preventDefault();
  const values = new FormData(e.currentTarget);
  const from = String(values.get('from')); const to = String(values.get('to'));
  if (from > to) { setError('Начало периода должно быть не позже окончания.'); return; }
  setHome(null); setError(''); setPeriod({ from, to });
 }
 return <div className="stack">
  <div><Button variant="secondary" onClick={() => { setHome(null); setReload(v => v + 1); }}>Обновить главную</Button></div>
  <Notice tone="error">{error}</Notice>
  {!home && !error && <p role="status">Загрузка сведений ребёнка…</p>}
  {home && <>
   <Card><h2>Ближайшие занятия и расписание</h2>
    {home.parent.lessons.length ? home.parent.lessons.map(l => <article key={l.id}><h3>{l.topic}</h3><p>{date(l.starts_at)} — {date(l.ends_at)} (Москва)</p></article>) : <p>Ближайших занятий пока нет.</p>}
    <Button variant="secondary" onClick={() => onSection('lessons')}>Всё расписание</Button>
   </Card>
   <Card><h2>Посещаемость за период</h2>
    <form className="filters" onSubmit={applyPeriod}>
     <Input label="Период с" name="from" type="date" defaultValue={home.parent.attendance.from} required />
     <Input label="Период по" name="to" type="date" defaultValue={home.parent.attendance.to} required />
     <Button type="submit">Показать посещаемость</Button>
    </form>
    <p>{home.parent.attendance.percent === null ? 'Нет данных о посещаемости за этот период.' : `Посещаемость: ${percent(home.parent.attendance.percent)}`}</p>
    <ul><li>Присутствовал: {home.parent.attendance.present}</li><li>Опоздал: {home.parent.attendance.late}</li><li>Отсутствовал: {home.parent.attendance.absent}</li><li>Уважительная причина: {home.parent.attendance.excused}</li></ul>
    <p>Учитываются отметки завершённых занятий, включая обе выбранные даты по Москве. Присутствия и опоздания делятся на все учтённые отметки.</p>
   </Card>
   <Card><h2>Программа и прогресс</h2>
    {home.programs.length ? home.programs.map((p, i) => <article key={i}><h3>{p.title}{p.version ? ` · версия ${p.version}` : ''}</h3>{p.percent === null ? <p>Нет данных для расчёта.</p> : <><progress aria-label={`Прогресс: ${p.title}`} value={p.percent} max={100} /><p>{percent(p.percent)}</p></>}</article>) : <p>Нет действующей учебной программы.</p>}
    <p>Выполнение программы группы по подтверждённым занятиям.</p>
    <Button variant="secondary" onClick={() => onSection('progress')}>Личные цели и выполненные задания</Button>
   </Card>
   {Object.entries({ observations: 'Опубликованные результаты', attempts: 'Работы ребёнка' }).map(([kind, title]) => <Card key={kind}><h2>{title}</h2>
    {home.blocks[kind]?.length ? home.blocks[kind].map(r => <article key={r.id}><h3>{r.content.title}</h3><StudyContent record={r} /></article>) : <p>Пока нет доступных записей.</p>}
    <Button variant="secondary" onClick={() => onSection(kind)}>Все {kind === 'observations' ? 'результаты' : 'работы'} ({home.totals[kind] || 0})</Button>
   </Card>)}
   <Card><h2>Опубликованные отчёты</h2>
    {home.parent.reports.length ? home.parent.reports.map(r => <article key={r.id}><h3>{r.title}</h3><FamilyReportContent report={r} /><FamilyAttachments files={r.files} /><Button variant="secondary" onClick={() => setReportID(r.id)} aria-label={`Открыть отчёт: ${r.title}`}>Открыть отчёт</Button></article>) : <p>Опубликованных отчётов пока нет.</p>}
    <Button variant="secondary" onClick={() => onSection('reports')}>Все отчёты ({home.parent.totals.reports})</Button>
   </Card>
   <Card><h2>Доступные события</h2><p>Ближайшие занятия и сроки активных заданий.</p>
    {home.parent.events.length ? home.parent.events.map(e => <article key={`${e.kind}-${e.id}`}><h3>{e.title}</h3><EventContent event={e} /></article>) : <p>Предстоящих учебных событий пока нет.</p>}
    <Button variant="secondary" onClick={() => onSection('events')}>Все события ({home.parent.totals.events})</Button>
   </Card>
   <Card><h2>Мои обращения</h2>
    {home.parent.requests.length ? home.parent.requests.map(r => <article key={r.id}><h3>{r.subject}</h3><p style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{r.body}</p><p style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>Ответ: {r.reply || 'Ожидается'}</p><a href={`#/my-requests/${r.id}`}>Открыть обращение №{r.id}</a></article>) : <p>Вы ещё не отправляли обращений по этому ребёнку.</p>}
    <Button variant="secondary" onClick={() => onSection('requests')}>Обращения и новый вопрос ({home.parent.totals.requests})</Button>
   </Card>
  </>}
  {reportID !== null && <PublishedReport id={reportID} onClose={() => setReportID(null)} />}
 </div>;
}
