import { useEffect, useState, type FormEvent } from 'react';
import { message, request } from '../api';
import { Button, Card, EmptyState, Input, Notice, Select } from '../components/ui';

type Report = { revision: number; state: string; outcome: string; plan_result: string; comment: string; confirm_element: boolean; reason: string; actor_id: number; created_at: string; lesson_id: number };
type Reports = { current: Report | null; history: Report[]; can_edit: boolean };
type Progress = { state: string; version_number: number; assigned_at: string; percent: number | null; total_weight: number; completed_weight: number | null; elements: { key: string; title: string; weight: number; completed: boolean; sources: Report[] }[] };

export function GroupProgress({ id }: { id: number }) {
  const [data, setData] = useState<Progress | null>(null), [error, setError] = useState(''), [version, setVersion] = useState(0);
  useEffect(() => { const c = new AbortController(); setData(null); setError(''); request<Progress>(`/groups/${id}/progress`, 'GET', undefined, c.signal).then(v => { if (!c.signal.aborted) setData(v); }).catch(e => { if (!c.signal.aborted) setError(message(e)); }); return () => c.abort(); }, [id, version]);
  return <Card><h2>Прогресс программы группы</h2><p className="muted">Выполнение назначенного плана группы. Это не уровень навыка или личный прогресс ученика.</p><Notice tone="error">{error}</Notice>
    {error ? <Button onClick={() => setVersion(v => v + 1)}>Повторить расчёт прогресса</Button> : !data ? <p role="status">Загружаем прогресс…</p> : data.state === 'restricted' ? <p>Сводный прогресс недоступен в вашей области прав.</p> : data.percent === null ? <EmptyState title="Нет данных для расчёта прогресса" description="В назначенной версии нет обязательных элементов с весами." /> : <>
      <p>Версия {data.version_number} · назначение от {new Intl.DateTimeFormat('ru-RU', { timeZone: 'Europe/Moscow', dateStyle: 'short' }).format(new Date(data.assigned_at))}</p>
      <p aria-label="Прогресс программы группы">{data.percent.toLocaleString('ru-RU', { maximumFractionDigits: 2 })}%</p><p>Подтверждённый вес: {data.completed_weight} из {data.total_weight}</p>
      <ul>{data.elements.map(e => <li key={e.key}>{e.title} · вес {e.weight} · {e.completed ? 'Подтверждён' : 'Не подтверждён'}{e.sources.map(s => <p key={s.lesson_id}><a href={`#/lessons/${s.lesson_id}`}>Отчёт занятия, редакция {s.revision}</a> · {new Intl.DateTimeFormat('ru-RU', { timeZone: 'Europe/Moscow', dateStyle: 'short' }).format(new Date(s.created_at))}</p>)}</li>)}</ul>
    </>}
  </Card>;
}

export function LessonReport({ id }: { id: number }) {
  const [data, setData] = useState<Reports | null>(null), [error, setError] = useState(''), [version, setVersion] = useState(0), [busy, setBusy] = useState(false);
  useEffect(() => { const c = new AbortController(); setData(null); setError(''); request<Reports>(`/lessons/${id}/report`, 'GET', undefined, c.signal).then(v => { if (!c.signal.aborted) setData(v); }).catch(e => { if (!c.signal.aborted) setError(message(e)); }); return () => c.abort(); }, [id, version]);
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const form = new FormData(event.currentTarget); setBusy(true); setError('');
    try { await request(`/lessons/${id}/report`, 'PUT', { revision: data?.current?.revision ?? 0, state: form.get('state'), outcome: form.get('outcome'), plan_result: form.get('plan_result'), comment: form.get('comment'), confirm_element: form.has('confirm_element'), reason: form.get('reason') }); setVersion(v => v + 1); }
    catch (err) { setError(message(err)); } finally { setBusy(false); }
  }
  const current = data?.current;
  return <Card><h2>Отчёт и подтверждение элемента программы</h2><Notice tone="error">{error}</Notice>
    {error && data && <Button variant="secondary" onClick={() => { if (window.confirm('Загрузить последнюю редакцию? Сначала сохраните введённый текст: форма будет обновлена.')) setVersion(v => v + 1); }}>Загрузить последнюю редакцию отчёта</Button>}
    {!data ? error ? <Button onClick={() => setVersion(v => v + 1)}>Повторить загрузку отчёта</Button> : <p role="status">Загружаем отчёт…</p> : <>
      <p>{current ? `${current.state === 'submitted' ? 'Отправлен' : 'Черновик'} · редакция ${current.revision}` : 'Отчёт ещё не создан'}</p>
      {data.can_edit ? <form onSubmit={save} key={current?.revision ?? 0}>
        <Input label="Фактический результат занятия" name="outcome" defaultValue={current?.outcome} maxLength={10000} disabled={busy} />
        <Select label="Выполнение плана занятия" name="plan_result" defaultValue={current?.plan_result ?? 'not_completed'} disabled={busy}><option value="not_completed">Не выполнен</option><option value="partial">Частично</option><option value="complete">Полностью</option></Select>
        <Input label="Комментарий к выполнению" name="comment" defaultValue={current?.comment} maxLength={10000} disabled={busy} />
        <label className="checkbox"><input type="checkbox" name="confirm_element" defaultChecked={current?.confirm_element ?? false} disabled={busy} />Подтверждаю выполнение связанного элемента программы</label>
        <p className="muted">Подтверждение учитывается только после завершения занятия и отправки отчёта. Повторные занятия не увеличивают вес одного элемента.</p>
        <Select label="Состояние отчёта" name="state" defaultValue={current?.state ?? 'draft'} disabled={busy}>{current?.state !== 'submitted' && <option value="draft">Черновик</option>}<option value="submitted">Отправлен</option></Select>
        <Input label="Причина исправления или внесения администратором" name="reason" maxLength={10000} disabled={busy} />
        <Button type="submit" disabled={busy}>Сохранить отчёт</Button>
      </form> : current && <><p>{current.outcome}</p><p>{current.comment}</p></>}
      {!!data.history.length && <details><summary>История отчёта</summary><ol>{data.history.map(r => <li key={r.revision}>Редакция {r.revision} · {r.state === 'submitted' ? 'Отправлен' : 'Черновик'}<p>{r.outcome}</p><p>{r.reason}</p></li>)}</ol></details>}
    </>}
  </Card>;
}
