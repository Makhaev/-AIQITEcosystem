import { ProgramTransfer } from './ProgramTransfer';
import { GroupProgress } from './LearningProgress';
import { useEffect, useState, type FormEvent } from 'react';
import { APIError, message, request, type User } from '../api';
import { Button, Card, EmptyState, Input, Modal, Notice, PageHeading, Select, Table, type Column } from '../components/ui';
import { RecordEditor, referenceItems, sections, Status } from './Education';

type Enrollment = { id: number; group_id: number; student_id: number; student_name: string; starts_on: string; ends_on: string | null };
type Group = { id: number; name: string; program_id: number; program_version_id: number; program_version_number: number; program_name: string; teacher_id: number | null; teacher_name: string | null; capacity: number; active_count: number; status: string; enrollments: Enrollment[]; [key: string]: unknown };
type Action = { kind: 'enroll' } | { kind: 'close' | 'transfer'; enrollment: Enrollment };
function today() { return new Intl.DateTimeFormat('en-CA', { timeZone: 'Europe/Moscow', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date()); }
function date(value: string) { return new Intl.DateTimeFormat('ru-RU', { timeZone: 'Europe/Moscow', dateStyle: 'medium' }).format(new Date(`${value}T00:00:00+03:00`)); }

function EnrollmentEditor({ group, action, close, saved }: { group: Group; action: Action; close: () => void; saved: (notice: string) => void }) {
  const [options, setOptions] = useState<{ id: number; name?: string; active_count?: unknown; capacity?: unknown }[]>([]);
  const [loading, setLoading] = useState(action.kind !== 'close'); const [lookupError, setLookupError] = useState('');
  const [error, setError] = useState(''); const [fields, setFields] = useState<Record<string, string>>({}); const [busy, setBusy] = useState(false);
  useEffect(() => {
    if (action.kind === 'close') return;
    const controller = new AbortController();
    referenceItems(action.kind === 'enroll' ? 'students?status=active' : 'groups?status=active', controller.signal)
      .then(items => { if (!controller.signal.aborted) setOptions(items.filter(item => action.kind === 'enroll' ? !group.enrollments.some(e => e.student_id === item.id && !e.ends_on) : item.id !== group.id)); })
      .catch(err => { if (!controller.signal.aborted) setLookupError(message(err)); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [action, group]);
  const title = action.kind === 'enroll' ? 'Зачислить ученика' : action.kind === 'close' ? 'Вывести ученика из группы' : 'Перевести ученика';
  const dateKey = action.kind === 'enroll' ? 'starts_on' : action.kind === 'close' ? 'ends_on' : 'transfers_on';
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const data = new FormData(event.currentTarget); const on = String(data.get(dateKey));
    setError(''); setFields({});
    if (action.kind !== 'enroll' && on < action.enrollment.starts_on) { setFields({ [dateKey]: 'Дата не может быть раньше начала зачисления.' }); return; }
    setBusy(true);
    try {
      const path = `/groups/${group.id}/enrollments`;
      if (action.kind === 'enroll') await request(path, 'POST', { student_id: Number(data.get('student_id')), starts_on: on });
      else if (action.kind === 'close') await request(`${path}/${action.enrollment.id}`, 'PATCH', { ends_on: on });
      else await request(`${path}/${action.enrollment.id}/transfer`, 'POST', { target_group_id: Number(data.get('target_group_id')), transfers_on: on });
      saved(action.kind === 'enroll' ? 'Ученик зачислен.' : action.kind === 'close' ? 'Зачисление завершено. История сохранена.' : 'Ученик переведён. История посещаемости сохранена.');
    } catch (err) { setError(message(err)); if (err instanceof APIError) setFields(err.fields); } finally { setBusy(false); }
  }
  return <Modal title={title} onClose={close} busy={busy} description={action.kind === 'enroll' ? undefined : `${action.enrollment.student_name}. История зачисления и посещаемости сохраняется.`}>
    <Notice tone="error">{lookupError || error}</Notice><form onSubmit={submit}>
      {action.kind === 'enroll' && <Select label="Ученик" name="student_id" required disabled={loading || busy || !!lookupError} error={fields.student_id}><option value="">{loading ? 'Загрузка…' : 'Выберите ученика'}</option>{options.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</Select>}
      {action.kind === 'transfer' && <Select label="Новая группа" name="target_group_id" required disabled={loading || busy || !!lookupError} error={fields.target_group_id}><option value="">{loading ? 'Загрузка…' : 'Выберите группу'}</option>{options.map(item => <option key={item.id} value={item.id}>{item.name} · {String(item.active_count)}/{String(item.capacity)}</option>)}</Select>}
      {!loading && action.kind !== 'close' && !options.length && !lookupError && <Notice>{action.kind === 'enroll' ? 'Нет доступных учеников для зачисления.' : 'Нет других активных групп для перевода.'}</Notice>}
      <Input label={action.kind === 'enroll' ? 'Дата начала' : action.kind === 'close' ? 'Дата окончания' : 'Дата перевода'} name={dateKey} type="date" required disabled={busy} defaultValue={action.kind === 'enroll' ? today() : [today(), action.enrollment.starts_on].sort().at(-1)} min={action.kind === 'enroll' ? undefined : action.enrollment.starts_on} error={fields[dateKey] || fields.ends_on} />
      <div className="modal-footer"><Button variant="secondary" onClick={close} disabled={busy}>Отмена</Button><Button type="submit" loading={busy} disabled={loading || !!lookupError || action.kind !== 'close' && !options.length}>{action.kind === 'enroll' ? 'Зачислить' : action.kind === 'close' ? 'Завершить зачисление' : 'Перевести'}</Button></div>
    </form>
  </Modal>;
}

export function GroupDetails({ id, user }: { id: string; user: User }) {
  const [card, setCard] = useState<Group | null>(null); const [loading, setLoading] = useState(true); const [error, setError] = useState('');
  const [version, setVersion] = useState(0); const [editor, setEditor] = useState(false); const [action, setAction] = useState<Action | null>(null); const [success, setSuccess] = useState('');
  const admin = user.roles.includes('admin');
  useEffect(() => { const controller = new AbortController(); setLoading(true); setError(''); setCard(null); request<Group>(`/groups/${id}`, 'GET', undefined, controller.signal).then(setCard).catch(err => { if (!controller.signal.aborted) setError(message(err)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); }); return () => controller.abort(); }, [id, version]);
  const columns: Column<Enrollment>[] = [
    { key: 'name', title: 'Ученик', render: e => e.student_name },
    { key: 'starts', title: 'Начало', render: e => date(e.starts_on) },
    { key: 'ends', title: 'Окончание', render: e => e.ends_on ? date(e.ends_on) : 'Обучается' },
    ...(admin ? [{ key: 'actions', title: 'Действия', render: (e: Enrollment) => e.ends_on ? 'Завершено' : <div className="row-actions"><Button variant="secondary" onClick={() => setAction({ kind: 'close', enrollment: e })} aria-label={`Вывести ${e.student_name}`}>Вывести</Button><Button variant="secondary" onClick={() => setAction({ kind: 'transfer', enrollment: e })} aria-label={`Перевести ${e.student_name}`}>Перевести</Button></div> }] : []),
  ];
  return <><a className="back-link" href="#/groups">← Группы</a><PageHeading title={card?.name ?? 'Группа'} description="Программа, педагог и история состава группы." action={admin && card && <Button variant="secondary" onClick={() => setEditor(true)}>Изменить группу</Button>} />
    <div className="stack"><Notice tone="success">{success}</Notice><Notice tone="error">{error}</Notice>{loading ? <Card><p role="status">Загружаем данные…</p></Card> : error ? <Card><Button variant="secondary" onClick={() => setVersion(v => v + 1)}>Повторить загрузку</Button></Card> : card && <>
      <Card><h2>Основные данные</h2><dl className="detail-list"><div><dt>Статус</dt><dd><Status value={card.status} /></dd></div><div><dt>Программа</dt><dd><a href={`#/programs/${card.program_id}`}>{card.program_name}</a></dd></div><div><dt>Основной педагог</dt><dd>{card.teacher_name ?? 'Не назначен'}</dd></div><div><dt>Заполнение группы</dt><dd>{card.active_count} / {card.capacity}</dd></div></dl>{card.status !== 'active' && <p className="muted">Для новых зачислений группа должна быть активной. История сохранена.</p>}</Card>
      <>{card.program_version_id > 0 && <ProgramTransfer groupID={card.id} programID={card.program_id} versionID={card.program_version_id} versionNumber={card.program_version_number} admin={admin} updated={() => { setSuccess('Программа группы переведена. Прежние планы сохранены.'); setVersion(v => v + 1); }} />}</><Card className="table-card"><div className="table-heading"><h2>История зачислений</h2>{admin && <Button onClick={() => setAction({ kind: 'enroll' })} disabled={card.status !== 'active' || card.active_count >= card.capacity}>Зачислить ученика</Button>}</div>{card.enrollments.length ? <Table caption="История зачислений" items={card.enrollments} columns={columns} rowKey={e => e.id} /> : <EmptyState title="Учеников пока нет" />}</Card>
      <GroupProgress key={`${id}/${version}/${user.roles.join(',')}`} id={Number(id)} />
    </>}</div>
    {editor && card && <RecordEditor section={sections.groups} path="groups" record={card} close={() => setEditor(false)} saved={() => { setEditor(false); setSuccess('Группа сохранена.'); setVersion(v => v + 1); }} />}
    {action && card && <EnrollmentEditor group={card} action={action} close={() => setAction(null)} saved={notice => { setAction(null); setSuccess(notice); setVersion(v => v + 1); }} />}
  </>;
}
