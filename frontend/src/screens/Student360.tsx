import { useState, type FormEvent } from 'react';
import { Badge, Button, Card, EmptyState, Input, Notice, Table, type Column } from '../components/ui';

type Enrollment = { id: number; group_id: number; group_name: string; group_status: string; program_id: number; program_name: string; teacher_id: number | null; teacher_name: string | null; starts_on: string; ends_on: string | null };
type Lesson = { id: number; group_id: number; group_name: string; topic: string; teacher_name: string; starts_at: string; ends_at: string; location: string };
type AttendanceItem = Lesson & { status: 'unmarked' | 'present' | 'late' | 'absent' | 'excused'; comment: string };
type Change = { id: number; occurred_at: string; actor_name: string | null; action: string; object_type: string; object_id: number | null };
export type OverviewBlocks = { full_access: boolean; enrollments: Enrollment[]; upcoming_lessons: Lesson[]; attendance: { from: string; to: string; present: number; late: number; absent: number; excused: number; unmarked: number; percent: number | null; items: AttendanceItem[] }; history?: Change[] };
const date = (value: string) => value.split('-').reverse().join('.');
const timestamp = (value: string) => new Intl.DateTimeFormat('ru-RU', { timeZone: 'Europe/Moscow', dateStyle: 'short', timeStyle: 'short' }).format(new Date(value));
const statuses = { unmarked: 'Не отмечен', present: 'Присутствовал', late: 'Опоздал', absent: 'Отсутствовал', excused: 'Уважительная причина' };
const actions: Record<string, string> = { 'student.created': 'Создана карточка ученика', 'student.updated': 'Изменены данные ученика или куратор', 'student.archived': 'Ученик перенесён в архив', 'student.restored': 'Ученик восстановлен', 'student.parent_linked': 'Добавлена связь с родителем', 'student.parent_link_updated': 'Изменено представительство или основной контакт', 'parent.created': 'Создана карточка родителя', 'parent.updated': 'Изменены данные или контакты родителя', 'parent.archived': 'Родитель перенесён в архив', 'parent.restored': 'Родитель восстановлен', 'enrollment.created': 'Ученик зачислен в группу', 'enrollment.closed': 'Завершено зачисление в группу', 'enrollment.transferred_out': 'Ученик переведён из группы', 'enrollment.transferred_in': 'Ученик переведён в группу', 'attendance.updated': 'Сохранена посещаемость занятия', 'attendance.corrected': 'Исправлен завершённый журнал', 'lesson.roster_corrected': 'Скорректирован состав журнала' };

export function Student360Blocks({ data, applyPeriod }: { data: OverviewBlocks; applyPeriod: (query: string) => void }) {
  const [periodError, setPeriodError] = useState('');
  function filter(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const form = new FormData(event.currentTarget);
    if (String(form.get('from')) > String(form.get('to'))) { setPeriodError('Начало периода не должно быть позже окончания.'); return; }
    setPeriodError('');
    applyPeriod(new URLSearchParams({ from: String(form.get('from')), to: String(form.get('to')) }).toString());
  }
  const enrollments: Column<Enrollment>[] = [
    { key: 'group', title: 'Группа', render: v => <><a href={`#/groups/${v.group_id}`}>{v.group_name}</a>{v.group_status === 'archived' && <Badge>Группа в архиве</Badge>}</> },
    { key: 'program', title: 'Программа', render: v => <a href={`#/programs/${v.program_id}`}>{v.program_name}</a> },
    { key: 'teacher', title: 'Основной педагог', render: v => v.teacher_name || 'Не назначен' },
    { key: 'period', title: 'Зачисление', render: v => <><span>{date(v.starts_on)} — {v.ends_on ? date(v.ends_on) : 'по настоящее время'}</span><Badge tone={v.ends_on ? 'neutral' : 'success'}>{v.ends_on ? 'Завершено' : 'Открыто'}</Badge></> },
  ];
  const lessons: Column<Lesson>[] = [
    { key: 'topic', title: 'Занятие', render: v => <><a href={`#/lessons/${v.id}`}>{v.topic}</a><span className="muted">{v.group_name}</span></> },
    { key: 'time', title: 'Время (Москва)', render: v => <><span>{timestamp(v.starts_at)}</span><span>до {timestamp(v.ends_at)}</span></> },
    { key: 'teacher', title: 'Педагог', render: v => v.teacher_name },
    { key: 'location', title: 'Место', render: v => v.location || 'Не указано' },
  ];
  const attendance: Column<AttendanceItem>[] = [
    ...lessons.slice(0, 3),
    { key: 'status', title: 'Посещаемость', render: v => <Badge tone={v.status === 'present' || v.status === 'late' ? 'success' : v.status === 'absent' ? 'danger' : 'neutral'}>{statuses[v.status]}</Badge> },
    { key: 'comment', title: 'Комментарий', render: v => v.comment || '—' },
  ];
  const history: Column<Change>[] = [
    { key: 'date', title: 'Дата (Москва)', render: v => timestamp(v.occurred_at) },
    { key: 'action', title: 'Изменение', render: v => actions[v.action] || 'Изменена связанная запись' },
    { key: 'actor', title: 'Автор', render: v => v.actor_name || 'Система' },
  ];
  return <>
    <Card className="table-card"><div className="table-heading"><h2>Группы, программы и педагоги</h2></div>{data.enrollments.length ? <Table caption="Зачисления ученика" columns={enrollments} items={data.enrollments} rowKey={v => v.id} /> : <EmptyState title="Зачислений пока нет" />}</Card>
    <Card className="table-card"><div className="table-heading"><h2>Ближайшие занятия</h2><span className="muted">До 10 следующих занятий</span></div>{data.upcoming_lessons.length ? <Table caption="Ближайшие занятия ученика" columns={lessons} items={data.upcoming_lessons} rowKey={v => v.id} /> : <EmptyState title="Ближайших занятий нет" />}</Card>
    <Card className="table-card"><div className="table-heading"><h2>Посещаемость за период</h2></div>
      <Notice tone="error">{periodError}</Notice>
      <form className="filters" onSubmit={filter} key={`${data.attendance.from}/${data.attendance.to}`}><Input label="Период с" type="date" name="from" defaultValue={data.attendance.from} required /><Input label="Период по" type="date" name="to" defaultValue={data.attendance.to} required /><Button type="submit" variant="secondary">Показать посещаемость</Button></form>
      <div className="student-attendance-summary"><p className="student-attendance-percent" aria-label="Процент посещаемости">{data.attendance.percent == null ? 'Нет данных' : `${new Intl.NumberFormat('ru-RU', { maximumFractionDigits: 2 }).format(data.attendance.percent)}%`}</p>
        <p>Присутствовал: {data.attendance.present} · Опоздал: {data.attendance.late} · Отсутствовал: {data.attendance.absent} · Уважительная причина: {data.attendance.excused}</p><p className="muted">Только завершённые занятия. Присутствия и опоздания / все внесённые отметки × 100%.</p>{data.attendance.unmarked > 0 && <p className="muted">Без отметки: {data.attendance.unmarked}. В расчёт не входят.</p>}</div>
      {data.attendance.items.length ? <Table caption="Посещаемость ученика" columns={attendance} items={data.attendance.items} rowKey={v => v.id} /> : <EmptyState title="Нет завершённых занятий за период" />}
    </Card>
    {data.full_access && <Card className="table-card"><div className="table-heading"><h2>История критических изменений</h2></div>{data.history?.length ? <Table caption="История изменений ученика" columns={history} items={data.history} rowKey={v => v.id} /> : <EmptyState title="Изменений пока нет" />}</Card>}
  </>;
}
