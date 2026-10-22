import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { APIError, message, request, type User } from '../api';
import { Badge, Button, Card, EmptyState, Input, Modal, Notice, PageHeading, Pagination, Select, Table, type Column } from '../components/ui';
import { referenceItems } from './Education';
import { RescheduleEditor, SnapshotEditor } from './LessonHistory';
import { LessonPlan, LessonPlanPicker, type PlanSnapshot } from './LessonPlan';

type Lesson = { id: number; group_id: number; group_name: string; program_id: number; program_name?: string; module_id: number | null; module_name: string | null; topic: string; teacher_id: number; teacher_name: string; teacher_user_id: number | null; starts_at: string; ends_at: string; location: string; status: string; roster_initialized_at: string | null; snapshot_state?: string; program_version_id?: number; version_module_key?: string; template_key?: string; plan_snapshot?: PlanSnapshot };
type Participant = { id: number; enrollment_id: number; student_id: number; name: string; status: string; comment: string };
type Journal = { lesson_id: number; lesson_date: string; initialized_at: string; has_marks: boolean; items: Participant[] };
type GroupOption = { id: number; name: string; program_id: number; teacher_id: number | null; program_version_id?: number };
type Choice = { id: number; name: string };
type Enrollment = { id: number; student_id: number; student_name: string; starts_on: string; ends_on: string | null };
const statuses = [['scheduled', 'Запланировано'], ['completed', 'Проведено'], ['cancelled', 'Отменено']];
const marks = [['unmarked', 'Не отмечен'], ['present', 'Присутствовал'], ['absent', 'Отсутствовал'], ['excused', 'Уважительная причина'], ['late', 'Опоздал']];
const moscow = 'Europe/Moscow';
function Status({ value }: { value: string }) { return <Badge tone={value === 'completed' ? 'success' : value === 'cancelled' ? 'danger' : 'warning'}>{statuses.find(([key]) => key === value)?.[1] ?? value}</Badge>; }
function date(value: string, withTime = true) { return new Intl.DateTimeFormat('ru-RU', { timeZone: moscow, dateStyle: 'medium', ...(withTime ? { timeStyle: 'short' as const } : {}) }).format(new Date(value)); }
function localTime(value?: string) {
  if (!value) return '';
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone: moscow, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).formatToParts(new Date(value));
  const get = (key: string) => parts.find(p => p.type === key)?.value;
  return `${get('year')}-${get('month')}-${get('day')}T${get('hour')}:${get('minute')}`;
}
// Use IANA offset data for the chosen date, including historical Moscow offsets.
function toISO(value: string) {
  const offset = new Intl.DateTimeFormat('en', { timeZone: moscow, timeZoneName: 'longOffset' }).formatToParts(new Date(`${value}Z`)).find(p => p.type === 'timeZoneName')!.value.replace('GMT', '') || '+00:00';
  return new Date(`${value}${offset}`).toISOString();
}
function useRemote<T>(path: string) {
  const [data, setData] = useState<T | null>(null); const [error, setError] = useState(''); const [loading, setLoading] = useState(true); const [version, setVersion] = useState(0);
  useEffect(() => { const controller = new AbortController(); setLoading(true); setError(''); setData(null); request<T>(path, 'GET', undefined, controller.signal).then(setData).catch(err => { if (!controller.signal.aborted) setError(message(err)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); }); return () => controller.abort(); }, [path, version]);
  return { data, error, loading, refresh: () => setVersion(v => v + 1) };
}

