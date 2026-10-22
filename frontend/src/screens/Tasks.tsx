import { useEffect, useId, useState, type FormEvent, type ReactNode } from 'react';
import { APIError, message, request, type User } from '../api';
import { Badge, Button, Card, EmptyState, Input, Modal, Notice, PageHeading, Pagination, Select, Table, type Column } from '../components/ui';

type TaskStatus = 'todo' | 'in_progress' | 'done' | 'cancelled';
type ObjectType = 'student' | 'group' | 'lesson';
type Task = { id: number; title: string; description: string; creator_id: number; creator_name: string; assignee_id: number; assignee_name: string; due_at: string; status: TaskStatus; overdue: boolean; object_type: ObjectType | null; object_id: number | null; object_name: string | null; closed_at: string | null };
type Assignee = { id: number; name: string; blocked: boolean };
type Page<T> = { items: T[]; total: number; page: number; page_size: number };
type Related = { id: number; name?: string; topic?: string; starts_at?: string };
const statuses: Record<TaskStatus, string> = { todo: 'К выполнению', in_progress: 'В работе', done: 'Выполнена', cancelled: 'Отменена' };
const nextStatuses: Record<TaskStatus, TaskStatus[]> = { todo: ['in_progress', 'cancelled'], in_progress: ['done', 'cancelled'], done: ['in_progress', 'cancelled'], cancelled: [] };
const objects = { student: { label: 'Ученик', path: 'students' }, group: { label: 'Группа', path: 'groups' }, lesson: { label: 'Занятие', path: 'lessons' } };
const time = (value: string) => new Intl.DateTimeFormat('ru-RU', { timeZone: 'Europe/Moscow', dateStyle: 'short', timeStyle: 'short' }).format(new Date(value));
function localTime(value?: string) {
  if (!value) return '';
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone: 'Europe/Moscow', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).formatToParts(new Date(value));
  const get = (key: string) => parts.find(part => part.type === key)?.value;
  return `${get('year')}-${get('month')}-${get('day')}T${get('hour')}:${get('minute')}`;
}
function Status({ task }: { task: Task }) { return <><Badge tone={task.status === 'done' ? 'success' : task.status === 'in_progress' ? 'warning' : 'neutral'}>{statuses[task.status]}</Badge>{task.overdue && <Badge tone="danger">Просрочена</Badge>}</>; }

function RelatedPicker({ kind, selected, onSelect, disabled }: { kind: ObjectType; selected: Related | null; onSelect: (value: Related | null) => void; disabled: boolean }) {
  const [data, setData] = useState<Page<Related> | null>(null); const [page, setPage] = useState(1); const [search, setSearch] = useState(''); const [error, setError] = useState(''); const [loading, setLoading] = useState(true); const [version, setVersion] = useState(0);
  useEffect(() => { const controller = new AbortController(); setLoading(true); setError(''); const timer = window.setTimeout(() => { request<Page<Related>>(`/${objects[kind].path}?page=${page}&page_size=20&search=${encodeURIComponent(search)}`, 'GET', undefined, controller.signal).then(setData).catch(err => { if (!controller.signal.aborted) setError(message(err)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); }); }, 200); return () => { controller.abort(); clearTimeout(timer); }; }, [kind, page, search, version]);
  const label = (v: Related) => `${v.name || v.topic || `№${v.id}`}${v.starts_at ? ` · ${time(v.starts_at)}` : ''} · №${v.id}`;
  return <><Input label="Поиск связанной записи" value={search} onChange={e => { setSearch(e.target.value); setPage(1); }} maxLength={200} disabled={disabled} /><Notice tone="error">{error}</Notice>{error && <Button variant="secondary" onClick={() => setVersion(v => v + 1)}>Повторить поиск</Button>}
    <Select label="Связанная запись" value={selected?.id ?? ''} required disabled={disabled || loading || !!error} onChange={e => onSelect(data?.items.find(v => v.id === Number(e.target.value)) || null)}>
      <option value="">{loading ? 'Загружаем…' : 'Выберите запись'}</option>{selected && !data?.items.some(v => v.id === selected.id) && <option value={selected.id}>{label(selected)}</option>}{data?.items.map(v => <option key={v.id} value={v.id}>{label(v)}</option>)}
    </Select>{data && <Pagination page={page} pageSize={20} total={data.total} onChange={setPage} disabled={disabled || loading} />}</>;
}

