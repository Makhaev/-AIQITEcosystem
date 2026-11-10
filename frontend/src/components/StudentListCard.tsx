import { useEffect, useState, type ReactNode } from 'react';
import { request } from '../api';
import { EntityCard } from './EntityCard';
import type { OverviewBlocks } from '../screens/Student360';

// Reuse the server-scoped 360 resource; never infer another role's data in the browser.
export function StudentListCard({ id, name, status, groups, href, overview = true, children }: {
  id: number; name: string; status: string; groups?: string; href: string; overview?: boolean; children?: ReactNode;
}) {
  const [data, setData] = useState<OverviewBlocks | null>(null); const [failed, setFailed] = useState(false);
  useEffect(() => {
    if (!overview) return;
    const controller = new AbortController(); setData(null); setFailed(false);
    void request<OverviewBlocks>(`/students/${id}/360`, 'GET', undefined, controller.signal).then(setData).catch(() => { if (!controller.signal.aborted) setFailed(true); });
    return () => controller.abort();
  }, [id, overview]);
  const fallback = !overview ? 'Недоступно' : failed ? 'Не удалось загрузить' : 'Загружаем…';
  const next = data?.upcoming_lessons?.[0]; const a = data?.attendance;
  const date = (value: string) => value.split('-').reverse().join('.');
  return <EntityCard compact title={name} titleHref={href} status={status === 'active' ? 'Активен' : 'В архиве'} tone={status === 'active' ? 'success' : 'neutral'} facts={[
    { label: 'Группа', value: groups || data?.enrollments?.filter(e => !e.ends_on).map(e => e.group_name).join(', ') || (data ? 'Нет текущих групп' : fallback) },
    { label: 'Ближайшее занятие · МСК', value: next ? new Date(next.starts_at).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow', dateStyle: 'short', timeStyle: 'short' }) : data ? 'Не запланировано' : fallback },
    { label: a ? `Посещаемость · ${date(a.from)} — ${date(a.to)}` : 'Посещаемость', value: a ? a.percent == null ? 'Нет данных' : `${a.percent.toLocaleString('ru-RU', { maximumFractionDigits: 1 })}%` : fallback },
  ]} action={<a className="button button--primary" href={href}>Открыть карточку</a>}>{children}</EntityCard>;
}
