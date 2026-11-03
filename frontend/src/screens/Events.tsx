import { useEffect, useId, useState, type FormEvent } from 'react';
import { APIError, message, request, type User } from '../api';
import { Badge, Button, Card, Input, Notice, PageHeading, Pagination, Select } from '../components/ui';
import { eventInstant, eventLocal } from './eventTime';
import { EventParticipation } from './EventParticipation';
import { navigateWithNotice, useNavigationNotice } from '../navigation';

type Person = { id: number; name: string; active: boolean };
type Event = {
  id: number; title: string; type: string; description: string; organizer: string; responsible_ids: number[]; responsibles: Person[];
  starts_at: string | null; ends_at: string | null; time_zone: string; location: string; capacity: number;
  registration_starts_at: string | null; registration_ends_at: string | null; conditions: string; required_documents: string;
  status: string; revision: number; registration_open: boolean; can_edit: boolean; allowed_actions: string[];
  published_at: string | null; registration_closed_at: string | null; completed_at: string | null;
};
type Page<T> = { items: T[]; total: number; page_size: number };
type Draft = { title: string; type: string; description: string; organizer: string; time_zone: string; location: string; capacity: string; starts_at: string; ends_at: string; registration_starts_at: string; registration_ends_at: string; conditions: string; required_documents: string; responsibles: Person[] };
const statuses: Record<string, string> = { draft: 'Черновик', published: 'Опубликовано', registration_closed: 'Регистрация закрыта', completed: 'Завершено', cancelled: 'Отменено' };
const actions: Record<string, string> = { publish: 'Опубликовать', close_registration: 'Закрыть регистрацию', complete: 'Завершить событие', cancel: 'Отменить событие' };
const blank = (): Draft => ({ title: '', type: '', description: '', organizer: '', time_zone: 'Europe/Moscow', location: '', capacity: '', starts_at: '', ends_at: '', registration_starts_at: '', registration_ends_at: '', conditions: '', required_documents: '', responsibles: [] });
const date = (value: string | null, zone: string) => value ? new Date(value).toLocaleString('ru-RU', { timeZone: zone }) : 'Не указано';
function toDraft(e: Event): Draft {
  return { title: e.title, type: e.type, description: e.description, organizer: e.organizer, time_zone: e.time_zone, location: e.location, capacity: String(e.capacity || ''), starts_at: eventLocal(e.starts_at, e.time_zone), ends_at: eventLocal(e.ends_at, e.time_zone), registration_starts_at: eventLocal(e.registration_starts_at, e.time_zone), registration_ends_at: eventLocal(e.registration_ends_at, e.time_zone), conditions: e.conditions, required_documents: e.required_documents, responsibles: e.responsibles };
}
function TextField({ label, value, onChange, disabled, maxLength = 5000 }: { label: string; value: string; onChange: (v: string) => void; disabled: boolean; maxLength?: number }) {
  const id = useId(); return <div className="field"><label htmlFor={id}>{label}</label><textarea className="input" id={id} value={value} onChange={e => onChange(e.target.value)} rows={3} maxLength={maxLength} disabled={disabled} /></div>;
}
function StaffPicker({ selected, onChange, disabled }: { selected: Person[]; onChange: (v: Person[]) => void; disabled: boolean }) {
  const [search, setSearch] = useState(''), [page, setPage] = useState(1), [reload, setReload] = useState(0);
  const [data, setData] = useState<Page<Person> | null>(null), [error, setError] = useState('');
  useEffect(() => {
    const c = new AbortController(); setData(null); setError('');
    request<Page<Person>>(`/events/staff?${new URLSearchParams({ search, page: String(page) })}`, 'GET', undefined, c.signal)
      .then(v => { if (!c.signal.aborted) setData(v); }).catch(e => { if (!c.signal.aborted) setError(message(e)); });
    return () => c.abort();
  }, [search, page, reload]);
  return <fieldset className="stack" disabled={disabled}><legend>Ответственные сотрудники ({selected.length} из 20)</legend>
    {selected.map(p => <div className="row-actions" key={p.id}><span>{p.name}{!p.active && ' · недоступен'}</span><Button variant="ghost" onClick={() => onChange(selected.filter(v => v.id !== p.id))}>Убрать {p.name}</Button></div>)}
    <Input label="Найти сотрудника" maxLength={200} value={search} onChange={e => { setSearch(e.target.value); setPage(1); }} />
    <Notice tone="error">{error}</Notice>{error && <Button variant="secondary" onClick={() => setReload(v => v + 1)}>Повторить поиск сотрудников</Button>}
    {!data && !error && <p role="status">Загрузка сотрудников…</p>}
    {data?.items.length === 0 && <p>Сотрудники не найдены.</p>}
    {data?.items.map(p => <label key={p.id}><input type="checkbox" checked={selected.some(v => v.id === p.id)} disabled={disabled || (selected.length >= 20 && !selected.some(v => v.id === p.id))} onChange={e => onChange(e.target.checked ? [...selected, p] : selected.filter(v => v.id !== p.id))} /> {p.name}</label>)}
    {data && <Pagination page={page} total={data.total} pageSize={data.page_size} disabled={disabled} onChange={setPage} />}
  </fieldset>;
}

