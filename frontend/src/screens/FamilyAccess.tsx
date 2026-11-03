import { FamilyReportPanel, reportNotices } from './FamilyReportWorkflow';
import { FamilyReportEditor } from './FamilyReportEditor';
import { useEffect, useState, type FormEvent } from 'react';
import { message, request } from '../api';
import { Button, Card, Input, Notice, Pagination, Select } from '../components/ui';
import type { FamilyReport, FamilyRequest } from './FamilyPortal';
type Page<T> = { items: T[]; total: number };
type Link = { parent_id: number; name: string; confirmed: boolean };

export function FamilyAccess({ sid, admin }: { sid: string; admin: boolean }) {
 const [open, setOpen] = useState(false);
 return <Card><h2>Доступ семьи и отчёты</h2><Button variant="secondary" onClick={() => setOpen(v => !v)}>{open ? 'Свернуть' : 'Открыть управление'}</Button>{open && <FamilyManagement key={sid} sid={sid} admin={admin} />}</Card>;
}
function FamilyManagement({ sid, admin }: { sid: string; admin: boolean }) {
 const [links, setLinks] = useState<Link[]>([]); const [reports, setReports] = useState<Page<FamilyReport> | null>(null); const [requests, setRequests] = useState<Page<FamilyRequest> | null>(null); const [reportPage, setReportPage] = useState(1); const [requestPage, setRequestPage] = useState(1); const [version, setVersion] = useState(0); const [error, setError] = useState(''); const [success, setSuccess] = useState(''); const [busy, setBusy] = useState(false); const [editing, setEditing] = useState<FamilyReport | null>(null);
 useEffect(() => { const c = new AbortController(); setError(''); setReports(null); setRequests(null); setLinks([]);
 Promise.all([admin ? request<Link[]>(`/students/${sid}/family-access`, 'GET', undefined, c.signal) : Promise.resolve([]), request<Page<FamilyReport>>(`/students/${sid}/family-reports?page=${reportPage}`, 'GET', undefined, c.signal), request<Page<FamilyRequest>>(`/students/${sid}/family-requests?page=${requestPage}`, 'GET', undefined, c.signal)]).then(([l, r, q]) => { if (!c.signal.aborted) { setLinks(l); setReports(r); setRequests(q); } }).catch(e => { if (!c.signal.aborted) setError(message(e)); }); return () => c.abort(); }, [sid, admin, reportPage, requestPage, version]);
 async function save(path: string, method: string, body: unknown) { setBusy(true); setError(''); setSuccess(''); try { await request(path, method, body); setVersion(v => v + 1); setSuccess('Сохранено. Права проверяются при следующих запросах.'); return true; } catch (e) { setError(message(e)); return false; } finally { setBusy(false); } }
 async function share(e: FormEvent<HTMLFormElement>) { e.preventDefault(); const d = new FormData(e.currentTarget); const kind = d.get('kind'); await save(`/${kind}/${d.get('id')}/family-access`, 'PUT', { revision: Number(d.get('revision')), students: d.get('students') === 'on', parents: d.get('parents') === 'on' }); }
 return <div className="stack"><Notice tone="error">{error}</Notice><Notice tone="success">{success}</Notice><Button variant="secondary" onClick={() => setVersion(v => v + 1)} disabled={busy}>Обновить управление</Button>
 {admin && <section><h3>Подтверждённые семейные связи</h3><p>Контактная связь сама по себе не открывает данные ребёнка. Подтверждайте доступ после проверки представительства.</p>{links.map(l => <div key={l.parent_id} className="row-actions"><span>{l.name} — {l.confirmed ? 'Доступ подтверждён' : 'Нет доступа'}</span><Button variant="secondary" disabled={busy} onClick={() => void save(`/students/${sid}/parents/${l.parent_id}/access`, 'PUT', { confirmed: !l.confirmed })}>{l.confirmed ? 'Отозвать доступ' : 'Подтвердить доступ'}</Button></div>)}
 <h3>Разрешить обратную связь или файл</h3><p>Для наблюдения нужна текущая проверенная опубликованная редакция. Для файла — отдельное право управления; вложение должно быть связано с доступной семье записью или отчётом.</p>
 <form onSubmit={share} className="stack"><Select label="Объект" name="kind"><option value="learning">Наблюдение</option><option value="files">Файл</option></Select><Input label="ID объекта" name="id" type="number" min={1} required /><Input label="Редакция наблюдения" name="revision" type="number" min={1} /><label><input type="checkbox" name="students" /> Ученику</label><label><input type="checkbox" name="parents" /> Подтверждённым родителям</label><Button type="submit" loading={busy}>Сохранить разрешения</Button></form></section>}
 <section><h3>Отчёты семье</h3><p>Черновик проходит проверку и утверждение перед публикацией. Исправление создаётся отдельной версией; прежний опубликованный отчёт остаётся доступен семье до замены или отзыва.</p>
 {reports?.items.map(r => <FamilyReportPanel key={r.id} report={r} onEdit={setEditing} onChange={(next, action) => { setSuccess(reportNotices[action]); setEditing(action === "correct" ? next : null); setVersion(v => v + 1); }} />)}
 {reports && <Pagination page={reportPage} total={reports.total} pageSize={20} onChange={setReportPage} disabled={busy} />}
 <FamilyReportEditor key={editing ? `${editing.id}/${editing.revision}` : `new/${version}`} sid={sid} editing={editing} onSaved={r => { setEditing(r); setVersion(v => v + 1); setSuccess('Черновик сохранён. Проверьте состав и текст перед публикацией.'); }} onCancel={() => setEditing(null)} /></section>
 <section><h3>Обращения семьи</h3>{requests?.items.map(q => <article key={q.id}><h4>{q.subject}</h4><p style={{ whiteSpace: 'pre-wrap' }}>{q.body}</p><a href={`#/family-requests/${q.id}`}>Открыть обращение №{q.id}</a></article>)}{requests && <Pagination page={requestPage} total={requests.total} pageSize={20} onChange={setRequestPage} disabled={busy} />}</section>
 </div>;
}
