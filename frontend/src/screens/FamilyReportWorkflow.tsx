import { useEffect, useState } from 'react';
import { message, request } from '../api';
import { Button, Card, Input, Notice, PageHeading, Pagination } from '../components/ui';
import type { FamilyReport } from './FamilyPortal';
import { FamilyReportContent } from './FamilyReportContent';
import { FamilyReportEditor } from './FamilyReportEditor';

const states: Record<string, string> = { draft: 'Черновик', review: 'На проверке', approved: 'Утверждён', published: 'Опубликован', withdrawn: 'Отозван' };
const labels: Record<string, string> = { submit: 'Отправить на проверку', return: 'Вернуть на доработку', approve: 'Утвердить', publish: 'Опубликовать семье', withdraw: 'Отозвать публикацию', correct: 'Создать исправленную версию' };
export const reportNotices: Record<string, string> = { submit: 'Отчёт отправлен на проверку.', return: 'Отчёт возвращён на доработку.', approve: 'Отчёт утверждён.', publish: 'Отчёт опубликован семье.', withdraw: 'Публикация отозвана.', correct: 'Создана исправленная версия отчёта.' };
export function FamilyReportPanel({ report: r, onChange, onEdit }: { report: FamilyReport; onChange: (r: FamilyReport, action: string) => void; onEdit: (r: FamilyReport) => void }) {
 const [busy, setBusy] = useState(false), [error, setError] = useState(''), [reason, setReason] = useState('');
 const actions = r.actions || [];
 async function act(action: string) {
  if ((action === 'return' || action === 'withdraw') && !reason.trim()) { setError('Укажите замечание или причину.'); return; }
  setBusy(true); setError('');
  try {
   const next = await request<FamilyReport>(`/family-reports/${r.id}/actions`, 'POST', { action, version: action === 'withdraw' ? r.published_version : r.version, revision: r.revision, reason });
   setReason(''); onChange(next, action);
  } catch (e) { setError(message(e)); } finally { setBusy(false); }
 }
 return <article className="stack"><h3>{r.title} · {states[r.state || 'draft'] || r.state}</h3>
  {r.published_version && <p>Семье доступна версия {r.published_version}.</p>}
  <FamilyReportContent report={r} />
  {r.files.length > 0 && <ul>{r.files.filter(f => f.url).map(f => <li key={f.id}><a href={f.url} download>{f.name}</a></li>)}</ul>}
  <Notice tone="error">{error}</Notice>
  {actions.some(a => a === 'return' || a === 'withdraw') && <Input label={`Замечание или причина — отчёт №${r.id}`} value={reason} onChange={e => setReason(e.target.value)} maxLength={5000} disabled={busy} />}
  <div className="row-actions">{actions.includes('edit') && <Button variant="secondary" disabled={busy} onClick={() => onEdit(r)}>Изменить отчёт</Button>}
   {actions.filter(a => a !== 'edit' && labels[a]).map(a => <Button key={a} variant="secondary" disabled={busy} onClick={() => void act(a)}>{labels[a]}</Button>)}
  </div>
  {!!r.decisions?.length && <details><summary>История переходов и замечания</summary><ul>{r.decisions.map((d, i) => <li key={i}>Версия {d.version} · {labels[d.action] || d.action} · {d.actor_name} · {new Date(d.created_at).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow' })}{d.reason && <p style={{ whiteSpace: 'pre-wrap' }}>{d.reason}</p>}</li>)}</ul></details>}
 </article>;
}
export function FamilyReports({ id }: { id?: string }) {
 const [data, setData] = useState<{ items: FamilyReport[]; total: number } | null>(null), [error, setError] = useState(''), [notice, setNotice] = useState('');
 const [page, setPage] = useState(1), [reload, setReload] = useState(0), [editing, setEditing] = useState<FamilyReport | null>(null);
 useEffect(() => { const c = new AbortController(); setData(null); setError('');
  request<any>(id ? `/family-reports/${id}` : `/family-reports?page=${page}`, 'GET', undefined, c.signal).then(v => { if (!c.signal.aborted) setData(id ? { items: [v], total: 1 } : v); }).catch(e => { if (!c.signal.aborted) setError(message(e)); });
  return () => c.abort();
 }, [id, page, reload]);
 return <><PageHeading title="Отчёты семье" description="Подготовка, проверка, утверждение и публикация версий отчётов." />
  <Notice tone="error">{error}</Notice><Notice tone="success">{notice}</Notice><Button variant="secondary" onClick={() => { setEditing(null); setReload(v => v + 1); }}>Обновить отчёты</Button>
  {!data && !error && <p role="status">Загрузка…</p>}
  {data?.items.map(r => <Card key={r.id}><p>Ученик: {r.snapshot?.student_name || `№${r.student_id}`}</p><FamilyReportPanel report={r} onEdit={setEditing} onChange={(next, action) => { setNotice(reportNotices[action]); setEditing(action === 'correct' ? next : null); setReload(v => v + 1); }} /></Card>)}
  {data && !data.items.length && <Card><p>Нет доступных отчётов. Руководитель образования видит только назначенные ему версии, переданные на проверку, и свои действующие публикации.</p></Card>}
  {data && !id && <Pagination page={page} pageSize={20} total={data.total} onChange={p => { setEditing(null); setPage(p); }} />}
  {editing && !error && <Card><FamilyReportEditor key={`${editing.id}/${editing.revision}`} sid={String(editing.student_id)} editing={editing} onSaved={r => { setNotice('Черновик отчёта сохранён.'); setEditing(r); setReload(v => v + 1); }} onCancel={() => setEditing(null)} /></Card>}
 </>;
}