export function Events({ user, id }: { user: User; id?: string }) {
  if (id) return <EventCard id={id} admin={user.roles.includes('admin')} />;
  return <EventList admin={user.roles.includes('admin')} drafts={user.roles.some(r => r === 'admin' || r === 'leader')} />;
}
function EventList({ admin, drafts }: { admin: boolean; drafts: boolean }) {
  const [data, setData] = useState<Page<Event> | null>(null), [error, setError] = useState('');
  const [search, setSearch] = useState(''), [status, setStatus] = useState(''), [page, setPage] = useState(1), [reload, setReload] = useState(0);
  useEffect(() => {
    const c = new AbortController(); setData(null); setError('');
    request<Page<Event>>(`/events?${new URLSearchParams({ search, status, page: String(page) })}`, 'GET', undefined, c.signal)
      .then(v => { if (!c.signal.aborted) setData(v); }).catch(e => { if (!c.signal.aborted) setError(message(e)); });
    return () => c.abort();
  }, [search, status, page, reload]);
  return <div className="stack">
    <PageHeading title="Бесплатные события" description="Встречи, мастер-классы и другие события центра." action={admin && <a className="button button--primary" href="#/events/new">Создать событие</a>} />
    <Card><div className="filters"><Input label="Поиск по названию" value={search} maxLength={200} onChange={e => { setSearch(e.target.value); setPage(1); }} />
      <Select label="Статус события" value={status} onChange={e => { setStatus(e.target.value); setPage(1); }}><option value="">Все доступные</option>{Object.entries(statuses).filter(([key]) => drafts || key !== 'draft').map(([key, label]) => <option key={key} value={key}>{label}</option>)}</Select>
      <Button variant="secondary" onClick={() => setReload(v => v + 1)}>Обновить события</Button></div></Card>
    <Notice tone="error">{error}</Notice>{!data && !error && <p role="status">Загрузка…</p>}
    {data?.items.length === 0 && <Card><p>Доступных событий нет.</p></Card>}
    {data?.items.map(e => <Card key={e.id} className="stack"><Badge>{statuses[e.status]}</Badge><h2><a href={`#/events/${e.id}`}>{e.title}</a></h2><p>{e.type || 'Тип не указан'} · Бесплатно</p><p>{date(e.starts_at, e.time_zone)} — {date(e.ends_at, e.time_zone)} ({e.time_zone})</p><p>{e.location || 'Место не указано'}</p></Card>)}
    {data && <Pagination page={page} total={data.total} pageSize={data.page_size} onChange={setPage} />}
  </div>;
}
function EventCard({ id, admin }: { id: string; admin: boolean }) {
  const creating = id === 'new';
  const [card, setCard] = useState<Event | null>(null), [draft, setDraft] = useState<Draft | null>(creating && admin ? blank() : null);
  const [error, setError] = useState(''), [busy, setBusy] = useState(false), [stale, setStale] = useState(false), [reload, setReload] = useState(0);
  const [notice, setNotice] = useNavigationNotice();
  const zones = useId();
  useEffect(() => {
    if (creating) return;
    const c = new AbortController(); setCard(null); setError('');
    request<Event>(`/events/${id}`, 'GET', undefined, c.signal).then(v => { if (!c.signal.aborted) { setCard(v); setStale(false); } }).catch(e => { if (!c.signal.aborted) { setError(message(e)); setDraft(null); } });
    return () => c.abort();
  }, [id, creating, reload]);
  const fail = (e: unknown) => {
    setError(e instanceof Error && !(e instanceof APIError) ? e.message : message(e));
    if (e instanceof APIError && [401, 403, 404].includes(e.status)) { setCard(null); setDraft(null); }
    if (e instanceof APIError && e.status === 409) setStale(true);
  };
  async function save(e: FormEvent) {
    e.preventDefault(); if (!draft) return; setBusy(true); setError(''); setNotice('');
    try {
      const input = { title: draft.title, type: draft.type, description: draft.description, organizer: draft.organizer, time_zone: draft.time_zone, location: draft.location, capacity: Number(draft.capacity || 0), conditions: draft.conditions, required_documents: draft.required_documents, responsible_ids: draft.responsibles.map(v => v.id), revision: card?.revision || 0, starts_at: eventInstant(draft.starts_at, draft.time_zone), ends_at: eventInstant(draft.ends_at, draft.time_zone), registration_starts_at: eventInstant(draft.registration_starts_at, draft.time_zone), registration_ends_at: eventInstant(draft.registration_ends_at, draft.time_zone) };
      const result = await request<Event>(creating ? '/events' : `/events/${id}`, creating ? 'POST' : 'PUT', input);
      if (creating) navigateWithNotice(`#/events/${result.id}`, 'Черновик события создан.');
      else { setCard(result); setDraft(null); setNotice('Карточка сохранена.'); }
    } catch (e) { fail(e); } finally { setBusy(false); }
  }
  async function act(action: string) {
    if (!card) return; setBusy(true); setError(''); setNotice('');
    try { setCard(await request<Event>(`/events/${id}/actions`, 'POST', { action, revision: card.revision })); setNotice('Статус события обновлён.'); }
    catch (e) { fail(e); } finally { setBusy(false); }
  }
  function edit<K extends keyof Draft>(key: K, value: Draft[K]) { setDraft(v => v ? { ...v, [key]: value } : v); }
  if (creating && !admin) return <PageHeading title="Раздел недоступен" />;
  return <div className="stack"><a href="#/events">← Бесплатные события</a>
    <PageHeading title={creating ? 'Новое событие' : card?.title || 'Карточка события'} description={draft ? creating || card?.status === 'draft' ? 'Черновик можно сохранить частично. Для публикации заполните все сведения, кроме необязательного списка документов.' : 'После сохранения изменения будут видны читателям карточки.' : undefined} />
    <Notice tone="error">{error}</Notice><Notice tone="success">{notice}</Notice>
    {!creating && !card && !error && <p role="status">Загрузка…</p>}
    {!creating && (error || stale) && <><p>Загрузка актуальной карточки заменит несохранённый ввод. При необходимости скопируйте его перед обновлением.</p><Button variant="secondary" disabled={busy} onClick={() => { setDraft(null); setReload(v => v + 1); }}>Загрузить актуальную карточку</Button></>}
    {draft && <Card><form className="stack" onSubmit={e => void save(e)}>
      <Input label="Название" required maxLength={200} value={draft.title} disabled={busy} onChange={e => edit('title', e.target.value)} />
      <Input label="Тип события" hint="Например, мастер-класс или открытая встреча." maxLength={100} value={draft.type} disabled={busy} onChange={e => edit('type', e.target.value)} />
      <TextField label="Описание" value={draft.description} disabled={busy} onChange={v => edit('description', v)} />
      <Input label="Организатор" maxLength={200} value={draft.organizer} disabled={busy} onChange={e => edit('organizer', e.target.value)} />
      <StaffPicker selected={draft.responsibles} onChange={v => edit('responsibles', v)} disabled={busy} />
      <Input label="Часовой пояс" hint="Все даты формы вводятся в выбранном поясе. Например, Europe/Moscow или Asia/Yekaterinburg." list={zones} value={draft.time_zone} maxLength={100} disabled={busy} required onChange={e => edit('time_zone', e.target.value)} />
      <datalist id={zones}>{['Europe/Moscow', 'Europe/Kaliningrad', 'Asia/Yekaterinburg', 'Asia/Novosibirsk', 'Asia/Vladivostok', 'UTC'].map(z => <option key={z} value={z} />)}</datalist>
      {([['starts_at', 'Начало события'], ['ends_at', 'Окончание события'], ['registration_starts_at', 'Начало регистрации'], ['registration_ends_at', 'Окончание регистрации']] as const).map(([key, label]) => <Input key={key} label={label} type="datetime-local" step="1" value={draft[key]} disabled={busy} onChange={e => edit(key, e.target.value)} />)}
      <Input label="Место" maxLength={1000} value={draft.location} disabled={busy} onChange={e => edit('location', e.target.value)} />
      <Input label="Вместимость" hint="Количество участников; это не число свободных мест." type="number" min={0} max={1000000} step={1} value={draft.capacity} disabled={busy} onChange={e => edit('capacity', e.target.value)} />
      <TextField label="Условия участия" value={draft.conditions} disabled={busy} onChange={v => edit('conditions', v)} />
      <TextField label="Необходимые документы" value={draft.required_documents} disabled={busy} onChange={v => edit('required_documents', v)} />
      <p>Если документы не нужны, оставьте поле пустым. Участие бесплатное.</p>
      <div className="row-actions"><Button type="submit" disabled={busy || stale}>Сохранить {creating || card?.status === 'draft' ? 'черновик' : 'карточку'}</Button>{!creating && <Button variant="secondary" disabled={busy} onClick={() => setDraft(null)}>Отменить редактирование</Button>}</div>
    </form></Card>}
    {card && !draft && <><Card className="stack"><Badge>{statuses[card.status]}</Badge><p>Бесплатно · Редакция {card.revision}</p>
      <h2>О событии</h2><p>Тип: {card.type || 'Не указан'}</p><p style={{ whiteSpace: 'pre-wrap' }}>{card.description || 'Описание не заполнено.'}</p>
      <p>Организатор: {card.organizer || 'Не указан'}</p><p>Ответственные: {card.responsibles.map(p => p.name).join(', ') || 'Не назначены'}</p>
      <h2>Время и место</h2><p>Часовой пояс: {card.time_zone}</p><p>Начало: {date(card.starts_at, card.time_zone)}<br />Окончание: {date(card.ends_at, card.time_zone)}</p><p style={{ whiteSpace: 'pre-wrap' }}>Место: {card.location || 'Не указано'}</p><p>Вместимость: {card.capacity || 'Не указана'}</p>
      <h2>Участие</h2><p>Период регистрации: {date(card.registration_starts_at, card.time_zone)} — {date(card.registration_ends_at, card.time_zone)}</p>
      <p>{card.registration_open ? 'Сейчас действует период регистрации.' : 'Сейчас регистрация по датам или статусу закрыта.'}</p>
      <p style={{ whiteSpace: 'pre-wrap' }}>Условия: {card.conditions || 'Не указаны'}</p><p style={{ whiteSpace: 'pre-wrap' }}>Необходимые документы: {card.required_documents || 'Не требуются'}</p>
      {card.status === 'cancelled' && <p>Событие отменено. История регистрации сохранена.</p>}
      <div className="row-actions">{card.can_edit && <Button variant="secondary" disabled={busy || stale} onClick={() => setDraft(toDraft(card))}>Редактировать событие</Button>}{card.allowed_actions.map(action => <Button key={action} disabled={busy || stale} onClick={() => void act(action)}>{actions[action]}</Button>)}</div>
    </Card>{card.published_at && <Card><p>Опубликовано: {date(card.published_at, card.time_zone)}</p>{card.registration_closed_at && <p>Регистрация закрыта: {date(card.registration_closed_at, card.time_zone)}</p>}{card.completed_at && <p>Завершено: {date(card.completed_at, card.time_zone)}</p>}</Card>}</>}
    {card && !draft && card.status !== 'draft' && <EventParticipation id={card.id} open={card.registration_open} capacity={card.capacity} revision={card.revision} />}
  </div>;
}
