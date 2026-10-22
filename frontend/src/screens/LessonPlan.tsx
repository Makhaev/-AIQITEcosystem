import { useEffect, useState } from 'react';
import { message, request } from '../api';
import { Card, Notice, Select } from '../components/ui';
import { TemplatePlan, versionURL, type ProgramVersion, type Template } from './ProgramVersions';
export type PlanSnapshot = { origin?: string; version_number?: number; version_id?: number; program_name?: string; section_name?: string; module_name?: string; template?: Template };
export function LessonPlan({ programID, versionID, snapshot }: { programID: number; versionID?: number; snapshot?: PlanSnapshot }) {
  if (!snapshot || !versionID) return null;
  return <Card><h2>План занятия</h2>{snapshot.origin === 'legacy_unknown' ? <Notice>Для этого старого занятия полный исторический план неизвестен.</Notice> : <><p>{snapshot.program_name}{snapshot.version_number ? ` · Версия ${snapshot.version_number}` : ''}</p><p className="muted">{[snapshot.section_name, snapshot.module_name].filter(Boolean).join(' / ')}</p>{snapshot.template ? <TemplatePlan template={snapshot.template} /> : <Notice>Шаблон для занятия не выбран.</Notice>}</>}<a href={versionURL(programID, versionID)}>Открыть закреплённую версию</a></Card>;
}
export function LessonPlanPicker({ programID, versionID, moduleKey, templateKey, disabled, change }: { programID: number; versionID?: number; moduleKey: string; templateKey: string; disabled: boolean; change: (module: string, template: string, topic?: string) => void }) {
  const [v, setV] = useState<ProgramVersion | null>(null); const [loading, setLoading] = useState(false); const [error, setError] = useState('');
  useEffect(() => { const c = new AbortController(); setV(null); setError(''); if (!versionID) return; setLoading(true); request<ProgramVersion>(`/programs/${programID}/versions/${versionID}`, 'GET', undefined, c.signal).then(setV).catch(e => { if (!c.signal.aborted) setError(message(e)); }).finally(() => { if (!c.signal.aborted) setLoading(false); }); return () => c.abort(); }, [programID, versionID]);
  if (!versionID) return null;
  const options = v?.document.sections.flatMap(s => s.modules.flatMap(m => [
    { value: `${m.key}/`, topic: undefined, label: `${s.name} / ${m.name} · Без шаблона` },
    ...m.lessons.map(t => ({ value: `${m.key}/${t.key}`, topic: t.topic, label: `${s.name} / ${m.name} / ${t.topic} · ${t.duration_minutes} мин.` })),
  ])) ?? [];
  const value = `${moduleKey}/${templateKey}`;
  return <><Notice tone="error">{error}</Notice><Select label="Шаблон закреплённой версии" value={value} disabled={disabled || loading || !!error} onChange={e => { const [m, t] = e.target.value.split('/'); change(m, t, options.find(o => o.value === e.target.value)?.topic); }} hint={v ? `Версия ${v.number}. Конспект и материалы сохранятся в плане занятия.` : undefined}><option value="/">Без шаблона</option>{value !== '/' && !options.some(o => o.value === value) && <option value={value}>Текущий модуль без шаблона</option>}{options.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}</Select></>;
}
