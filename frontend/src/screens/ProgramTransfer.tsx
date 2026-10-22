import { useEffect, useState, type FormEvent } from 'react';
import { message, request } from '../api';
import { Button, Card, Input, Modal, Notice, Select } from '../components/ui';
import { referenceItems } from './Education';
import { TemplatePlan, VersionPicker, versionURL, type ProgramVersion } from './ProgramVersions';
import type { PlanSnapshot } from './LessonPlan';

type Mapping = { lesson_id: number; version_module_key: string; template_key: string };
type Affected = { id: number; topic: string; starts_at: string; suggested: Mapping; needs_mapping: boolean; clears_legacy_module: boolean };
type Preview = { target: ProgramVersion; lessons: Affected[]; preview_token: string };
type SavedLesson = { id: number; topic: string; plan_snapshot: PlanSnapshot };
type History = { id: number; previous_version_id: number; target_version_id: number; reason: string; created_at: string; previous_lessons: SavedLesson[]; resulting_lessons: SavedLesson[] };
function SavedPlan({ title, lesson }: { title: string; lesson: SavedLesson }) {
  const plan = lesson.plan_snapshot;
  return <section><h4>{title}</h4><p>{plan.program_name}{plan.version_number ? ` · Версия ${plan.version_number}` : ''}</p><p className="muted">{[plan.section_name, plan.module_name].filter(Boolean).join(' / ')}</p>{plan.template ? <TemplatePlan template={plan.template} /> : <p>{lesson.topic} · Без шаблона</p>}</section>;
}
export function ProgramTransfer({ groupID, programID, versionID, versionNumber, admin, updated }: { groupID: number; programID: number; versionID: number; versionNumber: number; admin: boolean; updated: () => void }) {
  const [open, setOpen] = useState(false); const [history, setHistory] = useState<History[]>([]); const [error, setError] = useState('');
  useEffect(() => { if (!admin) return; const c = new AbortController(); request<History[]>(`/groups/${groupID}/program-transfers`, 'GET', undefined, c.signal).then(setHistory).catch(e => { if (!c.signal.aborted) setError(message(e)); }); return () => c.abort(); }, [groupID, versionID, admin]);
  return <Card><div className="table-heading"><h2>Учебная версия группы</h2>{admin && <Button variant="secondary" onClick={() => setOpen(true)}>Перевести на другую версию</Button>}</div><p><a href={versionURL(programID, versionID)}>Открыть версию {versionNumber}</a></p><p className="muted">Публикация новых редакций не меняет план этой группы.</p><Notice tone="error">{error}</Notice>
    {!!history.length && <details><summary>История переводов ({history.length})</summary><ol className="version-history">{history.map(h => <li key={h.id}><strong>{new Date(h.created_at).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow' })} МСК</strong><p>{h.reason}</p><p className="muted">Версия ID {h.previous_version_id} → {h.target_version_id} · Занятий: {h.previous_lessons.length}. Прежние планы сохранены.</p>{h.previous_lessons.map(before => {
      const after = h.resulting_lessons.find(l => l.id === before.id);
      return <details className="curriculum-template" key={before.id}><summary>Сохранённые планы: {before.topic}</summary><SavedPlan title="До перевода" lesson={before} />{after && <SavedPlan title="После перевода" lesson={after} />}</details>;
    })}</li>)}</ol></details>}
    {open && <TransferEditor groupID={groupID} programID={programID} close={() => setOpen(false)} saved={() => { setOpen(false); updated(); }} />}
  </Card>;
}
function TransferEditor({ groupID, programID, close, saved }: { groupID: number; programID: number; close: () => void; saved: () => void }) {
  const [programs, setPrograms] = useState<{ id: number; name: string }[]>([]); const [program, setProgram] = useState(String(programID)); const [target, setTarget] = useState(''); const [preview, setPreview] = useState<Preview | null>(null); const [mappings, setMappings] = useState<Mapping[]>([]); const [busy, setBusy] = useState(false); const [error, setError] = useState('');
  useEffect(() => { const c = new AbortController(); referenceItems('programs?status=active', c.signal).then(items => setPrograms(items.map(v => ({ id: v.id, name: String(v.name) })))).catch(e => { if (!c.signal.aborted) setError(message(e)); }); return () => c.abort(); }, []);
  async function load() { setBusy(true); setError(''); setPreview(null); try { const p = await request<Preview>(`/groups/${groupID}/program-transfer/preview`, 'POST', { target_version_id: Number(target) }); setPreview(p); setMappings(p.lessons.map(l => l.needs_mapping ? { lesson_id: l.id, version_module_key: '?', template_key: '?' } : l.suggested)); } catch (e) { setError(message(e)); } finally { setBusy(false); } }
  async function apply(e: FormEvent<HTMLFormElement>) { e.preventDefault(); if (!preview) return; setBusy(true); setError(''); try { await request(`/groups/${groupID}/program-transfer`, 'POST', { target_version_id: Number(target), preview_token: preview.preview_token, reason: String(new FormData(e.currentTarget).get('reason') ?? ''), mappings }); saved(); } catch (e) { setError(message(e)); } finally { setBusy(false); } }
  const choices = preview?.target.document.sections.flatMap(s => s.modules.flatMap(m => [{ value: `${m.key}/`, label: `${s.name} / ${m.name} · Без шаблона` }, ...m.lessons.map(t => ({ value: `${m.key}/${t.key}`, label: `${m.name} / ${t.topic}` }))])) ?? [];
  return <Modal title="Перевод программы группы" description="Перевод изменяет только будущие запланированные занятия. Проведённые и начавшиеся занятия сохраняют свои планы." busy={busy} onClose={close}><Notice tone="error">{error}</Notice><Select label="Целевая программа" value={program} disabled={busy} onChange={e => { setProgram(e.target.value); setTarget(''); setPreview(null); }}><option value="">Выберите программу</option>{programs.map(p => <option value={p.id} key={p.id}>{p.name}</option>)}</Select>
    <VersionPicker programID={Number(program)} selected={target} disabled={busy} onChange={id => { setTarget(id); setPreview(null); }} /><Button variant="secondary" onClick={() => void load()} disabled={!target} loading={busy}>Показать затронутые занятия</Button>
    {preview && <form onSubmit={apply}><h3>Будет изменено занятий: {preview.lessons.length}</h3>{!preview.lessons.length && <Notice>Будущих занятий нет. Изменится версия для нового планирования.</Notice>}{preview.lessons.map((l, i) => <div className="transfer-lesson" key={l.id}><strong>{l.topic}</strong><p className="muted">{new Date(l.starts_at).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow' })} МСК</p>{l.clears_legacy_module && <Notice tone="warning">Модуль прежней программы будет снят. Выберите план целевой версии.</Notice>}<Select label={`Новый план: ${l.topic}`} value={`${mappings[i].version_module_key}/${mappings[i].template_key}`} disabled={busy} onChange={e => { const [m, t] = e.target.value.split('/'); setMappings(old => old.map((item, j) => j === i ? { lesson_id: l.id, version_module_key: m, template_key: t } : item)); }} required>{l.needs_mapping && <option value="?/?" disabled>Выберите соответствующий шаблон</option>}<option value="/">Сохранить тему без шаблона</option>{choices.map(c => <option value={c.value} key={c.value}>{c.label}</option>)}</Select></div>)}<Input label="Причина перевода" name="reason" required maxLength={1000} disabled={busy} /><div className="modal-footer"><Button variant="secondary" onClick={close} disabled={busy}>Отмена</Button><Button type="submit" disabled={mappings.some(m => m.template_key === '?')} loading={busy}>Подтвердить перевод</Button></div></form>}
  </Modal>;
}
