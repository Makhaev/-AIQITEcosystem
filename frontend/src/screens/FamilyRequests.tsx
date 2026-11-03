import { useEffect, useRef, useState, type FormEvent } from 'react';
import { APIError, message, request } from '../api';
import { navigateWithNotice, useNavigationNotice } from '../navigation';
import { Button, Card, Input, Notice, PageHeading, Pagination, Select } from '../components/ui';

type Attachment = { id: number; name: string; url: string };
type Message = { id: number; author_name: string; body: string; kind: string; created_at: string | null; files: Attachment[] };
type Event = { action: string; from_status: string; to_status: string; actor_name: string; assignee_id?: number; reason: string; created_at: string | null };
export type RequestCard = { id: number; student_id: number | null; student_name: string; author_name: string; assignee_id: number | null; assignee_name: string; subject: string; body: string; reply: string; revision: number; status: string; created_at: string; answered_at: string | null; closed_at: string | null; actions: string[]; messages?: Message[]; events?: Event[] };
const states: Record<string, string> = { new: 'Новое', assigned: 'Назначено', in_progress: 'В работе', answered: 'Дан ответ', closed: 'Закрыто' };
const labels: Record<string, string> = { create: 'Создано', assign: 'Назначен ответственный', start: 'Взято в работу', message: 'Сообщение', answer: 'Дан ответ', close: 'Закрыто', reopen: 'Открыто повторно', legacy_answer: 'Прежний ответ' };
const date = (v?: string | null) => v ? new Date(v).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow' }) : 'Не зарегистрирована';
async function uploads(files: File[]) {
 if (files.length > 3 || files.reduce((n, f) => n + f.size, 0) > 10 * 1024 * 1024 || files.some(f => !f.size)) throw new Error('До трёх непустых файлов, суммарно до 10 МиБ.');
 return Promise.all(files.map(async f => ({ name: f.name, data: await new Promise<string>((resolve, reject) => { const r = new FileReader(); r.onload = () => resolve(String(r.result).split(',')[1]); r.onerror = () => reject(new Error('Не удалось прочитать файл.')); r.readAsDataURL(f); }) })));
}
function errorText(e: unknown) { return e instanceof APIError ? message(e) : e instanceof Error ? e.message : message(e); }
function UploadInput({ onChange, disabled }: { onChange: (f: File[]) => void; disabled: boolean }) { return <Input label="Вложения — до 3 файлов, всего до 10 МиБ" type="file" multiple disabled={disabled} onChange={e => onChange(Array.from(e.target.files || []))} />; }

