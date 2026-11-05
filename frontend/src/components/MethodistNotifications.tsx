import { useState } from 'react';
import { message, request } from '../api';
import { Button, Modal, Notice } from './ui';

type Update = { student_id: number; student_name: string; kind: string; summary: string; created_at: string };
export function MethodistNotifications() {
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<Update[] | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  async function refresh() {
    setLoading(true); setError('');
    try { const data = await request<{ recent_reports: Update[] }>('/methodist/workspace'); setItems(data.recent_reports); }
    catch (err) { setError(message(err)); }
    finally { setLoading(false); }
  }
  return <><Button variant="secondary" aria-haspopup="dialog" onClick={() => { setOpen(true); void refresh(); }}>Уведомления</Button>
    {open && <Modal title="Последние отчёты" description="Новые отчёты педагогов и кураторов." onClose={() => setOpen(false)}>
      <Notice tone="error">{error}</Notice>{loading ? <p role="status">Загружаем отчёты…</p> : error ? <Button variant="secondary" onClick={() => void refresh()}>Повторить загрузку</Button> : items?.length ? items.map((item, index) => <article className="pilot-list-row" key={`${item.kind}:${index}`}><h3>{item.student_name}</h3>
        <p className="muted">{item.kind === 'teacher' ? 'Отчёт педагога' : 'Отчёт куратора'} · {new Date(item.created_at).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow' })} МСК</p>
        <p className="pilot-prose">{item.summary}</p><a href={`#/methodist-${item.kind === 'teacher' ? 'teacher' : 'curator'}-reports?student_id=${item.student_id}`} onClick={() => setOpen(false)}>Открыть отчёты ученика</a>
      </article>) : <p>Отчётов пока нет.</p>}
    </Modal>}</>;
}
