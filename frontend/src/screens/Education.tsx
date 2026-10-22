import { useEffect, useId, useState, type FormEvent, type ReactNode } from 'react';
import { APIError, message, request, type User } from '../api';
import { Badge, Button, Card, EmptyState, Input, Modal, Notice, PageHeading, Pagination, Select, Table, type Column } from '../components/ui';
import { Icon, type IconName } from '../components/Icon';
import { GroupEditor } from './GroupEditor';

type RecordData = { id: number; name?: string; title?: string; status?: string; [key: string]: unknown };
type ListData = { items: RecordData[]; total: number; page: number; page_size: number };
type Field = { key: string; label: string; type?: 'text' | 'email' | 'tel' | 'date' | 'datetime-local' | 'number' | 'textarea' | 'select' | 'checkbox'; required?: boolean; options?: [string, string][]; reference?: string; min?: number; maxLength?: number; hint?: string };
type Section = { title: string; description: string; singular: string; fields: Field[]; columns: [string, string][]; statusOptions?: [string, string][]; detail?: boolean };
const activeStatuses: [string, string][] = [['active', 'Активен'], ['archived', 'В архиве']];
const programStatuses: [string, string][] = [['draft', 'Черновик'], ['active', 'Активна'], ['archived', 'В архиве']];
const taskStatuses: [string, string][] = [['todo', 'К выполнению'], ['in_progress', 'В работе'], ['done', 'Выполнена'], ['cancelled', 'Отменена']];
const attendanceStatuses: [string, string][] = [['unmarked', 'Не отмечен'], ['present', 'Присутствовал'], ['absent', 'Отсутствовал'], ['excused', 'Уважительная причина'], ['late', 'Опоздал']];
const lessonStatuses: [string, string][] = [['scheduled', 'Запланировано'], ['completed', 'Проведено'], ['cancelled', 'Отменено']];
const nameField: Field = { key: 'name', label: 'Имя и фамилия', required: true, maxLength: 200 };
const contactFields: Field[] = [{ key: 'phone', label: 'Телефон', type: 'tel' }, { key: 'email', label: 'Email', type: 'email' }];
export const sections: Record<string, Section> = {
  teachers: { title: 'Педагоги', description: 'Команда, специализации и статус педагогов.', singular: 'Педагог', statusOptions: activeStatuses, columns: [['name', 'Педагог'], ['specialization', 'Специализация'], ['phone', 'Телефон']], fields: [nameField, ...contactFields, { key: 'specialization', label: 'Специализация' }, { key: 'user_id', label: 'Учётная запись', type: 'select', reference: 'users' }] },
  students: { title: 'Ученики', description: 'Карточки учеников, кураторы и образовательная история.', singular: 'Ученик', detail: true, statusOptions: activeStatuses, columns: [['name', 'Ученик'], ['birth_date', 'Дата рождения'], ['curator_name', 'Куратор']], fields: [nameField, { key: 'birth_date', label: 'Дата рождения', type: 'date' }, { key: 'curator_id', label: 'Куратор', type: 'select', reference: 'users?role=curator' }] },
  parents: { title: 'Родители', description: 'Контакты родителей и представителей учеников.', singular: 'Родитель', columns: [['name', 'Родитель'], ['phone', 'Телефон'], ['email', 'Email']], fields: [nameField, ...contactFields] },
  programs: { title: 'Программы', description: 'Образовательные программы и последовательность модулей.', singular: 'Программа', detail: true, statusOptions: programStatuses, columns: [['name', 'Программа'], ['description', 'Описание']], fields: [{ key: 'name', label: 'Название', required: true }, { key: 'description', label: 'Описание', type: 'textarea' }, { key: 'status', label: 'Статус', type: 'select', options: programStatuses, required: true }] },
  groups: { title: 'Группы', description: 'Состав групп, программы и основные педагоги.', singular: 'Группа', detail: true, statusOptions: programStatuses, columns: [['name', 'Группа'], ['program_name', 'Программа'], ['teacher_name', 'Педагог'], ['capacity', 'Вместимость']], fields: [{ key: 'name', label: 'Название', required: true }, { key: 'program_id', label: 'Программа', type: 'select', reference: 'programs?status=active', required: true }, { key: 'teacher_id', label: 'Основной педагог', type: 'select', reference: 'teachers?status=active' }, { key: 'capacity', label: 'Вместимость', type: 'number', min: 1, required: true }, { key: 'status', label: 'Статус', type: 'select', options: programStatuses, required: true }] },
  tasks: { title: 'Задачи', description: 'Ожидаемый результат, ответственный и срок выполнения.', singular: 'Задача', statusOptions: taskStatuses, columns: [['title', 'Задача'], ['assignee_name', 'Ответственный'], ['due_at', 'Срок (МСК)']], fields: [{ key: 'title', label: 'Название', required: true }, { key: 'description', label: 'Ожидаемый результат', type: 'textarea', required: true }, { key: 'assignee_id', label: 'Ответственный', type: 'select', reference: 'users', required: true }, { key: 'due_at', label: 'Срок (МСК)', type: 'datetime-local', required: true }, { key: 'object_type', label: 'Связанный объект', type: 'select', options: [['student', 'Ученик'], ['group', 'Группа'], ['lesson', 'Занятие']] }, { key: 'object_id', label: 'Номер связанной карточки', type: 'number', min: 1, hint: 'Укажите номер существующей карточки выбранного типа.' }] },
  lessons: { title: 'Расписание', description: 'Занятия и журнал посещаемости. Время указано по Москве.', singular: 'Занятие', detail: true, statusOptions: lessonStatuses, columns: [['topic', 'Тема'], ['group_name', 'Группа'], ['teacher_name', 'Педагог'], ['starts_at', 'Начало (МСК)']], fields: [{ key: 'group_id', label: 'Группа', type: 'select', reference: 'groups?status=active', required: true }, { key: 'topic', label: 'Тема', required: true }, { key: 'teacher_id', label: 'Педагог', type: 'select', reference: 'teachers?status=active', required: true }, { key: 'starts_at', label: 'Начало (МСК)', type: 'datetime-local', required: true }, { key: 'ends_at', label: 'Окончание (МСК)', type: 'datetime-local', required: true }, { key: 'location', label: 'Место проведения' }] },
};

