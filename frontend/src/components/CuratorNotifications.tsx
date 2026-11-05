import { useEffect, useState } from 'react';
import { message, request } from '../api';
import { Button, Modal, Notice } from './ui';

export function CuratorNotifications() {
  const [open, setOpen] = useState(false);
  const [data, setData] = useState<{ messages: { id: number; report_id: number; student_name: string; parent_name: string; message: string; created_at: string }[] } | null>(null);
  const [error, setError] = useState(''); const [version, setVersion] = useState(0);
  useEffect(() => {
    if (!open) return;
    const controller = new AbortController(); setData(null); setError('');
    void request<typeof data>('/curator/workspace', 'GET', undefined, controller.signal).then(value => { if (!controller.signal.aborted) setData(value); }).catch(err => { if (!controller.signal.aborted) setError(message(err)); });
    return () => controller.abort();
  }, [open, version]);
  return <><Button variant="secondary" onClick={() => setOpen(true)} aria-haspopup="dialog">Уведомления</Button>{open && <Modal title="Уведомления" description="Последние 10 ответов родителей к вашим отчётам." onClose={() => setOpen(false)}><Notice tone="error">{error}</Notice>{!data && !error && <p role="status">Загружаем ответы…</p>}{data && (data.messages.length ? data.messages.map(item => <article className="pilot-list-row" key={item.id}><h3>{item.student_name} · {item.parent_name}</h3><p className="pilot-prose">{item.message}</p><p className="muted">{new Date(item.created_at).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow' })} МСК</p><a href={`#/family-report/${item.report_id}`} onClick={() => setOpen(false)}>Открыть отчёт</a></article>) : <p>Ответов родителей пока нет.</p>)}<Button variant="secondary" onClick={() => setVersion(v => v + 1)}>Обновить</Button></Modal>}</>;
}
