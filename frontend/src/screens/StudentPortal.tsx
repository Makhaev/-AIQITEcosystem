import { MAX_FILE_BYTES } from '../upload';
import { useEffect, useState, type FormEvent } from 'react';
import { message, request, APIError } from '../api';
import { Button, Card, Input, Notice, Pagination, SectionHeading } from '../components/ui';

type FileLink = { id: number; name: string; url: string };
export type StudyRecord = {
 id: number; student_id: number; kind: string; state: string; revision: number; created_at: string;
 content: { [key: string]: string | boolean }; files: FileLink[];
 can_submit?: boolean; submission_issue?: string; feedback?: string;
 parent?: { id: number; title: string }; sources: { id: number; title: string }[];
};
type Page<T> = { items: T[]; total: number; page: number; page_size: number };
type Home = {
 next_lesson: { topic: string; starts_at: string; ends_at: string } | null;
 programs: { title: string; version: number; percent: number | null; state: string }[];
 blocks: { [kind: string]: StudyRecord[] }; totals: { [kind: string]: number };
};
const labels: { [key: string]: string } = { title: 'Название', instruction: 'Инструкция', criteria: 'Критерии проверки', due_on: 'Срок', format: 'Формат ответа', allow_late: 'Поздняя сдача', answer: 'Ответ', fact: 'Обратная связь', occurred_on: 'Дата', skill_name: 'Навык', starts_on: 'Начало участия', ends_on: 'Окончание участия', contribution: 'Мой вклад' };
const statuses: { [key: string]: string } = { planned: 'Запланировано', active: 'В работе', achieved: 'Достигнуто', cancelled: 'Отменено', revised: 'Пересмотрено', assigned: 'Назначено', completed: 'Выполнено', submitted: 'На проверке', accepted: 'Принято', revision: 'На доработке', reviewed: 'Проверено' };
const formats: { [key: string]: string } = { text: 'Текст', link: 'Ссылка HTTP(S)', file: 'Приватный файл' };
const date = (value: string) => new Date(value).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow' });
export function StudyContent({ record: r }: { record: StudyRecord }) {
 return <><p>{statuses[r.state] || r.state}</p>{Object.entries(r.content).filter(([k]) => k !== 'title' && labels[k]).map(([k,v]) => <p key={k} style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}><strong>{labels[k]}: </strong>{typeof v === 'boolean' ? v ? 'Да' : 'Нет' : k === 'format' ? formats[v] || v : v}</p>)}
 {r.created_at && r.kind === 'attempts' && <p>Отправлено: {date(r.created_at)} (Москва) · Попытка №{r.id}</p>}
 {r.feedback && <p style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}><strong>Замечания педагога: </strong>{r.feedback}</p>}
 <ul>{r.files.map(f => <li key={f.id}><a href={f.url} download>{f.name}</a></li>)}</ul>
 {r.sources.map(s => <p key={s.id}>Основание: {s.title}</p>)}</>;
}
export function StudentHome({ sid, onSection, onAssignment }: { sid: string; onSection: (kind: string) => void; onAssignment: (id: number) => void }) {
 const [home,setHome]=useState<Home|null>(null); const [error,setError]=useState(''); const [version,setVersion]=useState(0);
 useEffect(()=>{const c=new AbortController();setHome(null);setError('');request<Home>(`/family/students/${sid}/home`,'GET',undefined,c.signal).then(v=>{if(!c.signal.aborted)setHome(v)}).catch(e=>{if(!c.signal.aborted)setError(message(e))});return()=>c.abort()},[sid,version]);
 return <div className="stack"><div><Button variant="secondary" onClick={()=>setVersion(v=>v+1)}>Обновить главную</Button></div><Notice tone="error">{error}</Notice>{!home&&!error&&<p role="status">Загрузка…</p>}{home&&<>
 <Card><h2>Ближайшее занятие</h2>{home.next_lesson ? <><h3>{home.next_lesson.topic}</h3><p>{date(home.next_lesson.starts_at)} — {date(home.next_lesson.ends_at)} (Москва)</p></>:<p>Ближайших занятий пока нет.</p>}<Button variant="secondary" onClick={()=>onSection('lessons')}>Все занятия</Button></Card>
 <Card><h2>Прогресс программы</h2>{home.programs.length ? home.programs.map((p,i)=><div key={i}><h3>{p.title}{p.version ? ` · версия ${p.version}` : ''}</h3>{p.percent===null ? <p>Нет данных для расчёта.</p> : <><progress aria-label={`Прогресс: ${p.title}`} value={p.percent} max={100}/><p>{p.percent.toLocaleString('ru-RU',{maximumFractionDigits:1})}%</p></>}</div>):<p>Нет действующей учебной программы.</p>}<p>Выполнение программы группы по подтверждённым занятиям. Личные цели и работы показаны отдельно.</p></Card>
 {Object.entries({assignments:'Активные задания и сроки',goals:'Индивидуальные цели',attempts:'Мои работы',projects:'Мои проекты',observations:'Опубликованная обратная связь'}).map(([kind,title])=><Card key={kind}><h2>{title}</h2>{home.blocks[kind]?.length ? home.blocks[kind].map(r=><article key={r.id}><h3>{r.content.title}</h3><StudyContent record={r}/>{kind==='assignments'&&<Button onClick={()=>onAssignment(r.id)}>Открыть задание</Button>}</article>):<p>Пока нет записей.</p>}<Button variant="secondary" onClick={()=>onSection(kind)}>Открыть раздел ({home.totals[kind] || 0})</Button></Card>)}
 </>}</div>;
}

