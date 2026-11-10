import { useEffect, useState } from 'react';
import { APIError, message, request } from '../api';
import { Button, Card, EmptyState, Input, Notice, Pagination, Table, type Column } from './ui';

type StudentRow = { id: number; name: string; birth_date: string; registered_on: string; groups: string; curators: string };
type Register = { items: StudentRow[]; total: number; month: string; as_of: string };
const currentMonth = () => new Intl.DateTimeFormat('sv-SE', { timeZone: 'Europe/Moscow', year: 'numeric', month: '2-digit' }).format(new Date());
const date = (value: string) => value ? value.split('-').reverse().join('.') : '—';

export function MonthlyStudentRegister({ revision }: { revision: number }) {
  const [month, setMonth] = useState(currentMonth);
  const [page, setPage] = useState(1);
  const [reload, setReload] = useState(0);
  const [data, setData] = useState<Register | null>(null);
  const [error, setError] = useState('');
  const [exportError, setExportError] = useState('');
  const [exporting, setExporting] = useState(false);
  useEffect(() => { const refresh = () => setReload(v => v + 1); window.addEventListener('focus', refresh); return () => window.removeEventListener('focus', refresh); }, []);
  useEffect(() => {
    const controller = new AbortController(); setData(null); setError(''); setExportError('');
    void request<Register>(`/students/monthly-register?${new URLSearchParams({ month, page: String(page) })}`, 'GET', undefined, controller.signal)
      .then(value => { if (!controller.signal.aborted) setData(value); })
      .catch(err => { if (!controller.signal.aborted) setError(message(err)); });
    return () => controller.abort();
  }, [month, page, revision, reload]);
  async function download() {
    setExportError(''); setExporting(true);
    try {
      const response = await fetch(`/api/v1/students/monthly-register?${new URLSearchParams({ month, format: 'xlsx' })}`, { credentials: 'same-origin' });
      if (!response.ok) {
        const body = await response.json().catch(() => ({ message: 'Не удалось скачать таблицу.' }));
        if (response.status === 401) window.dispatchEvent(new Event('aiq-session-ended'));
        throw new APIError(response.status, body.code, body.message, body.field_errors);
      }
      const url = URL.createObjectURL(await response.blob());
      const link = document.createElement('a'); link.href = url; link.download = `students-${month}.xlsx`; document.body.append(link); link.click(); link.remove(); window.setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (err) { setExportError(message(err)); } finally { setExporting(false); }
  }
  const columns: Column<StudentRow>[] = [
    { key: 'number', title: '№', render: s => (page - 1) * 20 + (data?.items.findIndex(row => row.id === s.id) ?? 0) + 1 },
    { key: 'name', title: 'ФИО ученика', render: s => <a href={`#/students/${s.id}`}>{s.name}</a> },
    { key: 'birth_date', title: 'Дата рождения', render: s => date(s.birth_date) },
    { key: 'registered_on', title: 'Дата добавления', render: s => date(s.registered_on) },
    { key: 'groups', title: 'Группы', render: s => s.groups || '—' },
    { key: 'curators', title: 'Кураторы', render: s => s.curators || '—' },
  ];
  return <Card className="monthly-register"><div className="table-heading"><h2>Таблица учеников по месяцам</h2><Button variant="secondary" loading={exporting} disabled={!data || !!error} onClick={() => void download()}>Скачать Excel</Button></div>
    <p>Ученики добавляются автоматически. При переводе карточки в архив ученик убирается из текущего состава; состав прошлых месяцев сохраняется на конец месяца.</p>
    <div className="filters"><Input label="Месяц таблицы" type="month" value={month} min="1900-01" max={currentMonth()} onChange={e => { if (/^\d{4}-\d{2}$/.test(e.target.value)) { setMonth(e.target.value); setPage(1); } }} /><Button variant="secondary" onClick={() => setReload(v => v + 1)}>Обновить таблицу</Button></div>
    <Notice tone="error">{error || exportError}</Notice>
    {!data && !error && <p role="status">Загружаем таблицу учеников…</p>}
    {data && <><p className="muted">Состав на {date(data.as_of)} · Учеников: {data.total}. Excel содержит весь выбранный месяц, включая остальные страницы.</p>{data.items.length ? <Table caption={`Таблица учеников за ${month}`} columns={columns} items={data.items} rowKey={s => s.id} /> : <EmptyState title="В этом месяце учеников нет" />}<Pagination page={page} total={data.total} pageSize={20} onChange={setPage} /></>}
  </Card>;
}