function text(value: unknown): string { return value == null || value === '' ? 'Не указано' : String(value); }
function date(value: unknown, withTime = false) { if (!value) return 'Не указано'; const result = new Date(String(value)); return Number.isNaN(result.getTime()) ? text(value) : new Intl.DateTimeFormat('ru-RU', { timeZone: 'Europe/Moscow', dateStyle: 'medium', ...(withTime ? { timeStyle: 'short' as const } : {}) }).format(result); }
function moscowInput(value: unknown) { if (!value) return ''; const parts = new Intl.DateTimeFormat('en-CA', { timeZone: 'Europe/Moscow', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).formatToParts(new Date(String(value))); const get = (type: string) => parts.find(part => part.type === type)?.value; return `${get('year')}-${get('month')}-${get('day')}T${get('hour')}:${get('minute')}`; }
export function Status({ value }: { value?: string }) { const label = [...activeStatuses, ...programStatuses, ...taskStatuses, ...lessonStatuses, ...attendanceStatuses].find(([key]) => key === value)?.[1] ?? value ?? 'Не указан'; return <Badge tone={['active', 'present', 'completed', 'done'].includes(value ?? '') ? 'success' : ['absent', 'cancelled'].includes(value ?? '') ? 'danger' : ['unmarked', 'late', 'in_progress', 'draft'].includes(value ?? '') ? 'warning' : 'neutral'}>{label}</Badge>; }
function displayError(err: unknown) { return err instanceof APIError && err.status === 404 ? 'Этот раздел готовится к запуску. Данные пока недоступны.' : message(err); }
function useRemote<T>(path: string) {
  const [data, setData] = useState<T | null>(null); const [error, setError] = useState(''); const [loading, setLoading] = useState(true); const [version, setVersion] = useState(0);
  useEffect(() => { const controller = new AbortController(); setLoading(true); setError(''); setData(null); request<T>(path, 'GET', undefined, controller.signal).then(setData).catch(err => { if (!controller.signal.aborted) setError(displayError(err)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); }); return () => controller.abort(); }, [path, version]);
  return { data, error, loading, refresh: () => setVersion(value => value + 1) };
}
function ResourceState({ loading, error, retry, children }: { loading: boolean; error: string; retry: () => void; children: ReactNode }) { return loading ? <Card><p role="status">Загружаем данные…</p></Card> : error ? <Card><EmptyState title="Раздел недоступен" description={error} action={<Button variant="secondary" onClick={retry}>Повторить загрузку</Button>} /></Card> : <>{children}</>; }

export async function referenceItems(reference: string, signal?: AbortSignal): Promise<RecordData[]> {
  const items: RecordData[] = []; const separator = reference.includes('?') ? '&' : '?';
  for (let page = 1; ; page++) {
    const result = await request<ListData>(`/${reference}${separator}page_size=100&page=${page}`, 'GET', undefined, signal);
    items.push(...result.items);
    if (items.length >= result.total || !result.items.length) return items;
  }
}

function LookupSelect({ label, name, reference }: { label: string; name: string; reference: string }) {
  const remote = useRemote<ListData>(`/${reference}${reference.includes('?') ? '&' : '?'}page_size=100`);
  return <Select label={label} name={name} disabled={remote.loading || !!remote.error}><option value="">{remote.loading ? 'Загрузка…' : 'Все'}</option>{remote.data?.items.map(record => <option value={record.id} key={record.id}>{text(record.name ?? record.title)}</option>)}</Select>;
}

type RecordEditorProps = { section: Section; path: string; record?: RecordData; close: () => void; saved: () => void };
export function RecordEditor(props: RecordEditorProps) {
  return props.path === 'groups' ? <GroupEditor record={props.record} close={props.close} saved={props.saved} /> : <GeneralRecordEditor {...props} />;
}
function GeneralRecordEditor({ section, path, record, close, saved }: RecordEditorProps) {
  const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [fields, setFields] = useState<Record<string, string>>({}); const [options, setOptions] = useState<Record<string, RecordData[]>>({}); const [referencesLoading, setReferencesLoading] = useState(true); const id = useId();
  useEffect(() => { const controller = new AbortController(); (async () => { try { const values = await Promise.all(section.fields.filter(field => field.reference).map(async field => { const items = await referenceItems(field.reference!, controller.signal); const selected = record?.[field.key]; if (selected && !items.some(item => item.id === selected)) items.push({ id: Number(selected), name: String(record?.[field.key.replace(/_id$/, '_name')] ?? `№ ${selected}`) }); return [field.key, items] as const; })); if (!controller.signal.aborted) setOptions(Object.fromEntries(values)); } catch (err) { if (!controller.signal.aborted) setError(displayError(err)); } finally { if (!controller.signal.aborted) setReferencesLoading(false); } })(); return () => controller.abort(); }, [section, record]);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const data = new FormData(event.currentTarget); const body: Record<string, unknown> = {}; const invalid: Record<string, string> = {};
    for (const field of section.fields) { const value = String(data.get(field.key) ?? '').trim(); body[field.key] = field.type === 'checkbox' ? data.has(field.key) : value === '' ? null : field.type === 'number' || field.reference ? Number(value) : field.type === 'datetime-local' ? new Date(`${value}:00+03:00`).toISOString() : value; }
    if (path === 'parents' && !body.phone && !body.email) invalid.phone = 'Укажите телефон или email родителя.';
    if (body.birth_date && String(body.birth_date) > moscowInput(new Date().toISOString()).slice(0, 10)) invalid.birth_date = 'Дата рождения не может быть в будущем.';
    if (body.starts_at && body.ends_at && String(body.ends_at) <= String(body.starts_at)) invalid.ends_at = 'Окончание должно быть позже начала.';
    if (Object.keys(invalid).length) { setFields(invalid); setError('Проверьте введённые данные.'); return; }
    setBusy(true); setError(''); setFields({});
    try { await request(record ? `/${path}/${record.id}` : `/${path}`, record ? 'PATCH' : 'POST', body); saved(); }
    catch (err) { setError(err instanceof APIError ? err.message : message(err)); if (err instanceof APIError) setFields(err.fields ?? {}); } finally { setBusy(false); }
  }
  return <Modal title={`${record ? 'Изменить' : 'Создать'}: ${section.singular.toLowerCase()}`} onClose={close} busy={busy}><Notice tone="error">{error}</Notice><form onSubmit={submit}>{section.fields.map(field => {
    const value = record?.[field.key]; const common = { name: field.key, label: field.label, required: field.required, disabled: busy, hint: field.hint, error: fields[field.key] };
    if (field.type === 'checkbox') return <label className="checkbox" key={field.key}><input type="checkbox" name={field.key} disabled={busy} defaultChecked={value === true} />{field.label}</label>;
    if (field.type === 'select') return <Select key={`${field.key}-${referencesLoading}`} {...common} disabled={busy || referencesLoading} defaultValue={value == null ? field.key === 'status' && path === 'groups' ? 'draft' : '' : String(value)}><option value="">{field.required ? 'Выберите значение' : 'Не выбрано'}</option>{field.reference ? (options[field.key] ?? []).map(option => <option value={option.id} key={option.id}>{text(option.name ?? option.title)}</option>) : field.options?.map(([key, label]) => <option key={key} value={key}>{label}</option>)}</Select>;
    if (field.type === 'textarea') return <div className="field" key={field.key}><label htmlFor={`${id}-${field.key}`}>{field.label}</label><textarea id={`${id}-${field.key}`} name={field.key} required={field.required} disabled={busy} defaultValue={value == null ? '' : String(value)} aria-invalid={!!fields[field.key] || undefined} aria-describedby={fields[field.key] ? `${id}-${field.key}-error` : undefined} />{fields[field.key] && <p className="field-error" id={`${id}-${field.key}-error`}>{fields[field.key]}</p>}</div>;
    return <Input key={field.key} {...common} type={field.type ?? 'text'} min={field.min} step={field.type === 'number' ? 1 : undefined} maxLength={field.maxLength} defaultValue={field.type === 'datetime-local' ? moscowInput(value) : value == null ? '' : String(value)} />;
  })}<div className="modal-footer"><Button variant="secondary" onClick={close} disabled={busy}>Отмена</Button><Button type="submit" loading={busy} disabled={referencesLoading || !!error && Object.keys(options).length === 0 && section.fields.some(field => field.reference)}>{busy ? 'Сохраняем…' : 'Сохранить'}</Button></div></form></Modal>;
}