export function FamilyRequests({ staff = false, id, sid }: { staff?: boolean; id?: string; sid?: string }) {
 const root = staff ? '/family-requests' : '/family/requests', href = staff ? '#/family-requests' : '#/my-requests';
 const [data, setData] = useState<{ items: RequestCard[]; total: number } | null>(null), [error, setError] = useState(''), [page, setPage] = useState(1), [status, setStatus] = useState(''), [search, setSearch] = useState(''), [reload, setReload] = useState(0);
 useEffect(() => { if (id) return; const c = new AbortController(); setData(null); setError(''); const base = sid ? (staff ? `/students/${sid}/family-requests` : `/family/students/${sid}/requests`) : root;
  request<{ items: RequestCard[]; total: number }>(`${base}?page=${page}&status=${status}&search=${encodeURIComponent(search)}`, 'GET', undefined, c.signal).then(v => { if (!c.signal.aborted) setData(v); }).catch(e => { if (!c.signal.aborted) setError(message(e)); }); return () => c.abort();
 }, [id, sid, root, page, status, search, reload, staff]);
 if (id) return <RequestDetails key={`${root}/${id}`} id={id} root={root} href={href} />;
 return <div className="stack">{!sid && <PageHeading title={staff ? 'Обращения семьи' : 'Мои обращения'} description="Переписка, вложения, ответственный и история работы с обращением." />}
  <Card className="stack"><form className="filters" onSubmit={e => { e.preventDefault(); setPage(1); setSearch(String(new FormData(e.currentTarget).get('search') || '')); }}><Input label="Поиск обращения" name="search" maxLength={100} /><Button type="submit">Найти обращение</Button><Select label="Статус обращения" value={status} onChange={e => { setStatus(e.target.value); setPage(1); }}><option value="">Все статусы</option>{Object.entries(states).map(([s, n]) => <option key={s} value={s}>{n}</option>)}</Select><Button variant="secondary" onClick={() => setReload(v => v + 1)}>Обновить обращения</Button></form></Card>
  <Notice tone="error">{error}</Notice>{!data && !error && <p role="status">Загрузка…</p>}
  {data?.items.map(q => <Card key={q.id}><h2>{q.subject}</h2><p>{states[q.status] || q.status} · {q.student_name || 'Без привязки к ребёнку'}</p><p>Автор: {q.author_name} · Ответственный: {q.assignee_name || 'Не назначен'}</p><a href={`${href}/${q.id}`}>Открыть обращение №{q.id}</a></Card>)}
  {data && <>{!data.items.length && <Card className="stack"><p>Нет доступных обращений.</p></Card>}<Pagination page={page} total={data.total} pageSize={20} onChange={setPage} /></>}
  {!staff && !error && <NewRequest sid={sid} onCreated={q => { navigateWithNotice(`${href}/${q.id}`, 'Обращение отправлено.'); }} />}
 </div>;
}
function NewRequest({ sid, onCreated }: { sid?: string; onCreated: (q: RequestCard) => void }) {
 const lifetime = useRef(new AbortController());
 useEffect(() => () => lifetime.current.abort(), []);
 const [busy, setBusy] = useState(false), [error, setError] = useState(''), [files, setFiles] = useState<File[]>([]), [child, setChild] = useState(sid || ''), [children, setChildren] = useState<{ items: { id: number; name: string }[]; total: number } | null>(null), [childPage, setChildPage] = useState(1), [query, setQuery] = useState(''), [childReload, setChildReload] = useState(0), [childError, setChildError] = useState('');
 useEffect(() => { if (sid) return; const c = new AbortController(); setChildren(null); setChildError(''); request<any>(`/family/students?page=${childPage}&search=${encodeURIComponent(query)}`, 'GET', undefined, c.signal).then(v => { if (!c.signal.aborted) setChildren(v); }).catch(e => { if (!c.signal.aborted) setChildError(message(e)); }); return () => c.abort(); }, [sid, childPage, query, childReload]);
 async function submit(e: FormEvent<HTMLFormElement>) { e.preventDefault(); const d = new FormData(e.currentTarget); setBusy(true); setError(''); try { const attachments = await uploads(files); lifetime.current.signal.throwIfAborted(); const v = await request<RequestCard>(sid ? `/family/students/${sid}/requests` : '/family/requests', 'POST', { subject: d.get('subject'), body: d.get('body'), student_id: child ? Number(child) : null, files: attachments }, lifetime.current.signal); if (!lifetime.current.signal.aborted) onCreated(v); } catch (e) { if (!lifetime.current.signal.aborted) setError(errorText(e)); } finally { if (!lifetime.current.signal.aborted) setBusy(false); } }
 return <Card className="stack"><h2>Новое обращение</h2><Notice tone="error">{error}</Notice>{!sid && <><Input label="Поиск ребёнка для обращения" value={query} onChange={e => { setQuery(e.target.value); setChildPage(1); setChild(''); }} /><Notice tone="error">{childError}</Notice>{!children && !childError && <p role="status">Загрузка детей…</p>}{childError && <Button variant="secondary" onClick={() => setChildReload(v => v + 1)}>Повторить загрузку детей</Button>}{children?.total === 0 && <p>Нет доступных детей. Можно отправить обращение без привязки.</p>}<Select label="Ребёнок (необязательно)" disabled={busy || !children} value={child} onChange={e => setChild(e.target.value)}><option value="">Без привязки к ребёнку</option>{children?.items.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}</Select>{children && <Pagination page={childPage} total={children.total} pageSize={20} onChange={p => { setChildPage(p); setChild(''); }} />}</>}
  <form className="stack" onSubmit={submit}><Input label="Тема" name="subject" maxLength={200} required disabled={busy} /><label>Текст обращения<textarea className="input" name="body" maxLength={5000} required disabled={busy} /></label><UploadInput onChange={setFiles} disabled={busy} /><Button type="submit" loading={busy}>Отправить обращение</Button></form>
 </Card>;
}
function RequestDetails({ id, root, href }: { id: string; root: string; href: string }) {
 const [q, setQ] = useState<RequestCard | null>(null), [error, setError] = useState(''), [reload, setReload] = useState(0), [busy, setBusy] = useState(false), [body, setBody] = useState(''), [reason, setReason] = useState(''), [assignee, setAssignee] = useState(''), [files, setFiles] = useState<File[]>([]), [fileKey, setFileKey] = useState(0);
 const [notice, setNotice] = useNavigationNotice();
 useEffect(() => { const c = new AbortController(); setQ(null); setError(''); request<RequestCard>(`${root}/${id}`, 'GET', undefined, c.signal).then(v => { if (!c.signal.aborted) setQ(v); }).catch(e => { if (!c.signal.aborted) setError(message(e)); }); return () => c.abort(); }, [root, id, reload]);
 async function act(action: string) {
  if (!q) return; if (action === 'reopen' && !reason.trim()) { setError('Укажите причину повторного открытия.'); return; }
  if ((action === 'message' || action === 'answer') && !body.trim()) { setError('Введите сообщение.'); return; }
  setBusy(true); setError(''); setNotice('');
  try { const writing = action === 'message' || action === 'answer'; const v = await request<RequestCard>(`${root}/${id}/actions`, 'POST', { action, revision: q.revision, ...(action === 'assign' ? { assignee_id: Number(assignee) } : {}), reason, ...(writing ? { body, files: await uploads(files) } : {}) }); setQ(v); setNotice(writing ? 'Сообщение отправлено.' : 'Изменения обращения сохранены.'); setReason(''); if (writing) { setBody(''); setFiles([]); setFileKey(v => v + 1); } }
  catch (e) { setError(errorText(e)); if (e instanceof APIError && (e.status === 403 || e.status === 404)) { setQ(null); setBody(''); setFiles([]); } } finally { setBusy(false); }
 }
 return <div className="stack"><PageHeading title={q ? q.subject : 'Обращение'} /><a href={href}>К списку обращений</a><Button variant="secondary" disabled={busy} onClick={() => setReload(v => v + 1)}>Обновить обращение</Button><Notice tone="error">{error}</Notice><Notice tone="success">{notice}</Notice>{busy && <p role="status">Сохраняем изменения…</p>}{!q && !error && <p role="status">Загрузка…</p>}
 {q && <><Card className="stack"><h2>{states[q.status]}</h2><p>Автор: {q.author_name}. Ребёнок: {q.student_name || 'Не указан'}.</p><p>Ответственный: {q.assignee_name || 'Не назначен'}.</p><p>Создано: {date(q.created_at)}. Последний ответ: {date(q.answered_at)}. Закрыто: {date(q.closed_at)}. Время по Москве.</p>
  {q.actions.includes('assign') && <div className="row-actions"><Input label="ID ответственного" type="number" min={1} value={assignee} onChange={e => setAssignee(e.target.value)} disabled={busy} /><Button disabled={busy || !assignee} onClick={() => void act('assign')}>Назначить ответственного</Button></div>}
  {q.actions.includes('start') && <Button disabled={busy} onClick={() => void act('start')}>Взять в работу</Button>}
  {q.actions.includes('close') && <Button disabled={busy} onClick={() => void act('close')}>Закрыть обращение</Button>}
  {q.actions.includes('reopen') && <><Input label="Причина повторного открытия" value={reason} maxLength={5000} disabled={busy} onChange={e => setReason(e.target.value)} /><Button disabled={busy} onClick={() => void act('reopen')}>Открыть повторно</Button></>}
 </Card><Card className="stack"><h2>История сообщений</h2>{!q.messages?.length && <p>Сообщений пока нет.</p>}{q.messages?.map(m => <article key={m.id} className="stack"><h3>{m.kind === 'answer' ? 'Ответ' : 'Сообщение'} · {m.author_name || 'Автор прежнего ответа не зарегистрирован'}</h3><p>{date(m.created_at)}{m.created_at && ' (Москва)'}</p><p style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{m.body}</p>{m.files.length > 0 && <ul>{m.files.map(f => <li key={f.id}><a href={f.url} download>{f.name}</a></li>)}</ul>}</article>)}</Card>
 {(q.actions.includes('message') || q.actions.includes('answer')) && <Card className="stack"><label>Новое сообщение<textarea className="input" value={body} onChange={e => setBody(e.target.value)} maxLength={5000} disabled={busy} /></label><UploadInput key={fileKey} onChange={setFiles} disabled={busy} /><div className="row-actions">{q.actions.includes('message') && <Button disabled={busy} onClick={() => void act('message')}>Добавить сообщение</Button>}{q.actions.includes('answer') && <Button disabled={busy} onClick={() => void act('answer')}>Ответить и отметить ответ</Button>}</div></Card>}
 <Card className="stack"><h2>История переходов</h2>{!q.events?.length && <p>Переходов пока нет.</p>}<ul>{q.events?.map((e, i) => <li key={i}>{labels[e.action] || e.action}: {states[e.from_status] || e.from_status} → {states[e.to_status]} · {e.actor_name || 'Автор не зарегистрирован'} · {date(e.created_at)}{e.assignee_id && ` · Ответственный №${e.assignee_id}`}{e.reason && <p style={{ whiteSpace: 'pre-wrap' }}>{e.reason}</p>}</li>)}</ul></Card></>}
 </div>;
}
