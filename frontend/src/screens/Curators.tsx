import { useEffect, useState } from 'react';
import { message, request } from '../api';
import { Badge, Button, Card, EmptyState, Input, Notice, PageHeading, Pagination, Select, Table, type Column } from '../components/ui';

type Curator = { id: number; name: string; email: string; blocked: boolean; groups_count: number; students_count: number; reports_count: number; published_count: number; last_activity: string | null };
type Activity = Curator & { groups: { id: number; name: string; status: string }[]; recent_reports: { id: number; student_name: string; summary: string; updated_at: string; published_at: string | null }[] };
const date = (value: string | null) => value ? new Date(value).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow' }) : 'Пока нет отчётов';

export function Curators({ id }: { id?: string }) {
  const [search, setSearch] = useState('');
  const [status, setStatus] = useState('');
  const [page, setPage] = useState(1);
  const [version, setVersion] = useState(0);
  const [data, setData] = useState<{ items: Curator[]; total: number } | null>(null);
  const [activity, setActivity] = useState<Activity | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    const controller = new AbortController();
    setError(''); setData(null); setActivity(null);
    const url = id ? `/curators/${id}` : `/curators?${new URLSearchParams({ search, status, page: String(page) })}`;
    void request<Activity | { items: Curator[]; total: number }>(url, 'GET', undefined, controller.signal)
      .then(value => { if (!controller.signal.aborted) { if ('items' in value) setData(value); else setActivity(value); } })
      .catch(err => { if (!controller.signal.aborted) setError(message(err)); });
    return () => controller.abort();
  }, [id, search, status, page, version]);
  const columns: Column<Curator>[] = [
    { key: 'name', title: 'ФИО', render: c => <><a href={`#/curator-reports/${c.id}`}>{c.name}</a><span className="muted">{c.email}</span></> },
    { key: 'status', title: 'Статус', render: c => <Badge tone={c.blocked ? 'danger' : 'success'}>{c.blocked ? 'Заблокирован' : 'Активен'}</Badge> },
    { key: 'groups', title: 'Активные группы', render: c => c.groups_count },
    { key: 'students', title: 'Ученики', render: c => c.students_count },
    { key: 'reports', title: 'Отчёты', render: c => <>{c.reports_count} · опубликовано: {c.published_count}</> },
    { key: 'activity', title: 'Последнее изменение отчёта · МСК', render: c => date(c.last_activity) },
    { key: 'actions', title: 'Действия', render: c => <a className="button button--secondary" href={`#/curator-reports/${c.id}`}>Посмотреть активность</a> },
  ];
  return <><PageHeading title={activity?.name || (id ? 'Активность куратора' : 'Кураторы')} description="Закреплённые группы, ученики и работа с отчётами." />
    {id && <p><a href="#/curator-reports">← Все кураторы</a></p>}
    <Notice tone="error">{error}</Notice>
    {error && <Button variant="secondary" onClick={() => setVersion(v => v + 1)}>Повторить загрузку</Button>}
    {!id && <Card><div className="filters"><Input label="Поиск по ФИО или email" value={search} maxLength={200} onChange={e => { setSearch(e.target.value); setPage(1); }} /><Select label="Статус" value={status} onChange={e => { setStatus(e.target.value); setPage(1); }}><option value="">Все</option><option value="active">Активные</option><option value="blocked">Заблокированные</option></Select></div>
      {data && <><p className="muted">Всего: {data.total}</p>{data.items.length ? <Table caption="Кураторы AIQ" columns={columns} items={data.items} rowKey={c => c.id} /> : <EmptyState title="Кураторы не найдены" description="Проверьте фильтры. В списке отображаются пользователи с ролью «Куратор»." />}<Pagination page={page} total={data.total} pageSize={20} onChange={setPage} /></>}
    </Card>}
    {!error && !data && !activity && <p role="status">Загружаем кураторов…</p>}
    {activity && <div className="stack"><Card><h2>Работа куратора</h2><p>{activity.email} · {activity.blocked ? 'Заблокирован' : 'Активен'}</p><p>Активных групп: {activity.groups_count} · Активных учеников: {activity.students_count}</p><p>Отчётов: {activity.reports_count} · Опубликовано: {activity.published_count} · Черновиков: {activity.reports_count - activity.published_count}</p><p>Последнее изменение отчёта · МСК: {date(activity.last_activity)}</p></Card>
      <Card><h2>Закреплённые группы</h2>{activity.groups.length ? activity.groups.map(g => <p key={g.id}><a href={`#/groups/${g.id}`}>{g.name}</a> · {g.status === 'active' ? 'Активна' : 'В архиве'}</p>) : <p>Закреплённых групп пока нет.</p>}</Card>
      <Card><h2>Последние 20 отчётов</h2>{activity.recent_reports.length ? activity.recent_reports.map(r => <article className="pilot-list-row" key={r.id}><h3><a href={`#/family-report/${r.id}`}>{r.student_name}</a></h3><p>{r.published_at ? 'Опубликован' : 'Черновик'} · {date(r.updated_at)} МСК</p><p className="pilot-prose">{r.summary}</p></article>) : <p>Куратор пока не создавал отчёты.</p>}</Card>
    </div>}
  </>;
}