export function EntityScreen({ kind, user }: { kind: string; user: User }) {
  const section = sections[kind]; const [page, setPage] = useState(1); const [query, setQuery] = useState(''); const [status, setStatus] = useState(''); const [filters, setFilters] = useState(''); const [success, setSuccess] = useState(''); const [error, setError] = useState(''); const [editor, setEditor] = useState<RecordData | 'new' | null>(null); const [archive, setArchive] = useState<RecordData | null>(null); const [busy, setBusy] = useState(false); const [calendar, setCalendar] = useState(false);
  const remote = useRemote<ListData>(`/${kind}?page=${page}&page_size=20${filters}`); const canManage = user.roles.includes('admin') || kind === 'tasks' && user.roles.includes('leader');
  const columns: Column<RecordData>[] = section.columns.map(([key, title], index) => ({ key, title, render: record => index === 0 && section.detail ? <a href={`#/${kind}/${record.id}`}>{text(record[key])}</a> : key.endsWith('_at') ? date(record[key], true) : key === 'birth_date' ? date(record[key]) : text(record[key]) }));
  if (section.statusOptions) columns.push({ key: 'status', title: 'Статус', render: record => <Status value={record.status} /> });
  if (canManage) columns.push({ key: 'actions', title: 'Действия', render: record => <div className="row-actions"><Button variant="secondary" onClick={() => { setError(''); setEditor(record); }} aria-label={`Изменить ${text(record.name ?? record.title ?? record.topic)}`}>Изменить</Button>{kind !== 'tasks' && kind !== 'lessons' && record.status !== 'archived' && section.statusOptions && <Button variant="ghost" onClick={() => { setError(''); setArchive(record); }}>В архив</Button>}</div> });
  async function archiveRecord() { if (!archive) return; setBusy(true); setError(''); try { await request(`/${kind}/${archive.id}`, 'PATCH', { status: 'archived' }); setArchive(null); setSuccess('Запись перенесена в архив. История сохранена.'); remote.refresh(); } catch (err) { setError(message(err)); } finally { setBusy(false); } }
  function filter(event: FormEvent<HTMLFormElement>) { event.preventDefault(); const data = new FormData(event.currentTarget); const params = new URLSearchParams(); if (query) params.set('search', query); if (status) params.set('status', status); for (const key of ['date_from', 'date_to', 'teacher_id', 'group_id', 'assignee_id', 'overdue']) { if (data.get(key)) params.set(key, String(data.get(key))); } setFilters(params.size ? `&${params}` : ''); setPage(1); }
  return <><PageHeading title={section.title} description={section.description} action={canManage && <Button onClick={() => setEditor('new')} disabled={!remote.data || remote.loading}>Добавить: {section.singular.toLowerCase()}</Button>} /><form className="filters" onSubmit={filter}><Input label="Поиск" value={query} onChange={event => setQuery(event.target.value)} placeholder={kind === 'lessons' ? 'По теме занятия' : 'По имени или названию'} />{section.statusOptions && <Select label="Статус" value={status} onChange={event => setStatus(event.target.value)}><option value="">Все статусы</option>{section.statusOptions.map(([value, label]) => <option value={value} key={value}>{label}</option>)}</Select>}{kind === 'lessons' && <><Input label="С даты" type="date" name="date_from" /><Input label="По дату" type="date" name="date_to" /><LookupSelect label="Группа" name="group_id" reference="groups" /><LookupSelect label="Педагог" name="teacher_id" reference="teachers" /></>}{kind === 'tasks' && <label className="checkbox"><input name="overdue" value="true" type="checkbox" />Только просроченные</label>}<Button variant="secondary" type="submit">Применить</Button></form><div className="stack"><Notice tone="success">{success}</Notice><Notice tone="error">{!archive && error}</Notice>{kind === 'lessons' && <div className="view-switch" role="group" aria-label="Представление расписания"><Button variant="secondary" aria-pressed={!calendar} onClick={() => setCalendar(false)}>Список</Button><Button variant="secondary" aria-pressed={calendar} onClick={() => setCalendar(true)}>По дням</Button></div>}<ResourceState loading={remote.loading} error={remote.error} retry={remote.refresh}>{remote.data && <Card className="table-card"><div className="table-heading"><h2>{section.title}</h2><span className="muted">Всего: {remote.data.total}</span></div>{remote.data.items.length ? calendar ? <div className="card">{Array.from(new Set(remote.data.items.map(record => date(record.starts_at)))).map(day => <section className="calendar-day" key={day}><h2>{day}</h2><Table caption={`Занятия: ${day}`} columns={columns} items={remote.data!.items.filter(record => date(record.starts_at) === day)} rowKey={record => record.id} /></section>)}</div> : <Table caption={section.title} columns={columns} items={remote.data.items} rowKey={record => record.id} /> : <EmptyState title="Записей пока нет" description="Здесь появятся данные раздела. Если применены фильтры, попробуйте изменить их." />}<Pagination page={page} total={remote.data.total} pageSize={20} onChange={setPage} disabled={remote.loading} /></Card>}</ResourceState></div>{editor && <RecordEditor section={section} path={kind} record={editor === 'new' ? undefined : editor} close={() => setEditor(null)} saved={() => { setEditor(null); setSuccess('Изменения сохранены.'); remote.refresh(); }} />}{archive && <Modal title="Перенести запись в архив?" description="Запись сохранится в истории и не будет доступна для новых назначений." onClose={() => setArchive(null)} busy={busy}><Notice tone="error">{error}</Notice><div className="modal-footer"><Button variant="secondary" onClick={() => setArchive(null)} disabled={busy}>Отмена</Button><Button onClick={() => void archiveRecord()} loading={busy}>Перенести в архив</Button></div></Modal>}</>;
}

