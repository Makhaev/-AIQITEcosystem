import { useEffect, useState, type FormEvent } from 'react';
import { message, request } from '../api';
import { Button, EmptyState, Input, Modal, Notice, Pagination } from '../components/ui';

type Lesson = { id: number; group_id: number; starts_at: string; ends_at: string; topic: string; program_name?: string; module_id: number | null; module_name: string | null; version_module_key?: string };
type Enrollment = { id: number; student_id: number; student_name: string; starts_on: string; ends_on: string | null };
type Version = { id: number; program_name: string | null; module_name: string | null; topic: string | null; state: string; preserved_at: string; reason: string | null };
type Page<T> = { items: T[]; total: number; page: number; page_size: number };

function localTime(value: string) {
  const parts = new Intl.DateTimeFormat('sv-SE', { timeZone: 'Europe/Moscow', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).formatToParts(new Date(value));
  const get = (key: string) => parts.find(p => p.type === key)!.value;
  return `${get('year')}-${get('month')}-${get('day')}T${get('hour')}:${get('minute')}`;
}
function toISO(value: string) {
  const offset = new Intl.DateTimeFormat('en', { timeZone: 'Europe/Moscow', timeZoneName: 'longOffset' }).formatToParts(new Date(`${value}Z`)).find(p => p.type === 'timeZoneName')!.value.replace('GMT', '') || '+00:00';
  return new Date(`${value}${offset}`).toISOString();
}

export function RescheduleEditor({ lesson, close, saved }: { lesson: Lesson; close: () => void; saved: () => void }) {
  const [start, setStart] = useState(localTime(lesson.starts_at)); const [end, setEnd] = useState(localTime(lesson.ends_at));
  const [enrollments, setEnrollments] = useState<Enrollment[]>([]); const [selected, setSelected] = useState<number[]>([]);
  const [confirmed, setConfirmed] = useState(false);
  const [loading, setLoading] = useState(true); const [lookupError, setLookupError] = useState(''); const [version, setVersion] = useState(0); const [error, setError] = useState(''); const [busy, setBusy] = useState(false);
  useEffect(() => { const controller = new AbortController(); setLoading(true); setLookupError(''); request<{ enrollments: Enrollment[] }>(`/groups/${lesson.group_id}`, 'GET', undefined, controller.signal).then(v => { if (!controller.signal.aborted) setEnrollments(v.enrollments); }).catch(e => { if (!controller.signal.aborted) setLookupError(message(e)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); }); return () => controller.abort(); }, [lesson.group_id, version]);
  const day = start.slice(0, 10);
  const pupils = new Map<number, Enrollment>();
  [...enrollments].sort((a, b) => b.starts_on.localeCompare(a.starts_on) || b.id - a.id).forEach(e => { if (e.starts_on <= day && (!e.ends_on || e.ends_on >= day) && !pupils.has(e.student_id)) pupils.set(e.student_id, e); });
  const choices = [...pupils.values()]; const choiceKey = choices.map(e => e.id).join(',');
  useEffect(() => { setSelected(choices.map(e => e.id)); setConfirmed(false); }, [day, choiceKey]);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setError('');
    if (end <= start) { setError('Окончание должно быть позже начала.'); return; }
    if (!confirmed || loading || lookupError) return;
    setBusy(true);
    try { await request(`/lessons/${lesson.id}/reschedule`, 'POST', { starts_at: toISO(start), ends_at: toISO(end), enrollment_ids: selected, reason: new FormData(event.currentTarget).get('reason') }); saved(); }
    catch (e) { setError(message(e)); } finally { setBusy(false); }
  }
  return <Modal title="Перенести занятие и обновить состав" description="Прежние отметки сохранятся. Исключённые участники останутся в истории; новые получат «Не отмечен»." onClose={close} busy={busy}><Notice tone="error">{error || lookupError}</Notice>{lookupError && <Button variant="secondary" onClick={() => setVersion(v => v + 1)}>Повторить загрузку состава</Button>}<form onSubmit={submit}>
    <Input label="Новое начало (МСК)" type="datetime-local" value={start} onChange={e => { setStart(e.target.value); setConfirmed(false); }} required disabled={busy} />
    <Input label="Новое окончание (МСК)" type="datetime-local" value={end} onChange={e => { setEnd(e.target.value); setConfirmed(false); }} required disabled={busy} />
    {loading ? <p role="status">Загружаем состав…</p> : <fieldset disabled={busy || !!lookupError}><legend>Состав на новую дату: {day}</legend>{choices.map(e => <label className="checkbox" key={e.id}><input type="checkbox" checked={selected.includes(e.id)} onChange={event => { setSelected(values => event.target.checked ? [...values, e.id] : values.filter(v => v !== e.id)); setConfirmed(false); }} />{e.student_name}</label>)}{!choices.length && <p>На выбранную дату зачислений нет.</p>}</fieldset>}
    <Input label="Причина переноса" name="reason" required maxLength={1000} disabled={busy} onChange={() => setConfirmed(false)} />
    <label className="checkbox"><input type="checkbox" required checked={confirmed} onChange={e => setConfirmed(e.target.checked)} disabled={busy || loading || !!lookupError} />Подтверждаю перенос и выбранный состав</label>
    <div className="modal-footer"><Button variant="secondary" onClick={close} disabled={busy}>Отмена</Button><Button type="submit" loading={busy} disabled={loading || !!lookupError}>Подтвердить перенос</Button></div>
  </form></Modal>;
}

