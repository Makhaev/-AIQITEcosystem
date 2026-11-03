import { useEffect, useState } from 'react';
import { APIError, message, request } from '../api';
import { Button, Card, Notice, PageHeading, Pagination, Select } from '../components/ui';

type Item = { id: number; kind: string; title: string; url: string; created_at: string; read_at: string | null };
type Inbox = { items: Item[]; total: number; unread: number; page: number; page_size: number };

export function Notifications() {
  const [data, setData] = useState<Inbox | null>(null);
  const [page, setPage] = useState(1), [unread, setUnread] = useState(false), [reload, setReload] = useState(0);
  const [error, setError] = useState(''), [busy, setBusy] = useState(false), [notice, setNotice] = useState('');
  useEffect(() => {
    const refresh = () => setReload(v => v + 1);
    const timer = window.setInterval(refresh, 30_000);
    window.addEventListener('focus', refresh);
    return () => { window.clearInterval(timer); window.removeEventListener('focus', refresh); };
  }, []);
  useEffect(() => {
    const c = new AbortController();
    setData(null); setError('');
    request<Inbox>(`/notifications?page=${page}&unread=${unread}`, 'GET', undefined, c.signal)
      .then(v => { if (!c.signal.aborted) setData(v); })
      .catch(e => { if (!c.signal.aborted) setError(message(e)); });
    return () => c.abort();
  }, [page, unread, reload]);
  async function markRead(id: number) {
    setBusy(true); setError(''); setNotice('');
    try { await request(`/notifications/${id}/read`, 'POST'); setNotice('Уведомление отмечено прочитанным.'); setReload(v => v + 1); }
    catch (e) { if (e instanceof APIError && [401, 403, 404].includes(e.status)) setData(null); setError(message(e)); }
    finally { setBusy(false); }
  }
  return <div className="stack">
    <PageHeading title="Уведомления" description="Изменения расписания, учебные события и сообщения. Подробности доступны в кабинете." />
    <Card className="stack"><div className="filters">
      <Select label="Показать уведомления" value={String(unread)} onChange={e => { setUnread(e.target.value === 'true'); setPage(1); }}>
        <option value="false">Все</option><option value="true">Непрочитанные</option>
      </Select>
      <Button variant="secondary" disabled={busy} onClick={() => setReload(v => v + 1)}>Обновить уведомления</Button>
    </div>{data && <p role="status">Непрочитанных: {data.unread}{notice && ` · ${notice}`}</p>}</Card>
    <Notice tone="error">{error}</Notice>
    {!data && !error && <p role="status">Загрузка…</p>}
    {data && data.items.length === 0 && <Card><p>Нет доступных уведомлений.</p></Card>}
    {data?.items.map(n => <Card key={n.id} className="stack">
      <h2>{n.title}</h2>
      <p>{new Date(n.created_at).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow' })} (Москва) · {n.read_at ? 'Прочитано' : 'Не прочитано'}</p>
      <div className="row-actions"><a href={n.url}>Открыть в кабинете</a>
        {!n.read_at && <Button variant="secondary" disabled={busy} onClick={() => void markRead(n.id)}>Отметить прочитанным</Button>}
      </div>
    </Card>)}
    {data && <Pagination page={page} total={data.total} pageSize={data.page_size} onChange={setPage} />}
  </div>;
}
