import { MAX_FILE_BYTES } from '../upload';
import { useEffect, useState } from 'react';
import { message, request } from '../api';
import { Badge, Button, Card, Input, Notice, Pagination, Select } from '../components/ui';

type Page<T> = { items: T[]; total: number; page_size: number };
type Person = { id: number; name: string; active: boolean };
type Registration = { id: number; student_id: number; name: string; status: string; revision: number; created_at: string; allowed_actions: string[] };
type ParticipationPage = Page<Registration> & { can_manage: boolean; self_registration_min_age: number; occupied: number };
type History = { status: string; revision: number; created_at: string };
type Document = { id: number; name: string; size: number };
const states: Record<string, string> = { requested: 'Заявка подана', confirmed: 'Подтверждено', waitlisted: 'Лист ожидания', cancelled: 'Отменено', attended: 'Посетил', no_show: 'Не явился' };
const actions: Record<string, string> = { confirmed: 'Подтвердить участие', cancelled: 'Отменить участие', attended: 'Отметить посещение', no_show: 'Отметить неявку' };
const date = (s: string) => new Date(s).toLocaleString('ru-RU');

export function EventParticipation({ id, open, capacity, revision }: { id: number; open: boolean; capacity: number; revision: number }) {
  const [data, setData] = useState<ParticipationPage | null>(null), [candidates, setCandidates] = useState<Page<Person> | null>(null);
  const [page, setPage] = useState(1), [candidatePage, setCandidatePage] = useState(1), [search, setSearch] = useState(''), [selected, setSelected] = useState('');
  const [reload, setReload] = useState(0), [error, setError] = useState(''), [candidateError, setCandidateError] = useState(''), [notice, setNotice] = useState(''), [busy, setBusy] = useState(false);
  useEffect(() => {
    const c = new AbortController(); setData(null); setError('');
    request<ParticipationPage>(`/events/${id}/registrations?page=${page}`, 'GET', undefined, c.signal).then(v => { if (!c.signal.aborted) setData(v); }).catch(e => { if (!c.signal.aborted) setError(message(e)); });
    return () => c.abort();
  }, [id, page, reload, revision]);
  useEffect(() => {
    const c = new AbortController(); setCandidates(null); setSelected(''); setCandidateError('');
    if (open) request<Page<Person>>(`/events/${id}/candidates?${new URLSearchParams({ page: String(candidatePage), search })}`, 'GET', undefined, c.signal).then(v => { if (!c.signal.aborted) setCandidates(v); }).catch(e => { if (!c.signal.aborted) setCandidateError(message(e)); });
    return () => c.abort();
  }, [id, open, candidatePage, search, reload]);
  async function mutate(r?: Registration, action?: string) {
    setBusy(true); setError(''); setNotice('');
    try {
      const v = await request<Registration>(`/events/${id}/registrations${r ? `/${r.id}/actions` : ''}`, 'POST', r ? { action, revision: r.revision } : { student_id: Number(selected) });
      setNotice(`Участие: ${states[v.status]}.`); setReload(n => n + 1);
    } catch (e) { setError(message(e)); } finally { setBusy(false); }
  }
  return <Card className="stack"><h2>{data?.can_manage ? 'Участники события' : 'Регистрация и участие'}</h2>
    <Notice tone="error">{error}</Notice><Notice tone="success">{notice}</Notice>
    <Button variant="secondary" disabled={busy} onClick={() => setReload(n => n + 1)}>Обновить заявки</Button>
    {!data && !error && <p role="status">Загрузка заявок…</p>}
    {data && <><p>Занято мест: {data.occupied} из {capacity}. Заявка резервирует место до подтверждения ответственным. При заполнении мест заявка попадает в лист ожидания.</p>
      <p>{data.self_registration_min_age > 0 ? `Самостоятельная запись ученика доступна с ${data.self_registration_min_age} лет. Возраст определяется на дату регистрации в часовом поясе события.` : 'Самостоятельная запись ученика закрыта до настройки согласованного возрастного правила. Записать ребёнка может родитель с подтверждённым доступом.'}</p>
      {open && <div className="stack"><Input label="Найти участника" value={search} onChange={e => { setSearch(e.target.value); setCandidatePage(1); }} />
        <Notice tone="error">{candidateError}</Notice>
        {!candidates && !candidateError && <p role="status">Загрузка доступных участников…</p>}
        {candidateError && <Button variant="secondary" onClick={() => setReload(n => n + 1)}>Повторить поиск участников</Button>}
        <Select label="Участник" value={selected} disabled={busy || !candidates} onChange={e => setSelected(e.target.value)}><option value="">Выберите ученика</option>{candidates?.items.map(p => <option key={p.id} value={p.id} disabled={!p.active}>{p.name}{!p.active ? ' — самостоятельная запись недоступна' : ''}</option>)}</Select>
        {candidates?.total === 0 && <p>Нет доступных учеников для записи.</p>}
        {candidates && <Pagination page={candidatePage} total={candidates.total} pageSize={candidates.page_size} onChange={setCandidatePage} disabled={busy} />}
        <Button loading={busy} disabled={!selected} onClick={() => void mutate()}>Подать заявку</Button></div>}
      {data.items.length === 0 && <p>Заявок пока нет.</p>}
      {data.items.map(r => <section className="stack" key={r.id}><h3>{r.name}</h3><Badge>{states[r.status]}</Badge><p>Заявка №{r.id} · {date(r.created_at)}</p>
        <div className="row-actions">{r.allowed_actions.map(a => <Button key={a} disabled={busy} variant="secondary" onClick={() => void mutate(r, a)}>{actions[a]}</Button>)}</div>
        <RegistrationDetails key={`${r.id}:${r.revision}:${revision}`} base={`/events/${id}/registrations/${r.id}`} />
      </section>)}
      <Pagination page={page} total={data.total} pageSize={data.page_size} onChange={setPage} disabled={busy} />
    </>}
  </Card>;
}
function RegistrationDetails({ base }: { base: string }) {
  const [expanded, setExpanded] = useState(false), [history, setHistory] = useState<History[]>([]), [docs, setDocs] = useState<Page<Document> | null>(null);
  const [page, setPage] = useState(1), [reload, setReload] = useState(0), [error, setError] = useState(''), [busy, setBusy] = useState(false), [notice, setNotice] = useState('');
  useEffect(() => {
    if (!expanded) return;
    const c = new AbortController(); setDocs(null); setHistory([]); setError('');
    Promise.all([request<History[]>(`${base}/history`, 'GET', undefined, c.signal), request<Page<Document>>(`${base}/documents?page=${page}`, 'GET', undefined, c.signal)])
      .then(([h, d]) => { if (!c.signal.aborted) { setHistory(h); setDocs(d); } }).catch(e => { if (!c.signal.aborted) setError(message(e)); });
    return () => c.abort();
  }, [expanded, base, page, reload]);
  async function upload(file: File) {
    setBusy(true); setError(''); setNotice('');
    try {
      if (file.size === 0 || file.size > MAX_FILE_BYTES) throw new Error('Размер документа: от 1 байта до 10 МиБ.');
      const data = await new Promise<string>((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(String(reader.result).split(',')[1]); reader.onerror = reject; reader.readAsDataURL(file); });
      await request(`${base}/documents`, 'POST', { name: file.name, data }); setNotice('Документ сохранён.'); setReload(n => n + 1);
    } catch (e) { setError(e instanceof Error ? e.message : message(e)); } finally { setBusy(false); }
  }
  return <div className="stack"><Button variant="ghost" aria-expanded={expanded} disabled={busy} onClick={() => setExpanded(v => !v)}>{expanded ? 'Скрыть историю и документы' : 'История и документы'}</Button>
    {expanded && <><Notice tone="error">{error}</Notice><Notice tone="success">{notice}</Notice>{!docs && !error && <p role="status">Загрузка истории и документов…</p>}{busy && <p role="status">Сохраняем документ…</p>}<h4>История заявки</h4>{history.map(h => <p key={h.revision}>{date(h.created_at)} · {states[h.status]}</p>)}{docs && !history.length && <p>История заявки пуста.</p>}
      <h4>Документы участия</h4><p>Доступ имеют участник, родители с действующим семейным доступом, администратор и назначенные ответственные события.</p>
      <Input label="Добавить документ (до 10 МиБ)" type="file" disabled={busy} onChange={e => { const f = e.target.files?.[0]; e.target.value = ''; if (f) void upload(f); }} />
      {docs?.items.map(d => <a key={d.id} href={`/api/v1${base}/documents/${d.id}`} download>{d.name} ({Math.ceil(d.size / 1024)} КиБ)</a>)}
      {docs?.total === 0 && <p>Документов нет.</p>}{docs && <Pagination page={page} total={docs.total} pageSize={docs.page_size} onChange={setPage} />}
      {error && <Button variant="secondary" onClick={() => setReload(n => n + 1)}>Повторить загрузку документов</Button>}
    </>}
  </div>;
}