function Editor({ task, assignees, close, saved }: { task?: Task; assignees: Assignee[]; close: () => void; saved: () => void }) {
  const descriptionID = useId(); const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [fields, setFields] = useState<Record<string, string>>({});
  const [kind, setKind] = useState<ObjectType | ''>(task?.object_type || ''); const [related, setRelated] = useState<Related | null>(task?.object_id ? { id: task.object_id, name: task.object_name || `№${task.object_id}` } : null);
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const data = new FormData(event.currentTarget); setError(''); setFields({});
    if (kind && !related) { setError('Выберите связанную запись.'); return; }
    const due = new Date(`${data.get('due_at')}:00+03:00`);
    if (Number.isNaN(due.getTime())) { setError('Укажите корректный срок.'); return; }
    setBusy(true);
    try { await request(task ? `/tasks/${task.id}` : '/tasks', task ? 'PATCH' : 'POST', { title: data.get('title'), description: data.get('description'), assignee_id: Number(data.get('assignee_id')), due_at: due.toISOString(), status: data.get('status'), object_type: kind || null, object_id: kind ? related?.id : null }); saved(); }
    catch (err) { setError(message(err)); if (err instanceof APIError) setFields(err.fields); } finally { setBusy(false); }
  }
  return <Modal title={task ? 'Изменить задачу' : 'Новая задача'} description="Срок указан по Москве. Можно связать задачу с одной записью." onClose={close} busy={busy}><Notice tone="error">{error}</Notice><form onSubmit={save}>
    <Input label="Название" name="title" defaultValue={task?.title} required maxLength={200} disabled={busy} error={fields.title} />
    <div className="field"><label htmlFor={descriptionID}>Ожидаемый результат</label><textarea id={descriptionID} name="description" defaultValue={task?.description} required maxLength={5000} disabled={busy} aria-invalid={!!fields.description || undefined} aria-describedby={fields.description ? `${descriptionID}-error` : undefined} />{fields.description && <p id={`${descriptionID}-error`} className="field-error">{fields.description}</p>}</div>
    <Select label="Ответственный" name="assignee_id" defaultValue={task?.assignee_id || ''} required disabled={busy} error={fields.assignee_id}><option value="">Выберите сотрудника</option>{assignees.map(v => <option key={v.id} value={v.id} disabled={v.blocked && v.id !== task?.assignee_id}>{v.name}{v.blocked ? ' · Заблокирован' : ''} · №{v.id}</option>)}</Select>
    <Input label="Срок (МСК)" name="due_at" type="datetime-local" defaultValue={localTime(task?.due_at)} required disabled={busy} error={fields.due_at} />
    <Select label="Статус задачи" name="status" defaultValue={task?.status || 'todo'} disabled={busy} error={fields.status}>{(task ? [task.status, ...nextStatuses[task.status]] : ['todo'] as TaskStatus[]).map(key => <option key={key} value={key}>{statuses[key]}</option>)}</Select>
    <Select label="Связь с записью" value={kind} onChange={e => { setKind(e.target.value as ObjectType | ''); setRelated(null); }} disabled={busy} error={fields.object_type || fields.object_id}><option value="">Без связи</option>{Object.entries(objects).map(([key, value]) => <option key={key} value={key}>{value.label}</option>)}</Select>
    {kind && <RelatedPicker key={kind} kind={kind} selected={related} onSelect={setRelated} disabled={busy} />}
    <div className="modal-footer"><Button variant="secondary" onClick={close} disabled={busy}>Отмена</Button><Button type="submit" loading={busy} disabled={!!kind && !related}>Сохранить задачу</Button></div>
  </form></Modal>;
}