type Overview = { student: RecordData; parents: RecordData[]; groups: RecordData[]; upcoming_lessons: RecordData[]; attendance: { percent: number | null; present: number; late: number; absent: number; excused: number }; history: RecordData[] };
export function StudentOverview({ id, user }: { id: string; user: User }) {
  const [period, setPeriod] = useState(''); const remote = useRemote<Overview>(`/students/${id}/overview${period}`);
  const contactsAllowed = user.roles.some(role => ['admin', 'leader', 'curator'].includes(role));
  function apply(event: FormEvent<HTMLFormElement>) { event.preventDefault(); const data = new FormData(event.currentTarget); const params = new URLSearchParams(); for (const key of ['from', 'to']) if (data.get(key)) params.set(key, String(data.get(key))); setPeriod(params.size ? `?${params}` : ''); }
  return <><a className="back-link" href="#/students">К списку учеников</a><PageHeading title={remote.data?.student.name ?? 'Карточка ученика'} description="Student 360 · группы, ближайшие занятия и посещаемость." /><form className="filters" onSubmit={apply}><Input label="Период с" name="from" type="date" /><Input label="Период по" name="to" type="date" /><Button type="submit" variant="secondary">Применить</Button></form><ResourceState loading={remote.loading} error={remote.error} retry={remote.refresh}>{remote.data && <div className="stack"><div className="grid"><Card><h2>Об ученике</h2><dl className="detail-list"><div><dt>Статус</dt><dd><Status value={remote.data.student.status} /></dd></div><div><dt>Дата рождения</dt><dd>{date(remote.data.student.birth_date)}</dd></div><div><dt>Куратор</dt><dd>{text(remote.data.student.curator_name)}</dd></div></dl></Card><Card><h2>Посещаемость</h2><span className="stat-value">{remote.data.attendance.percent == null ? 'Нет данных' : `${remote.data.attendance.percent.toLocaleString('ru-RU', { maximumFractionDigits: 2 })}%`}</span><p className="muted">По завершённым занятиям за выбранный период.</p><dl className="detail-list">{(['present', 'late', 'absent', 'excused'] as const).map(key => <div key={key}><dt>{attendanceStatuses.find(item => item[0] === key)?.[1]}</dt><dd>{remote.data!.attendance[key]}</dd></div>)}</dl></Card></div>{contactsAllowed && <Card><h2>Родители и контакты</h2>{remote.data.parents.length ? <Table caption="Родители ученика" columns={[{ key: 'name', title: 'Родитель', render: record => text(record.name) }, { key: 'contact', title: 'Контакты', render: record => <>{record.phone && <span>{text(record.phone)}</span>}{record.email && <span>{text(record.email)}</span>}</> }, { key: 'relation', title: 'Представительство', render: record => <>{text(record.relationship)}{record.is_primary === true && <Badge>Основной контакт</Badge>}</> }]} items={remote.data.parents} rowKey={record => record.id} /> : <EmptyState title="Родители не указаны" />}</Card>}<Card><h2>Группы и программы</h2>{remote.data.groups.length ? <Table caption="Группы ученика" columns={[{ key: 'name', title: 'Группа', render: record => text(record.name) }, { key: 'program', title: 'Программа', render: record => text(record.program_name) }, { key: 'teacher', title: 'Педагог', render: record => text(record.teacher_name) }]} items={remote.data.groups} rowKey={record => record.id} /> : <EmptyState title="Текущих групп нет" />}</Card><Card><h2>Ближайшие занятия</h2>{remote.data.upcoming_lessons.length ? <Table caption="Ближайшие занятия ученика" columns={[{ key: 'topic', title: 'Занятие', render: record => text(record.topic) }, { key: 'start', title: 'Начало (МСК)', render: record => date(record.starts_at, true) }]} items={remote.data.upcoming_lessons} rowKey={record => record.id} /> : <EmptyState title="Занятия не запланированы" />}</Card>{contactsAllowed && <Card><h2>История изменений</h2>{remote.data.history.length ? <Table caption="История ученика" columns={[{ key: 'time', title: 'Время (МСК)', render: record => date(record.occurred_at, true) }, { key: 'action', title: 'Действие', render: record => text(record.action) }]} items={remote.data.history} rowKey={record => record.id} /> : <EmptyState title="Изменений пока нет" />}</Card>}</div>}</ResourceState></>;
}

