import { useEffect, useState } from 'react';
import { request } from '../api';
import { Button, Card } from '../components/ui';
type Notification = { report_id: number; version: number; title: string; created_at: string };
export function FamilyReportNotifications({ sid }: { sid: string }) {
 const [items, setItems] = useState<Notification[]>([]), [error, setError] = useState(false), [reload, setReload] = useState(0);
 useEffect(() => { const c = new AbortController(); setItems([]); setError(false);
  request<Notification[]>(`/family/students/${sid}/notifications`, 'GET', undefined, c.signal).then(v => { if (!c.signal.aborted) setItems(v); }).catch(() => { if (!c.signal.aborted) setError(true); }); return () => c.abort();
 }, [sid, reload]);
 if (!items.length && !error) return null;
 return <Card><h2>Уведомления об отчётах</h2>{error ? <><p>Не удалось загрузить уведомления.</p><Button variant="secondary" onClick={() => setReload(v => v + 1)}>Повторить загрузку уведомлений</Button></> : <ul>{items.map(n => <li key={`${n.report_id}/${n.version}`}>Опубликован отчёт «{n.title}», версия {n.version} · {new Date(n.created_at).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow' })}</li>)}</ul>}</Card>;
}