function TaskDetail({ id, version, user, busy, close, actions, actionError }: { id: number; version: number; user: User; busy: boolean; close: () => void; actions: (task: Task) => ReactNode; actionError: string }) {
  const [task, setTask] = useState<Task | null>(null);
  const [error, setError] = useState('');
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setTask(null); setError('');
    request<Task>(`/tasks/${id}`, 'GET', undefined, controller.signal)
      .then(value => { if (!controller.signal.aborted) setTask(value); })
      .catch(err => { if (!controller.signal.aborted) setError(message(err)); });
    return () => controller.abort();
  }, [id, version, retry, user.id, user.roles.join(',')]);
  return <Modal title={task ? `Задача: ${task.title}` : 'Задача'} onClose={close} busy={busy}><div className="stack">
    <Notice tone="error">{error}</Notice>
    {error ? <Button variant="secondary" onClick={() => setRetry(v => v + 1)}>Повторить загрузку задачи</Button> : !task && <p role="status">Загружаем задачу…</p>}
    {task && <><Status task={task} /><h3>Ожидаемый результат</h3><p className="task-description">{task.description}</p><dl className="detail-list">
      <div><dt>Постановщик</dt><dd>{task.creator_name}</dd></div><div><dt>Ответственный</dt><dd>{task.assignee_name}</dd></div><div><dt>Срок (МСК)</dt><dd>{time(task.due_at)}</dd></div>
      <div><dt>Закрытие (МСК)</dt><dd>{task.closed_at ? time(task.closed_at) : ['done', 'cancelled'].includes(task.status) ? 'Историческая дата закрытия неизвестна' : 'Задача открыта'}</dd></div>
      <div><dt>Связанная запись</dt><dd>{task.object_type && task.object_id ? <a href={`#/${objects[task.object_type].path}/${task.object_id}`}>{objects[task.object_type].label}: {task.object_name} · №{task.object_id}</a> : 'Без связи'}</dd></div>
    </dl><Notice tone="error">{actionError}</Notice>{actions(task)}</>}
  </div></Modal>;
}