function LessonEditor({ lesson, close, saved }: { lesson?: Lesson; close: () => void; saved: () => void }) {
  const [planModule, setPlanModule] = useState(lesson?.version_module_key ?? ''); const [template, setTemplate] = useState(lesson?.template_key ?? ''); const [topic, setTopic] = useState(lesson?.topic ?? '');
  const historical = lesson?.status === 'completed' || lesson?.snapshot_state === 'captured' || lesson?.snapshot_state === 'legacy_unknown';
  const [groups, setGroups] = useState<GroupOption[]>([]); const [teachers, setTeachers] = useState<Choice[]>([]); const [modules, setModules] = useState<Choice[]>([]);
  const [groupID, setGroupID] = useState(lesson ? String(lesson.group_id) : ''); const [teacherID, setTeacherID] = useState(lesson ? String(lesson.teacher_id) : ''); const [moduleID, setModuleID] = useState(lesson?.module_id ? String(lesson.module_id) : '');
  const [loading, setLoading] = useState(true); const [modulesLoading, setModulesLoading] = useState(false); const [referenceError, setReferenceError] = useState(''); const [moduleError, setModuleError] = useState('');
  const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [fields, setFields] = useState<Record<string, string>>({});
  useEffect(() => {
    const controller = new AbortController();
    Promise.all([referenceItems('groups?status=active', controller.signal), referenceItems('teachers?status=active', controller.signal)])
      .then(([groupItems, teacherItems]) => {
        if (controller.signal.aborted) return;
        const groupChoices = groupItems.map(g => ({ id: g.id, name: String(g.name), program_id: Number(g.program_id), program_version_id: Number(g.program_version_id) || undefined, teacher_id: g.teacher_id ? Number(g.teacher_id) : null }));
        const teacherChoices = teacherItems.map(t => ({ id: t.id, name: String(t.name) }));
        if (lesson && !groupChoices.some(g => g.id === lesson.group_id)) groupChoices.push({ id: lesson.group_id, name: lesson.group_name, program_id: lesson.program_id, program_version_id: lesson.program_version_id, teacher_id: lesson.teacher_id });
        if (lesson && !teacherChoices.some(t => t.id === lesson.teacher_id)) teacherChoices.push({ id: lesson.teacher_id, name: lesson.teacher_name });
        setGroups(groupChoices); setTeachers(teacherChoices);
      }).catch(err => { if (!controller.signal.aborted) setReferenceError(message(err)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [lesson]);
  const programID = groups.find(g => String(g.id) === groupID)?.program_id;
  useEffect(() => {
    if (!programID) { setModules([]); return; }
    const controller = new AbortController(); setModulesLoading(true); setModuleError('');
    request<{ modules: Choice[] }>(`/programs/${programID}`, 'GET', undefined, controller.signal).then(card => { if (!controller.signal.aborted) setModules(card.modules); }).catch(err => { if (!controller.signal.aborted) setModuleError(message(err)); }).finally(() => { if (!controller.signal.aborted) setModulesLoading(false); });
    return () => controller.abort();
  }, [programID]);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const data = new FormData(event.currentTarget); const startInput = String(data.get('starts_at')); const endInput = String(data.get('ends_at'));
    const startsAt = lesson && startInput === localTime(lesson.starts_at) ? lesson.starts_at : toISO(startInput);
    const endsAt = lesson && endInput === localTime(lesson.ends_at) ? lesson.ends_at : toISO(endInput);
    if (new Date(endsAt).getTime() <= new Date(startsAt).getTime()) { setFields({ ends_at: 'Окончание должно быть позже начала.' }); return; }
    setBusy(true); setError(''); setFields({});
    try { await request(lesson ? `/lessons/${lesson.id}` : '/lessons', lesson ? 'PATCH' : 'POST', { group_id: Number(groupID), module_id: moduleID ? Number(moduleID) : null, topic: topic.trim(), version_module_key: planModule, template_key: template, teacher_id: Number(teacherID), starts_at: startsAt, ends_at: endsAt, location: String(data.get('location')).trim(), status: String(data.get('status')) }); saved(); }
    catch (err) { setError(message(err)); if (err instanceof APIError) setFields(err.fields); } finally { setBusy(false); }
  }
  return <Modal title={lesson ? 'Изменить занятие' : 'Создать занятие'} description="Начало и окончание указаны по московскому времени." onClose={close} busy={busy}><Notice tone="error">{referenceError || moduleError || error}</Notice><form onSubmit={submit}>
    <Select label="Группа" required value={groupID} disabled={busy || loading || !!lesson?.roster_initialized_at} error={fields.group_id} hint={lesson?.roster_initialized_at ? 'Группа сохранённого журнала зафиксирована.' : undefined} onChange={event => { setGroupID(event.target.value); setModuleID(''); setPlanModule(''); setTemplate(''); const group = groups.find(g => String(g.id) === event.target.value); if (group?.teacher_id && teachers.some(t => t.id === group.teacher_id)) setTeacherID(String(group.teacher_id)); }}><option value="">Выберите группу</option>{groups.map(g => <option key={g.id} value={g.id}>{g.name}</option>)}</Select>
    <Select label="Модуль" value={moduleID} disabled={busy || loading || modulesLoading || !programID || !!moduleError || historical} error={fields.module_id} onChange={event => setModuleID(event.target.value)}><option value="">Без модуля</option>{modules.map(m => <option key={m.id} value={m.id}>{m.name}</option>)}</Select>
    <LessonPlanPicker programID={lesson && String(lesson.group_id) === groupID ? lesson.program_id : Number(programID)} versionID={lesson && String(lesson.group_id) === groupID ? lesson.program_version_id : groups.find(g => String(g.id) === groupID)?.program_version_id} moduleKey={planModule} templateKey={template} disabled={busy || historical || !!lesson && new Date(lesson.starts_at).getTime() <= Date.now()} change={(m, t, title) => { setPlanModule(m); setTemplate(t); if (title) setTopic(title); }} /><Input label="Тема" name="topic" required maxLength={200} value={topic} onChange={e => setTopic(e.target.value)} readOnly={historical || !!template} disabled={busy} error={fields.topic} hint={historical ? 'Исправьте названия и тему через исторический снимок завершённого занятия.' : undefined} />
    <Select label="Педагог" required value={teacherID} disabled={busy || loading} error={fields.teacher_id} onChange={event => setTeacherID(event.target.value)}><option value="">Выберите педагога</option>{teachers.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}</Select>
    <Input label="Начало (МСК)" name="starts_at" type="datetime-local" required defaultValue={localTime(lesson?.starts_at)} disabled={busy} error={fields.starts_at} hint={lesson?.roster_initialized_at ? 'После открытия журнала можно менять время в пределах исходной московской даты.' : undefined} />
    <Input label="Окончание (МСК)" name="ends_at" type="datetime-local" required defaultValue={localTime(lesson?.ends_at)} disabled={busy} error={fields.ends_at} />
    <Input label="Место проведения" name="location" maxLength={500} defaultValue={lesson?.location} disabled={busy} error={fields.location} />
    <Select label="Статус" name="status" required defaultValue={lesson?.status ?? 'scheduled'} disabled={busy} error={fields.status}>{statuses.map(([key, label]) => <option key={key} value={key}>{label}</option>)}</Select>
    <div className="modal-footer"><Button variant="secondary" onClick={close} disabled={busy}>Отмена</Button><Button type="submit" loading={busy} disabled={loading || modulesLoading || !!referenceError || !!moduleError}>Сохранить занятие</Button></div>
  </form></Modal>;
}

export function Lessons({ user }: { user: User }) {
  const [page, setPage] = useState(1); const [filters, setFilters] = useState(''); const [calendar, setCalendar] = useState(false); const [editor, setEditor] = useState<Lesson | 'new' | null>(null); const [success, setSuccess] = useState('');
  const [groups, setGroups] = useState<Choice[]>([]); const [teachers, setTeachers] = useState<Choice[]>([]); const [lookupError, setLookupError] = useState('');
  const remote = useRemote<{ items: Lesson[]; total: number }>(`/lessons?page=${page}&page_size=20${filters}`); const admin = user.roles.includes('admin');
  useEffect(() => {
    const controller = new AbortController();
    Promise.all([referenceItems('groups', controller.signal), user.roles.includes('curator') && !user.roles.some(r => r === 'admin' || r === 'leader' || r === 'teacher') ? Promise.resolve([]) : referenceItems('teachers', controller.signal)])
      .then(([g, t]) => { if (!controller.signal.aborted) { setGroups(g.map(v => ({ id: v.id, name: String(v.name) }))); setTeachers(t.map(v => ({ id: v.id, name: String(v.name) }))); } }).catch(err => { if (!controller.signal.aborted) setLookupError(message(err)); });
    return () => controller.abort();
  }, [user.roles.join(',')]);
  function filter(event: FormEvent<HTMLFormElement>) { event.preventDefault(); const params = new URLSearchParams(); for (const [key, value] of new FormData(event.currentTarget)) if (String(value).trim()) params.set(key, String(value).trim()); setFilters(params.size ? `&${params}` : ''); setPage(1); }
  const columns: Column<Lesson>[] = [
    { key: 'topic', title: 'Тема', render: l => <><a href={`#/lessons/${l.id}`}>{l.topic}</a>{l.snapshot_state === 'legacy_unknown' && <span className="muted">Исторический снимок отсутствует; названия из текущих справочников.</span>}</> }, { key: 'group', title: 'Группа', render: l => l.group_name }, { key: 'module', title: 'Модуль', render: l => l.module_name ?? 'Без модуля' },
    { key: 'teacher', title: 'Педагог', render: l => l.teacher_name }, { key: 'time', title: 'Время (МСК)', render: l => <>{date(l.starts_at)}<br />{date(l.ends_at)}</> }, { key: 'location', title: 'Место', render: l => l.location || 'Не указано' }, { key: 'status', title: 'Статус', render: l => <Status value={l.status} /> },
    ...(admin ? [{ key: 'actions', title: 'Действия', render: (l: Lesson) => <Button variant="secondary" aria-label={`Изменить занятие ${l.topic}`} onClick={() => setEditor(l)}>Изменить</Button> }] : []),
  ];
  const items = remote.data?.items ?? []; const days = Array.from(new Set(items.map(l => localTime(l.starts_at).slice(0, 10))));
  return <><PageHeading title="Расписание" description="Занятия и журнал посещаемости. Время указано по Москве." action={admin && <Button onClick={() => setEditor('new')} disabled={remote.loading || !!remote.error}>Добавить занятие</Button>} />
    <form className="filters" onSubmit={filter}><Input label="Поиск по теме" name="search" maxLength={200} /><Select label="Статус" name="status"><option value="">Все статусы</option>{statuses.map(([key, label]) => <option key={key} value={key}>{label}</option>)}</Select><Input label="С даты" name="date_from" type="date" /><Input label="По дату" name="date_to" type="date" /><Select label="Группа" name="group_id"><option value="">Все группы</option>{groups.map(g => <option key={g.id} value={g.id}>{g.name}</option>)}</Select><Select label="Педагог" name="teacher_id"><option value="">Все педагоги</option>{teachers.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}</Select><Button type="submit" variant="secondary">Применить</Button></form>
    <div className="stack"><Notice tone="success">{success}</Notice><Notice tone="error">{lookupError || remote.error}</Notice><div className="view-switch" role="group" aria-label="Представление расписания"><Button variant="secondary" aria-pressed={!calendar} onClick={() => setCalendar(false)}>Список</Button><Button variant="secondary" aria-pressed={calendar} onClick={() => setCalendar(true)}>По дням</Button></div>
    <Card className="table-card">{remote.loading ? <p role="status">Загружаем данные…</p> : remote.error ? <Button variant="secondary" onClick={remote.refresh}>Повторить загрузку</Button> : items.length ? calendar ? <div className="card">{days.map(day => <section className="calendar-day" key={day}><h2>{date(`${day}T12:00:00+03:00`, false)}</h2><Table caption={`Занятия: ${day}`} items={items.filter(l => localTime(l.starts_at).startsWith(day))} columns={columns} rowKey={l => l.id} /></section>)}</div> : <Table caption="Расписание" items={items} columns={columns} rowKey={l => l.id} /> : <EmptyState title="Занятий пока нет" description="Добавьте занятие или измените фильтры." />}{remote.data && <Pagination page={page} pageSize={20} total={remote.data.total} onChange={setPage} disabled={remote.loading} />}</Card></div>
    {editor && <LessonEditor lesson={editor === 'new' ? undefined : editor} close={() => setEditor(null)} saved={() => { setEditor(null); setSuccess('Занятие сохранено.'); remote.refresh(); }} />}
  </>;
}

function RosterEditor({ lesson, journal, close, saved }: { lesson: Lesson; journal: Journal; close: () => void; saved: () => void }) {
  const [choices, setChoices] = useState<Enrollment[]>([]); const [selected, setSelected] = useState(journal.items.map(i => i.enrollment_id)); const [loading, setLoading] = useState(true); const [busy, setBusy] = useState(false); const [lookupError, setLookupError] = useState(''); const [error, setError] = useState('');
  useEffect(() => { const controller = new AbortController(); request<{ enrollments: Enrollment[] }>(`/groups/${lesson.group_id}`, 'GET', undefined, controller.signal).then(card => {
    if (controller.signal.aborted) return;
    const pupils = new Map<number, Enrollment>(); card.enrollments.forEach(e => { if (!pupils.has(e.student_id)) pupils.set(e.student_id, e); });
    journal.items.forEach(i => pupils.set(i.student_id, { id: i.enrollment_id, student_id: i.student_id, student_name: i.name, starts_on: journal.lesson_date, ends_on: null }));
    setChoices(Array.from(pupils.values()));
  }).catch(err => { if (!controller.signal.aborted) setLookupError(message(err)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); }); return () => controller.abort(); }, [lesson.group_id, journal]);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setError('');
    try { await request(`/lessons/${lesson.id}/roster`, 'PUT', { enrollment_ids: selected, reason: String(new FormData(event.currentTarget).get('reason')).trim() }); saved(); } catch (err) { setError(message(err)); } finally { setBusy(false); }
  }
  return <Modal title="Корректировать состав журнала" description="Изменение состава и причина попадут в историю действий. Прежние отметки сохраняются при исключении и повторном включении ученика." onClose={close} busy={busy}><Notice tone="error">{lookupError || error}</Notice><form onSubmit={submit}>{loading ? <p role="status">Загрузка учеников…</p> : <fieldset disabled={busy}><legend>Ученики из истории группы</legend>{choices.map(e => <label className="checkbox" key={e.id}><input type="checkbox" checked={selected.includes(e.id)} onChange={event => setSelected(values => event.target.checked ? [...values, e.id] : values.filter(id => id !== e.id))} />{e.student_name}</label>)}{!choices.length && <p>В истории группы пока нет учеников.</p>}</fieldset>}<Input label="Причина корректировки" name="reason" required maxLength={1000} disabled={busy} /><div className="modal-footer"><Button variant="secondary" onClick={close} disabled={busy}>Отмена</Button><Button type="submit" loading={busy} disabled={loading || !!lookupError}>Сохранить состав</Button></div></form></Modal>;
}

export function LessonDetail({ id, user }: { id: string; user: User }) {
  const [reschedule, setReschedule] = useState(false); const [snapshot, setSnapshot] = useState(false);
  const lesson = useRemote<Lesson>(`/lessons/${id}`); const journal = useRemote<Journal>(`/lessons/${id}/attendance`); const [entries, setEntries] = useState<Participant[]>([]); const [editor, setEditor] = useState(false); const [roster, setRoster] = useState(false); const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [success, setSuccess] = useState(''); const [confirm, setConfirm] = useState<'complete' | 'cancel' | null>(null);
  useEffect(() => { if (journal.data) setEntries(journal.data.items); }, [journal.data]);
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    setNow(Date.now());
    if (!lesson.data || new Date(lesson.data.ends_at).getTime() <= Date.now()) return;
    const end = new Date(lesson.data.ends_at).getTime();
    const timer = window.setInterval(() => { const current = Date.now(); setNow(current); if (current >= end) window.clearInterval(timer); }, 1000);
    return () => window.clearInterval(timer);
  }, [lesson.data?.starts_at, lesson.data?.ends_at]);
  const admin = user.roles.includes('admin'); const ownTeacher = user.roles.includes('teacher') && lesson.data?.teacher_user_id === user.id;
  const ready = !!journal.data && !journal.loading && !journal.error;
  const dirty = !!journal.data && (entries.length !== journal.data.items.length || entries.some(e => { const saved = journal.data!.items.find(i => i.student_id === e.student_id); return !saved || e.status !== saved.status || e.comment !== saved.comment; }));
  const hasMarks = journal.data?.has_marks ?? journal.data?.items.some(e => e.status !== 'unmarked') ?? false;
  const ended = !!lesson.data && new Date(lesson.data.ends_at).getTime() <= now;
  const canMark = ready && !!lesson.data && new Date(lesson.data.starts_at).getTime() <= now && (admin || ownTeacher) && lesson.data.status !== 'cancelled' && (lesson.data.status !== 'completed' || admin);
  const canComplete = ready && lesson.data?.status === 'scheduled' && (admin || ownTeacher) && ended && !dirty && journal.data!.items.every(e => e.status !== 'unmarked');
  const canCancel = ready && admin && lesson.data?.status === 'scheduled' && !hasMarks && !dirty;
  const editableLesson = useMemo(() => lesson.data ? { ...lesson.data, roster_initialized_at: journal.data?.initialized_at ?? lesson.data.roster_initialized_at } : null, [lesson.data, journal.data?.initialized_at]);
  function refresh() { lesson.refresh(); journal.refresh(); }
  async function save(event: FormEvent<HTMLFormElement>) { event.preventDefault(); setBusy(true); setError(''); try { await request(`/lessons/${id}/attendance`, 'PUT', { items: entries.map(({ student_id, status, comment }) => ({ student_id, status, comment })) }); setSuccess('Посещаемость сохранена.'); journal.refresh(); } catch (err) { setError(message(err)); if (err instanceof APIError && [403, 409].includes(err.status)) refresh(); } finally { setBusy(false); } }
  async function transition() { if (!confirm) return; setBusy(true); setError(''); try { await request(`/lessons/${id}/${confirm}`, 'POST'); setConfirm(null); setSuccess('Статус занятия сохранён.'); refresh(); } catch (err) { setError(message(err)); if (err instanceof APIError && [403, 409].includes(err.status)) refresh(); } finally { setBusy(false); } }
  return <><a className="back-link" href="#/lessons">← Расписание</a><PageHeading title={lesson.data?.topic ?? 'Занятие и посещаемость'} description="Данные занятия и сохранённый состав журнала." action={admin && lesson.data && <Button variant="secondary" onClick={() => setEditor(true)}>Изменить занятие</Button>} />
    {lesson.data && <div className="row-actions">{admin && lesson.data.status === 'scheduled' && <Button variant="secondary" disabled={dirty || !ready || busy} onClick={() => setReschedule(true)}>Перенести с обновлением состава</Button>}{admin && lesson.data.status === 'completed' && <Button variant="secondary" onClick={() => setSnapshot(true)}>Исторический снимок</Button>}{lesson.data.status === 'completed' && lesson.data.snapshot_state === 'legacy_unknown' && <Notice>Исторический снимок отсутствует. Названия показаны из текущих справочников.</Notice>}</div>}
    <div className="stack"><Notice tone="success">{success}</Notice><Notice tone="error">{!confirm && error || lesson.error || journal.error}</Notice>{lesson.loading ? <Card><p role="status">Загружаем данные…</p></Card> : lesson.error ? <Card><Button variant="secondary" onClick={refresh}>Повторить загрузку</Button></Card> : lesson.data && <>
      <Card><h2>О занятии</h2><dl className="detail-list"><div><dt>Статус</dt><dd><Status value={lesson.data.status} /></dd></div><div><dt>Группа</dt><dd><a href={`#/groups/${lesson.data.group_id}`}>{lesson.data.group_name}</a></dd></div><div><dt>Программа</dt><dd>{lesson.data.program_name || 'Не указано'}</dd></div><div><dt>Модуль</dt><dd>{lesson.data.module_name ?? 'Без модуля'}</dd></div><div><dt>Педагог</dt><dd>{lesson.data.teacher_name}</dd></div><div><dt>Начало (МСК)</dt><dd>{date(lesson.data.starts_at)}</dd></div><div><dt>Окончание (МСК)</dt><dd>{date(lesson.data.ends_at)}</dd></div><div><dt>Место проведения</dt><dd>{lesson.data.location || 'Не указано'}</dd></div></dl><div className="row-actions">{(admin || ownTeacher) && lesson.data.status === 'scheduled' && <Button variant="secondary" disabled={!canComplete || busy} onClick={() => setConfirm('complete')}>Завершить занятие</Button>}{admin && lesson.data.status === 'scheduled' && <Button variant="secondary" disabled={!canCancel || busy} onClick={() => setConfirm('cancel')}>Отменить занятие</Button>}</div>{(admin || ownTeacher) && lesson.data.status === 'scheduled' && <>{!ended && <p className="muted">Завершение доступно после окончания занятия.</p>}{dirty ? <p className="muted">Сначала сохраните изменения журнала.</p> : ready && journal.data!.items.some(e => e.status === 'unmarked') && <p className="muted">Для завершения сохраните отметку для каждого ученика.</p>}{admin && hasMarks && <p className="muted">Занятие с внесёнными отметками нельзя отменить.</p>}</>}</Card>
      <LessonPlan programID={lesson.data.program_id} versionID={lesson.data.program_version_id} snapshot={lesson.data.plan_snapshot} /><Card><div className="table-heading"><h2>Журнал посещаемости</h2>{admin && journal.data && <Button variant="secondary" onClick={() => setRoster(true)}>Корректировать состав</Button>}</div>{journal.loading ? <p role="status">Сохраняем состав журнала…</p> : journal.error ? <Button variant="secondary" onClick={journal.refresh}>Повторить загрузку журнала</Button> : journal.data && <><p className="muted">Состав сохранён на дату занятия{journal.data.lesson_date ? `: ${date(`${journal.data.lesson_date}T12:00:00+03:00`, false)}` : ''}. Изменения группы не меняют журнал.</p>{lesson.data.status === 'cancelled' && <Notice>Занятие отменено. Новые отметки недоступны.</Notice>}{!canMark && lesson.data.status === 'scheduled' && new Date(lesson.data.starts_at).getTime() > Date.now() && <Notice>Посещаемость можно отметить после начала занятия.</Notice>}{entries.length ? <form onSubmit={save}><Button variant="secondary" onClick={() => setEntries(values => values.map(e => ({ ...e, status: 'present' })))} disabled={!canMark || busy}>Все присутствовали</Button>{entries.map(entry => <div className="attendance-entry" key={entry.student_id}><strong>{entry.name}</strong><Select label={`Посещаемость: ${entry.name}`} value={entry.status} disabled={!canMark || busy} onChange={event => setEntries(values => values.map(e => e.student_id === entry.student_id ? { ...e, status: event.target.value } : e))}>{marks.map(([key, label]) => <option key={key} value={key}>{label}</option>)}</Select><Input label={`Комментарий: ${entry.name}`} value={entry.comment} maxLength={500} disabled={!canMark || busy} onChange={event => setEntries(values => values.map(e => e.student_id === entry.student_id ? { ...e, comment: event.target.value } : e))} /></div>)}<Button type="submit" loading={busy} disabled={!canMark}>Сохранить посещаемость</Button></form> : <EmptyState title="В сохранённом составе нет учеников" />}</>}</Card>
    </>}</div>
    {editor && editableLesson && <LessonEditor lesson={editableLesson} close={() => setEditor(false)} saved={() => { setEditor(false); setSuccess('Занятие сохранено.'); refresh(); }} />}
    {reschedule && lesson.data && <RescheduleEditor lesson={lesson.data} close={() => setReschedule(false)} saved={() => { setReschedule(false); setSuccess('Занятие перенесено, состав обновлён.'); refresh(); }} />}
    {snapshot && lesson.data && <SnapshotEditor lesson={lesson.data} close={() => setSnapshot(false)} saved={() => { setSnapshot(false); setSuccess('Исторический снимок исправлен, предыдущая версия сохранена.'); refresh(); }} />}
    {roster && lesson.data && journal.data && <RosterEditor lesson={lesson.data} journal={journal.data} close={() => setRoster(false)} saved={() => { setRoster(false); setSuccess('Состав исправлен. Изменение записано в историю действий.'); journal.refresh(); }} />}
    {confirm && <Modal title={confirm === 'complete' ? 'Завершить занятие?' : 'Отменить занятие?'} description={confirm === 'cancel' ? 'Отменённое занятие освобождает время группы и педагога. Журнал сохраняется.' : 'Все отметки сохранены. После завершения исправить журнал сможет только администратор.'} busy={busy} onClose={() => setConfirm(null)}><Notice tone="error">{error}</Notice><div className="modal-footer"><Button variant="secondary" onClick={() => setConfirm(null)} disabled={busy}>Назад</Button><Button onClick={() => void transition()} loading={busy} disabled={confirm === 'complete' ? !canComplete : !canCancel}>Подтвердить</Button></div></Modal>}
  </>;
}
