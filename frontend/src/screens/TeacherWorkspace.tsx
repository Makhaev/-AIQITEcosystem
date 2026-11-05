import { useContext, useEffect, useState, type FormEvent } from 'react';
import { message, request } from '../api';
import { Features } from '../features';
import { Badge, Button, Card, EmptyState, Input, Notice, PageHeading, Pagination, Select, Table, type Column } from '../components/ui';

type Group = { id: number; name: string; program_name: string; status: string; primary_teacher: boolean; student_count: number };
type Pupil = { id: number; name: string; status: string; groups: { id: number; name: string }[] };
type List<T> = { items: T[]; total: number; page: number; page_size: number; group?: { id: number; name: string } | null };
type Lesson = { id: number; topic: string; group_id: number; group_name: string; starts_at: string; ends_at: string; location: string; program_name: string; module_name: string | null };
type Overview = { profile: { id: number; name: string; specialization: string; status: string } | null; counts: { groups: number; students: number; lessons_today: number; pending_reviews: number; draft_reports: number }; upcoming_lessons: Lesson[] };
const statuses: Record<string, string> = { active: 'Активен', draft: 'Черновик', archived: 'В архиве' };
const time = (value: string) => new Date(value).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow', dateStyle: 'short', timeStyle: 'short' });
function usePersonal<T>(path: string) {
  const [data, setData] = useState<T | null>(null); const [error, setError] = useState(''); const [loading, setLoading] = useState(true); const [version, setVersion] = useState(0);
  useEffect(() => { const controller = new AbortController(); setData(null); setError(''); setLoading(true); void request<T>(path, 'GET', undefined, controller.signal).then(value => { if (!controller.signal.aborted) setData(value); }).catch(err => { if (!controller.signal.aborted) setError(message(err)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); }); return () => controller.abort(); }, [path, version]);
  return { data, error, loading, refresh: () => setVersion(v => v + 1) };
}
function LoadState({ loading, error, retry }: { loading: boolean; error: string; retry: () => void }) {
  return <>{loading && <p role="status">Загружаем ваш кабинет…</p>}<Notice tone="error">{error}</Notice>{error && <Button variant="secondary" onClick={retry}>Повторить загрузку</Button>}</>;
}
export function TeacherWorkspace() {
  const remote = usePersonal<Overview>('/teacher/workspace'); const data = remote.data;
  return <><PageHeading title="Кабинет педагога" description="Ваши группы, ученики и ближайшие занятия. Время указано по Москве." /><div className="stack"><LoadState loading={remote.loading} error={remote.error} retry={remote.refresh} />{data && <>
    {!data.profile ? <Card><EmptyState title="Профиль педагога пока не связан с аккаунтом" description="Попросите администратора выбрать вашу учётную запись в карточке педагога. После назначения группы и занятий здесь появятся ваши учебные данные." /></Card> : <><Card><h2>{data.profile.name}</h2>{data.profile.specialization && <p>{data.profile.specialization}</p>}{data.profile.status === 'archived' && <Notice tone="warning">Профиль в архиве. Для новых назначений обратитесь к администратору.</Notice>}<a href="#/teachers">Мой профиль</a></Card>
    <div className="stats">{([
      ['groups', 'Мои группы', '#/my-groups'], ['students', 'Мои ученики', '#/my-students'], ['lessons_today', 'Занятий сегодня', '#/lessons'], ['pending_reviews', 'Работы на проверке', '#/assignments'], ['draft_reports', 'Черновики отчётов', '#/reports'],
    ] as const).map(([key, label, href]) => <Card key={key}><h2><a href={href}>{label}</a></h2><span className="stat-value" aria-label={label}>{data.counts[key]}</span></Card>)}</div>
    <Card><h2>Ближайшие занятия</h2>{data.upcoming_lessons.length ? data.upcoming_lessons.map((lesson, index) => <article className="pilot-list-row" key={lesson.id}>{index === 0 && <Badge tone="success">Ближайшее занятие</Badge>}<h3><a href={`#/lessons/${lesson.id}`}>{lesson.topic}</a></h3><p><a href={`#/my-groups/${lesson.group_id}`}>{lesson.group_name}</a> · {lesson.program_name}{lesson.module_name ? ` · ${lesson.module_name}` : ''}</p><p>{time(lesson.starts_at)} – {time(lesson.ends_at)} МСК</p>{lesson.location && <p>{lesson.location}</p>}<div className="row-actions"><a className="button button--primary" href={`#/lessons/${lesson.id}`}>Открыть занятие и журнал</a><a className="button button--secondary" href={`#/my-groups/${lesson.group_id}`}>Ученики группы</a></div></article>) : <EmptyState title="Предстоящих занятий пока нет" description="После назначения занятия администратором оно появится здесь. Проведённые занятия доступны в расписании." />}</Card>
    <Card><h2>После занятия</h2><p>Сохраните посещаемость, проверьте ответы учеников и подготовьте отчёт родителю. Публикация доступна после завершения занятия и принятия всех работ ученика.</p><div className="row-actions"><a className="button button--secondary" href="#/assignments">Перейти к заданиям</a><a className="button button--secondary" href="#/reports">Подготовить отчёт</a></div></Card></>}
  </>}</div></>;
}
function PersonalFilters({ groups, apply }: { groups?: boolean; apply: (query: string) => void }) {
  function submit(event: FormEvent<HTMLFormElement>) { event.preventDefault(); const data = new FormData(event.currentTarget); const params = new URLSearchParams(); for (const key of ['search', 'status']) { const value = String(data.get(key) ?? '').trim(); if (value) params.set(key, value); } apply(params.toString()); }
  return <form className="filters" onSubmit={submit}><Input label={groups ? 'Поиск моей группы' : 'Поиск моего ученика'} name="search" maxLength={200} /><Select label="Статус" name="status"><option value="">Все статусы</option><option value="active">Активен</option>{groups && <option value="draft">Черновик</option>}<option value="archived">В архиве</option></Select><Button variant="secondary" type="submit">Применить</Button></form>;
}
export function TeacherGroups() {
  const [page, setPage] = useState(1); const [filters, setFilters] = useState(''); const remote = usePersonal<List<Group>>(`/teacher/groups?page=${page}&page_size=20&${filters}`);
  const columns: Column<Group>[] = [
    { key: 'name', title: 'Группа', render: g => <a href={`#/my-groups/${g.id}`}><strong>{g.name}</strong></a> },
    { key: 'program', title: 'Программа', render: g => g.program_name },
    { key: 'assignment', title: 'Моё назначение', render: g => g.primary_teacher ? 'Основной педагог' : 'Педагог занятия' },
    { key: 'students', title: 'Состав группы', render: g => <><span>Обучается: {g.student_count}</span><a href={`#/my-groups/${g.id}`}>Ученики моих занятий</a></> },
    { key: 'status', title: 'Статус', render: g => <Badge>{statuses[g.status]}</Badge> },
  ];
  return <><a className="back-link" href="#/teaching">← Кабинет педагога</a><PageHeading title="Мои группы" description="Группы, где вы назначены основным педагогом или ведёте занятия." /><PersonalFilters groups apply={value => { setFilters(value); setPage(1); }} /><div className="stack"><LoadState loading={remote.loading} error={remote.error} retry={remote.refresh} />{remote.data && <Card className="table-card">{remote.data.items.length ? <Table caption="Мои группы" columns={columns} items={remote.data.items} rowKey={g => g.id} /> : <EmptyState title="Группы не найдены" description="Проверьте фильтры. Если назначений ещё нет, попросите администратора назначить вас группе или занятию." />}<Pagination page={page} pageSize={20} total={remote.data.total} onChange={setPage} disabled={remote.loading} /></Card>}</div></>;
}
export function TeacherStudents({ groupID }: { groupID?: string }) {
  const features = useContext(Features); const [page, setPage] = useState(1); const [filters, setFilters] = useState('');
  const remote = usePersonal<List<Pupil>>(`/teacher/students?page=${page}&page_size=20&${groupID ? `group_id=${encodeURIComponent(groupID)}&` : ''}${filters}`);
  const columns: Column<Pupil>[] = [
    { key: 'name', title: 'Ученик', render: p => features.student360 ? <a href={`#/students/${p.id}`}>{p.name}</a> : p.name },
    { key: 'groups', title: 'Мои занятия в группах', render: p => p.groups.map(g => <a key={g.id} href={`#/my-groups/${g.id}`}>{g.name}</a>) },
    { key: 'status', title: 'Учебный статус', render: p => <Badge>{statuses[p.status]}</Badge> },
  ];
  return <><a className="back-link" href={groupID ? '#/my-groups' : '#/teaching'}>← {groupID ? 'Мои группы' : 'Кабинет педагога'}</a><PageHeading title={remote.data?.group ? `Ученики: ${remote.data.group.name}` : 'Мои ученики'} description="Ученики из состава ваших занятий. Для проведённых занятий сохраняется состав на дату урока." /><PersonalFilters apply={value => { setFilters(value); setPage(1); }} /><div className="stack"><LoadState loading={remote.loading} error={remote.error} retry={remote.refresh} />{remote.data && <Card className="table-card">{remote.data.items.length ? <Table caption="Мои ученики" columns={columns} items={remote.data.items} rowKey={p => p.id} /> : <EmptyState title="Ученики не найдены" description="Проверьте фильтры. Список появится после назначения вам занятия с зачисленными учениками." />}<Pagination page={page} pageSize={20} total={remote.data.total} onChange={setPage} disabled={remote.loading} /></Card>}</div></>;
}