export function Tasks({ user }: { user: User }) {
  const manage = user.roles.some(role => role === 'admin' || role === 'leader');
  const [data, setData] = useState<Page<Task> | null>(null); const [page, setPage] = useState(1); const [filters, setFilters] = useState(''); const [version, setVersion] = useState(0); const [loading, setLoading] = useState(true); const [error, setError] = useState(''); const [success, setSuccess] = useState(''); const [actionError, setActionError] = useState('');
  const [assignees, setAssignees] = useState<Assignee[]>([]); const [lookupError, setLookupError] = useState(''); const [lookupLoading, setLookupLoading] = useState(manage); const [lookupVersion, setLookupVersion] = useState(0);
  const [editor, setEditor] = useState<Task | 'new' | null>(null); const [detail, setDetail] = useState<number | null>(null); const [busy, setBusy] = useState<number | null>(null);
  useEffect(() => { const controller = new AbortController(); setLoading(true); setError(''); request<Page<Task>>(`/tasks?page=${page}&page_size=20${filters}`, 'GET', undefined, controller.signal).then(v => { if (!controller.signal.aborted) setData(v); }).catch(err => { if (!controller.signal.aborted) { setData(null); setError(message(err)); } }).finally(() => { if (!controller.signal.aborted) setLoading(false); }); return () => controller.abort(); }, [page, filters, version, user.id, user.roles.join(',')]);
  useEffect(() => { if (!manage) { setAssignees([]); setLookupLoading(false); return; } const controller = new AbortController(); setLookupLoading(true); setLookupError(''); (async () => { try { const items: Assignee[] = []; let page = 1; for (;;) { const result = await request<Page<Assignee>>(`/tasks/assignees?page=${page++}&page_size=100`, 'GET', undefined, controller.signal); items.push(...result.items); if (items.length >= result.total) break; if (!result.items.length) throw new Error('Incomplete directory'); } if (!controller.signal.aborted) setAssignees(items); } catch (err) { if (!controller.signal.aborted) setLookupError(message(err)); } finally { if (!controller.signal.aborted) setLookupLoading(false); } })(); return () => controller.abort(); }, [manage, lookupVersion]);
  // Refresh computed overdue filters as deadlines pass, and when returning to the tab.
  useEffect(() => { const refresh = () => setVersion(v => v + 1); const timer = window.setInterval(refresh, 30_000); window.addEventListener('focus', refresh); return () => { clearInterval(timer); window.removeEventListener('focus', refresh); }; }, []);
  async function change(task: Task, status: TaskStatus) { setBusy(task.id); setActionError(''); setSuccess(''); try { await request(`/tasks/${task.id}/status`, 'PATCH', { status }); setSuccess('Статус задачи обновлён.'); setDetail(null); setVersion(v => v + 1); } catch (err) { setActionError(message(err)); if (err instanceof APIError && [403, 404, 409].includes(err.status)) { setDetail(null); setVersion(v => v + 1); } } finally { setBusy(null); } }
  const actions = (task: Task) => <div className="row-actions">{manage && <Button variant="secondary" disabled={busy != null || lookupLoading || !!lookupError} onClick={() => { setDetail(null); setEditor(task); }}>Изменить</Button>}{(manage || task.assignee_id === user.id) && <>{task.status === 'todo' && <Button variant="secondary" disabled={busy != null} onClick={() => void change(task, 'in_progress')}>В работу</Button>}{task.status === 'in_progress' && <Button variant="secondary" disabled={busy != null} onClick={() => void change(task, 'done')}>Завершить</Button>}</>}{(manage || task.creator_id === user.id) && <>{task.status === 'done' && <Button variant="secondary" disabled={busy != null} onClick={() => void change(task, 'in_progress')}>Вернуть в работу</Button>}{task.status !== 'cancelled' && <Button variant="secondary" disabled={busy != null} onClick={() => void change(task, 'cancelled')}>Отменить задачу</Button>}</>}</div>;
  const columns: Column<Task>[] = [
    { key: 'title', title: 'Задача', render: v => <Button className="task-title" variant="ghost" onClick={() => setDetail(v.id)}>{v.title}</Button> },
    { key: 'people', title: 'Ответственный и постановщик', render: v => <><span>{v.assignee_name}</span><span className="muted">Поставил: {v.creator_name}</span></> },
    { key: 'due', title: 'Срок (МСК)', render: v => time(v.due_at) },
    { key: 'status', title: 'Статус', render: v => <Status task={v} /> },
    { key: 'actions', title: 'Действия', render: actions },
  ];
  function filter(event: FormEvent<HTMLFormElement>) { event.preventDefault(); const form = new FormData(event.currentTarget); const params = new URLSearchParams(); for (const [key, value] of form) if (String(value).trim()) params.set(key, String(value).trim()); setFilters(params.size ? `&${params}` : ''); setPage(1); setVersion(v => v + 1); }
  return <><PageHeading title="Задачи" description={manage ? 'Ожидаемый результат, ответственные и сроки команды.' : 'Ваши задачи и сроки выполнения.'} action={manage && <Button disabled={loading || !!error || lookupLoading || !!lookupError} onClick={() => setEditor('new')}>Добавить: задача</Button>} />
    <Notice tone="error">{lookupError}</Notice>{lookupError && <Button variant="secondary" onClick={() => setLookupVersion(v => v + 1)}>Повторить загрузку сотрудников</Button>}
    <form className="filters" onSubmit={filter}><Input label="Поиск" name="search" maxLength={200} placeholder="По названию" /><Select label="Ответственный" name="assignee_id" disabled={lookupLoading || manage && !!lookupError}><option value="">{manage ? 'Все сотрудники' : 'Мои задачи'}</option>{(manage ? assignees : [{ id: user.id, name: user.name, blocked: false }]).map(v => <option value={v.id} key={v.id}>{v.name}{v.blocked ? ' · Заблокирован' : ''}</option>)}</Select><Select label="Статус" name="status"><option value="">Все статусы</option>{Object.entries(statuses).map(([key, label]) => <option key={key} value={key}>{label}</option>)}</Select><Select label="Просрочка" name="overdue"><option value="">Все задачи</option><option value="true">Только просроченные</option><option value="false">Без просрочки</option></Select><Button type="submit" variant="secondary">Применить</Button></form>
    <div className="stack"><Notice tone="success">{success}</Notice><Notice tone="error">{error || actionError}</Notice><Card className="table-card" aria-busy={loading}><div className="table-heading"><h2>Список задач</h2><span className="muted">Всего: {data?.total ?? '—'}</span></div>{loading ? <p className="empty-state" role="status">Загружаем данные…</p> : error ? <EmptyState title="Не удалось загрузить задачи" action={<Button variant="secondary" onClick={() => setVersion(v => v + 1)}>Повторить загрузку</Button>} /> : data?.items.length ? <Table caption="Задачи" columns={columns} items={data.items} rowKey={v => v.id} /> : <EmptyState title="Задачи не найдены" description="Попробуйте изменить фильтры." />}{data && <Pagination page={page} total={data.total} pageSize={20} onChange={setPage} disabled={loading} />}</Card></div>
    {manage && editor && <Editor task={editor === 'new' ? undefined : editor} assignees={assignees} close={() => setEditor(null)} saved={() => { setEditor(null); setSuccess('Задача сохранена.'); setVersion(v => v + 1); }} />}
    {detail !== null && <TaskDetail id={detail} version={version} user={user} busy={busy != null} close={() => setDetail(null)} actions={actions} actionError={actionError} />}
  </>;
}