export function SnapshotEditor({ lesson, close, saved }: { lesson: Lesson; close: () => void; saved: () => void }) {
  const hasModule = lesson.module_id != null || !!lesson.version_module_key;
  const [history, setHistory] = useState<Page<Version> | null>(null); const [page, setPage] = useState(1); const [version, setVersion] = useState(0); const [loading, setLoading] = useState(true); const [lookupError, setLookupError] = useState(''); const [error, setError] = useState(''); const [busy, setBusy] = useState(false);
  useEffect(() => { const controller = new AbortController(); setLoading(true); setLookupError(''); request<Page<Version>>(`/lessons/${lesson.id}/snapshot-history?page=${page}&page_size=20`, 'GET', undefined, controller.signal).then(v => { if (!controller.signal.aborted) setHistory(v); }).catch(e => { if (!controller.signal.aborted) setLookupError(message(e)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); }); return () => controller.abort(); }, [lesson.id, page, version]);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setError(''); setBusy(true); const form = new FormData(event.currentTarget);
    try { await request(`/lessons/${lesson.id}/snapshot`, 'POST', { program_name: form.get('program_name'), module_name: hasModule ? form.get('module_name') : null, topic: form.get('topic'), reason: form.get('reason') }); saved(); }
    catch (e) { setError(message(e)); } finally { setBusy(false); }
  }
  return <Modal title="Исторический снимок занятия" description="Изменение сохранит предыдущую версию. Причина обязательна; audit фиксирует только изменённые поля." onClose={close} busy={busy}>
    <Notice tone="error">{error}</Notice><form onSubmit={submit}><Input label="Название программы в истории" name="program_name" defaultValue={lesson.program_name} required maxLength={200} disabled={busy} />{hasModule && <Input label="Название модуля в истории" name="module_name" defaultValue={lesson.module_name ?? ''} required maxLength={200} disabled={busy} />}<Input label="Тема в истории" name="topic" defaultValue={lesson.topic} required maxLength={200} disabled={busy} /><Input label="Причина исправления снимка" name="reason" required maxLength={1000} disabled={busy} /><div className="modal-footer"><Button variant="secondary" onClick={close} disabled={busy}>Закрыть</Button><Button type="submit" loading={busy}>Сохранить исправление снимка</Button></div></form>
    <h3>Предыдущие версии</h3><Notice tone="error">{lookupError}</Notice>{loading ? <p role="status">Загружаем версии…</p> : lookupError ? <Button variant="secondary" onClick={() => setVersion(v => v + 1)}>Повторить загрузку версий</Button> : history?.items.length ? <ul>{history.items.map(v => <li key={v.id}>{v.program_name ?? 'Историческое название неизвестно'} · {v.module_name ?? 'Без модуля'} · {v.topic ?? 'Историческая тема неизвестна'}{v.state === 'legacy_unknown' && ' · Исходный снимок отсутствовал'}<p className="muted">Причина исправления: {v.reason ?? 'Не сохранена для старой версии'}</p></li>)}</ul> : <EmptyState title="Предыдущих версий пока нет" />}{history && <Pagination page={page} pageSize={20} total={history.total} onChange={setPage} disabled={loading || busy} />}
  </Modal>;
}
