import { useEffect, useState } from 'react';
import { message, request } from '../api';
import { Badge, Button, Card, EmptyState, Input, Notice, PageHeading, Pagination, Table, type Column } from '../components/ui';

type StudentScore = { id: number; name: string; rank: number; progress_marks: number; completed_lessons: number; accepted_assignments: number; submitted_assignments: number; completed_tests: number; failed_tests: number; pending: number; last_activity: string | null };

export function StudentRating() {
  const [search, setSearch] = useState('');
  const [page, setPage] = useState(1);
  const [version, setVersion] = useState(0);
  const [data, setData] = useState<{ items: StudentScore[]; total: number } | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    const controller = new AbortController(); setData(null); setError('');
    void request<{ items: StudentScore[]; total: number }>(`/learning/rating?${new URLSearchParams({ search, page: String(page) })}`, 'GET', undefined, controller.signal)
      .then(result => { if (!controller.signal.aborted) setData(result); })
      .catch(err => { if (!controller.signal.aborted) setError(message(err)); });
    return () => controller.abort();
  }, [search, page, version]);
  const columns: Column<StudentScore>[] = [
    { key: 'rank', title: 'Место', render: s => <strong>#{s.rank}</strong> },
    { key: 'student', title: 'Ученик', render: s => <a href={`#/students/${s.id}`}>{s.name}</a> },
    { key: 'marks', title: 'Палочки прогресса', render: s => <Badge tone={s.progress_marks ? 'success' : 'neutral'}>{s.progress_marks}</Badge> },
    { key: 'lessons', title: 'Занятия', render: s => <>{s.completed_lessons} посещено</> },
    { key: 'homework', title: 'ДЗ', render: s => <><span>{s.accepted_assignments} принято</span><span className="muted">{s.submitted_assignments} на проверке</span></> },
    { key: 'tests', title: 'Тесты', render: s => <><span>{s.completed_tests} пройдено</span><span className="muted">{s.failed_tests} с ошибками</span></> },
    { key: 'pending', title: 'К выполнению', render: s => s.pending },
    { key: 'activity', title: 'Последняя учебная активность · МСК', render: s => s.last_activity ? new Date(s.last_activity).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow' }) : 'Пока нет активности' },
  ];
  return <><PageHeading title="Рейтинг учеников" description="Прогресс и учебная активность за всё время. За успешный тест, принятое ДЗ и посещённое завершённое занятие — по одной палочке." action={<Button variant="secondary" onClick={() => setVersion(v => v + 1)}>Обновить рейтинг</Button>} />
    <Card><Input label="Поиск ученика по ФИО" maxLength={200} value={search} onChange={e => { setSearch(e.target.value); setPage(1); }} /><Notice tone="error">{error}</Notice>
      {!data && !error && <p role="status">Загружаем рейтинг…</p>}
      {data && <><p className="muted">Активных учеников: {data.total}. При одинаковом числе палочек ученики занимают одно место.</p>{data.items.length ? <Table caption="Рейтинг и активность учеников" columns={columns} items={data.items} rowKey={s => s.id} /> : <EmptyState title="Ученики не найдены" description="Проверьте поиск и наличие активных учеников." />}<Pagination page={page} total={data.total} pageSize={20} onChange={setPage} /></>}
    </Card>
  </>;
}
