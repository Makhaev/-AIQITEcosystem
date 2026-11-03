import { useEffect, useState } from 'react';
import { APIError, message, request } from '../api';
import { Button, Card, Input, Notice, PageHeading, Pagination, Select } from '../components/ui';

type Delivery = { id: number; user_id: number; title: string; status: string; attempts: number; cycle_attempts: number; max_attempts: number; revision: number; next_attempt_at: string | null; lease_until: string | null; sent_at: string | null; delivered_at: string | null; last_error: string; can_retry: boolean; retries: { actor_id: number; reason: string; previous_attempts: number; created_at: string }[] };
type Queue = { items: Delivery[]; total: number; page_size: number };
const states: Record<string, string> = { pending: 'Ожидает отправки', processing: 'Обрабатывается', sent: 'Передано провайдеру', failed: 'Отправка прекращена' };
const failures: Record<string, string> = { provider_error: 'Провайдер не подтвердил приём письма.', configuration_unavailable: 'Проверьте настройки почты.', recipient_unavailable: 'Получатель или доступ больше не актуальны.', attempts_exhausted: 'Лимит попыток исчерпан.', processing_expired: 'Предыдущая обработка прервалась.', invalid_provider_receipt: 'Ответ провайдера не удалось проверить.' };
const date = (v: string | null) => v ? new Date(v).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow' }) + ' (Москва)' : '—';

export function NotificationDeliveries() {
  const [data, setData] = useState<Queue | null>(null), [status, setStatus] = useState('failed'), [page, setPage] = useState(1), [reload, setReload] = useState(0);
  const [error, setError] = useState(''), [notice, setNotice] = useState(''), [busy, setBusy] = useState(false);
  const [drafts, setDrafts] = useState<Record<number, { reason: string; acknowledge: boolean }>>({});
  useEffect(() => {
    const c = new AbortController(); setData(null); setError('');
    request<Queue>(`/notification-deliveries?page=${page}&status=${status}`, 'GET', undefined, c.signal)
      .then(v => { if (!c.signal.aborted) setData(v); })
      .catch(e => { if (!c.signal.aborted) { setError(message(e)); if (e instanceof APIError && (e.status === 401 || e.status === 403)) setDrafts({}); } });
    return () => c.abort();
  }, [page, status, reload]);
  function edit(id: number, fields: Partial<{ reason: string; acknowledge: boolean }>) {
    setDrafts(v => ({ ...v, [id]: { ...(v[id] || { reason: '', acknowledge: false }), ...fields } }));
  }
  async function retry(d: Delivery) {
    setBusy(true); setError(''); setNotice('');
    try {
      await request(`/notification-deliveries/${d.id}/retry`, 'POST', { revision: d.revision, reason: drafts[d.id]?.reason || '', acknowledge_duplicate_risk: drafts[d.id]?.acknowledge || false });
      setDrafts(v => { const next = { ...v }; delete next[d.id]; return next; });
      setNotice('Отправка возвращена в очередь. Права будут проверены ещё раз перед отправкой.');
      setReload(v => v + 1);
    } catch (e) {
      setError(message(e));
      // A stale revision must be refreshed explicitly before another submission.
      setData(null);
      if (e instanceof APIError && (e.status === 401 || e.status === 403)) setDrafts({});
    } finally { setBusy(false); }
  }
  return <div className="stack">
    <PageHeading title="Доставка уведомлений" description="Очередь email и окончательно неудачные отправки. Передано провайдеру означает приём письма почтовым сервером; доставка подтверждается отдельно." />
    <Card className="stack"><div className="filters">
      <Select label="Статус отправки" value={status} disabled={busy} onChange={e => { setStatus(e.target.value); setPage(1); setNotice(''); }}>
        <option value="">Все</option>{Object.entries(states).map(([key, label]) => <option key={key} value={key}>{label}</option>)}
      </Select>
      <Button variant="secondary" disabled={busy} onClick={() => setReload(v => v + 1)}>Обновить очередь</Button>
    </div></Card>
    <Notice tone="error">{error}</Notice><Notice>{notice}</Notice>
    {!data && !error && <p role="status">Загрузка…</p>}
    {data?.items.length === 0 && <Card><p>Отправок с таким статусом нет.</p></Card>}
    {data?.items.map(d => <Card key={d.id} className="stack">
      <h2>{d.title}</h2><p>Отправка №{d.id} · Аккаунт №{d.user_id} · {states[d.status]}</p>
      <p>Попытки: {d.cycle_attempts} из {d.max_attempts} в текущем цикле; всего {d.attempts}.</p>
      <p>Следующая попытка: {date(d.next_attempt_at)}</p>
      {d.status === 'processing' && <p>Срок восстановления прерванной обработки: {date(d.lease_until)}</p>}
      <p>Передано провайдеру: {date(d.sent_at)}<br />Доставка подтверждена: {date(d.delivered_at)}</p>
      {d.last_error && <p>{failures[d.last_error] || 'Отправка не завершена.'}</p>}
      {d.retries.length > 0 && <details><summary>История ручных повторов ({d.retries.length})</summary>{d.retries.map((h, i) => <p key={i}>{date(h.created_at)} · Сотрудник №{h.actor_id} · Попыток до повтора: {h.previous_attempts}. {h.reason}</p>)}</details>}
      {d.can_retry && <form className="stack" onSubmit={e => { e.preventDefault(); void retry(d); }}>
        <Input label={`Причина повтора отправки №${d.id}`} required maxLength={1000} value={drafts[d.id]?.reason || ''} disabled={busy} onChange={e => edit(d.id, { reason: e.target.value })} />
        <label><input type="checkbox" required checked={drafts[d.id]?.acknowledge || false} disabled={busy} onChange={e => edit(d.id, { acknowledge: e.target.checked })} /> При неопределённом результате прошлой отправки получатель может получить письмо повторно.</label>
        <Button type="submit" disabled={busy || !drafts[d.id]?.reason.trim() || !drafts[d.id]?.acknowledge}>Вернуть в очередь</Button>
      </form>}
    </Card>)}
    {data && <Pagination page={page} total={data.total} pageSize={data.page_size} onChange={setPage} />}
  </div>;
}