async function encodeFile(file: File): Promise<string> {
 return new Promise((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(String(reader.result).split(',')[1]);reader.onerror=()=>reject(new Error('read'));reader.readAsDataURL(file)});
}
export function StudentAssignment({ id, sid, onBack }: { id: number; sid: string; onBack: ()=>void }) {
 const [assignment,setAssignment]=useState<StudyRecord|null>(null);const [attempts,setAttempts]=useState<Page<StudyRecord>|null>(null);
 const [page,setPage]=useState(1);const [reload,setReload]=useState(0);const [error,setError]=useState('');const [success,setSuccess]=useState('');const [busy,setBusy]=useState(false);const [answer,setAnswer]=useState('');
 useEffect(()=>{const c=new AbortController();setAssignment(null);setAttempts(null);setError('');Promise.all([request<StudyRecord>(`/family/learning/${id}`,'GET',undefined,c.signal),request<Page<StudyRecord>>(`/family/students/${sid}/learning?kind=attempts&assignment_id=${id}&page=${page}`,'GET',undefined,c.signal)]).then(([a,b])=>{if(!c.signal.aborted){setAssignment(a);setAttempts(b)}}).catch(e=>{if(!c.signal.aborted)setError(message(e))});return()=>c.abort()},[id,sid,page,reload]);
 async function send(e:FormEvent<HTMLFormElement>){e.preventDefault();if(!assignment||busy)return;const form=e.currentTarget;setBusy(true);setError('');setSuccess('');try{
  const selected=new FormData(form).get('file');let file;
  if(assignment.content.format==='file'){
   if(!(selected instanceof File)||!selected.size||selected.size>MAX_FILE_BYTES){setError('Выберите файл размером от 1 байта до 10 МиБ.');return}
   file={name:selected.name,data:await encodeFile(selected)};
  }
  const result=await request<{id:number}>(`/family/assignments/${id}/attempts`,'POST',{revision:assignment.revision,answer:assignment.content.format==='file'?'':answer,...(file?{file}:{})});
  setAnswer('');form.reset();setPage(1);setReload(v=>v+1);setSuccess(`Попытка №${result.id} отправлена на проверку.`);
 }catch(e){setError(message(e));if(e instanceof APIError && [401,403,404,409].includes(e.status)){setAssignment(null);setAttempts(null)}}finally{setBusy(false)}}
 return <div className="stack"><div><Button variant="secondary" disabled={busy} onClick={onBack}>Назад к обучению</Button> <Button variant="secondary" disabled={busy} onClick={()=>setReload(v=>v+1)}>Обновить задание</Button></div><Notice tone="error">{error}</Notice><Notice tone="success">{success}</Notice>{!assignment&&!error&&<p role="status">Загрузка…</p>}{assignment&&<Card><SectionHeading>{assignment.content.title}</SectionHeading><StudyContent record={assignment}/>{assignment.can_submit ? <form onSubmit={send} className="stack"><h3>Отправить работу</h3>{assignment.content.format==='file' ? <Input label="Файл работы (до 10 МиБ)" name="file" type="file" required disabled={busy}/> : assignment.content.format==='link' ? <Input label="Ссылка на работу" name="answer" type="url" required maxLength={10000} value={answer} onChange={e=>setAnswer(e.target.value)} disabled={busy}/> : <label>Текст ответа<textarea className="input" name="answer" required maxLength={10000} value={answer} onChange={e=>setAnswer(e.target.value)} disabled={busy}/></label>}<p>Каждая отправка создаёт новую попытку. Черновик на сервере не сохраняется.</p><Button type="submit" loading={busy}>Отправить попытку</Button></form>:<p>{assignment.submission_issue||'Отправка работы недоступна.'}</p>}</Card>}
 {attempts&&<Card><h2>История попыток</h2>{attempts.items.map(r=><article key={r.id}><h3>{r.content.title}</h3><StudyContent record={r}/></article>)}{!attempts.items.length&&<p>Работа ещё не отправлена.</p>}<Pagination page={page} total={attempts.total} pageSize={20} disabled={busy} onChange={setPage}/></Card>}</div>;
}