type DashboardData = { active_students: number; active_groups: number; active_teachers: number; lessons_today: number; attendance_percent: number | null; overdue_tasks: number };
const dashboardMetrics = [['active_students', 'Активные ученики'], ['active_groups', 'Активные группы'], ['active_teachers', 'Активные педагоги'], ['lessons_today', 'Занятия сегодня'], ['attendance_percent', 'Посещаемость за период'], ['overdue_tasks', 'Просроченные задачи']] as const;
const metricDetails: Record<(typeof dashboardMetrics)[number][0], { icon: IconName; hint: string }> = {
  active_students: { icon: 'students', hint: 'Учатся и развиваются вместе с AIQ' },
  active_groups: { icon: 'groups', hint: 'Действующие учебные группы' },
  active_teachers: { icon: 'users', hint: 'Команда образовательного центра' },
  lessons_today: { icon: 'calendar', hint: 'Сегодня, по московскому времени' },
  attendance_percent: { icon: 'chart', hint: 'По завершённым занятиям' },
  overdue_tasks: { icon: 'tasks', hint: 'Требуют внимания команды' },
};

export function Dashboard() {
  const [today] = useState(() => moscowInput(new Date().toISOString()).slice(0, 10));
  const [monthEnd] = useState(() => { const [year, month] = today.split('-').map(Number); return new Date(Date.UTC(year, month, 0, 12)).toISOString().slice(0, 10); });
  const [period, setPeriod] = useState(`?date_from=${today.slice(0, 8)}01&date_to=${monthEnd}`);
  const [periodError, setPeriodError] = useState('');
  const remote = useRemote<DashboardData>(`/dashboard${period}`);
  const appliedPeriod = new URLSearchParams(period);
  const appliedFrom = appliedPeriod.get('date_from')!.split('-').reverse().join('.');
  const appliedTo = appliedPeriod.get('date_to')!.split('-').reverse().join('.');
  function apply(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const from = String(data.get('date_from')), to = String(data.get('date_to'));
    if (from > to) { setPeriodError('Начало периода не должно быть позже окончания.'); return; }
    setPeriodError('');
    const next = `?${new URLSearchParams({ date_from: from, date_to: to })}`;
    if (next === period) remote.refresh(); else setPeriod(next);
  }
  return <>
    <PageHeading title="Обзор" description="Ваш учебный центр в цифрах. Главное на сегодня и за выбранный период." action={<span className="date-label"><Icon name="calendar" />{new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', year: 'numeric', timeZone: 'Europe/Moscow' }).format(new Date(`${today}T12:00:00+03:00`))}</span>} />
    <section className="overview-banner" aria-label="Рабочий день"><div><span className="eyebrow">Люди. Обучение. Развитие.</span><h2>Учебный день<br />под контролем.</h2><p>От первого занятия до новых достижений —<br className="desktop-break" /> всё важное в одном пространстве.</p></div><a className="button button--primary" href="#/lessons">Открыть расписание<Icon name="arrow" /></a><div className="overview-art" aria-hidden="true"><span /><span /><span /></div></section>
    <div className="overview-section-heading"><h2>Показатели центра</h2><span className="muted">Данные вашего пространства</span></div>
    <form className="filters dashboard-filters" onSubmit={apply}>
      <Input label="Период с" type="date" name="date_from" required defaultValue={`${today.slice(0, 8)}01`} />
      <Input label="Период по" type="date" name="date_to" required defaultValue={monthEnd} />
      <Button variant="secondary" type="submit" disabled={remote.loading}>Показать</Button>
    </form>
    <p className="dashboard-period-summary muted">Период посещаемости: <strong>{appliedFrom} — {appliedTo}</strong> · МСК</p>
    <Notice tone="error">{periodError}</Notice>
    <ResourceState loading={remote.loading} error={remote.error} retry={remote.refresh}>
      {remote.data && <><div className="stats dashboard-stats">{dashboardMetrics.map(([key, label]) => <Card key={key} aria-label={label} className={`metric-card${key === 'active_students' ? ' metric-card--featured' : ''}${key === 'overdue_tasks' && remote.data!.overdue_tasks > 0 ? ' metric-card--attention' : ''}`}>
        <div className="metric-heading"><h2>{label}</h2><span className="metric-icon"><Icon name={metricDetails[key].icon} /></span></div>
        <span className="stat-value">{remote.data![key] == null ? 'Нет данных' : `${remote.data![key]!.toLocaleString('ru-RU', { maximumFractionDigits: 2 })}${key === 'attendance_percent' ? '%' : ''}`}</span>
        <p className="metric-hint">{metricDetails[key].hint}</p>
      </Card>)}</div>
      <div className="overview-details">
        <Card className="attendance-insight"><div className="insight-heading"><span className="metric-icon"><Icon name="chart" /></span><h2>Вовлечённость в обучение</h2></div><p className="muted">Посещаемость за выбранный период</p>{remote.data.attendance_percent == null ? <p className="insight-empty">Показатель появится после завершения занятий и внесения отметок.</p> : <><div className="attendance-scale"><strong>{remote.data.attendance_percent.toLocaleString('ru-RU', { maximumFractionDigits: 2 })}%</strong><span className="muted">из 100%</span></div><progress className="attendance-progress" max={100} value={remote.data.attendance_percent} aria-label="Доля присутствий и опозданий" /></>}<p className="insight-explanation">Присутствия и опоздания / все внесённые отметки × 100%. Без отметки и исключённые из журнала в расчёт не входят.</p></Card>
        <Card className="team-focus"><div className="insight-heading"><span className="metric-icon"><Icon name={remote.data.overdue_tasks > 0 ? 'tasks' : 'check'} /></span><h2>Фокус команды</h2></div><h3>{remote.data.overdue_tasks > 0 ? 'Вернём задачи в рабочий ритм' : 'Просроченных задач нет'}</h3><p className="muted">{remote.data.overdue_tasks > 0 ? 'Проверьте сроки и приоритеты задач, которые требуют внимания.' : 'Откройте задачи, чтобы спланировать следующие шаги команды.'}</p><a className="overview-text-link" href="#/tasks">Перейти к задачам<Icon name="arrow" /></a></Card>
      </div></>}
    </ResourceState>
  </>;
}
export function AuditLog() {
  const [page, setPage] = useState(1), [filters, setFilters] = useState(''), [filterError, setFilterError] = useState('');
  const remote = useRemote<ListData>(`/audit-logs?page=${page}&page_size=20${filters}`);
  function apply(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget), from = String(data.get('date_from')), to = String(data.get('date_to'));
    if (from && to && from > to) { setFilterError('Начало периода не должно быть позже окончания.'); return; }
    setFilterError('');
    const params = new URLSearchParams();
    for (const [key, value] of data) if (String(value)) params.set(key, String(value));
    const next = params.size ? `&${params}` : '';
    if (next === filters && page === 1) remote.refresh(); else { setFilters(next); setPage(1); }
  }
  const columns: Column<RecordData>[] = [
    { key: 'time', title: 'Время (МСК)', render: record => date(record.occurred_at, true) },
    { key: 'actor', title: 'Пользователь', render: record => <><span>{record.actor_name == null ? 'Система / неизвестный пользователь' : text(record.actor_name)}</span>{record.actor_id != null && <span className="muted">ID: {text(record.actor_id)}</span>}</> },
    { key: 'action', title: 'Действие', render: record => text(record.action) },
    { key: 'object_type', title: 'Тип объекта', render: record => text(record.object_type) },
    { key: 'object_id', title: 'ID объекта', render: record => text(record.object_id) },
    { key: 'fields', title: 'Изменённые поля', render: record => Array.isArray(record.changed_fields) && record.changed_fields.length ? record.changed_fields.map(field => <span key={String(field)}>{String(field)}</span>) : '—' },
    { key: 'request_id', title: 'Request ID', render: record => text(record.request_id) },
  ];
  return <>
    <PageHeading title="История действий" description="Критические изменения и действия команды. Записи доступны только для чтения; значения изменённых полей не сохраняются." />
    <form className="filters" onSubmit={apply}>
      <Input label="С даты" name="date_from" type="date" /><Input label="По дату" name="date_to" type="date" />
      <Input label="Автор (номер пользователя)" name="actor_id" type="number" min={1} step={1} />
      <Input label="Действие" name="action" maxLength={128} placeholder="Например, user.updated" />
      <Input label="Тип объекта" name="object_type" maxLength={128} placeholder="Например, student" />
      <Input label="ID объекта" name="object_id" type="number" min={1} step={1} />
      <Input label="Request ID" name="request_id" maxLength={128} />
      <Button type="submit" variant="secondary" disabled={remote.loading}>Применить</Button>
    </form>
    <Notice tone="error">{filterError}</Notice>
    <ResourceState loading={remote.loading} error={remote.error} retry={remote.refresh}>{remote.data && <Card className="table-card">
      <div className="table-heading"><h2>Журнал критических действий</h2><span className="muted">Всего: {remote.data.total}</span></div>
      {remote.data.items.length ? <Table caption="Журнал критических действий" items={remote.data.items} rowKey={record => record.id} columns={columns} /> : <EmptyState title="Действий за этот период нет" />}
      <Pagination page={page} pageSize={20} total={remote.data.total} onChange={setPage} disabled={remote.loading} />
    </Card>}</ResourceState>
  </>;
}
