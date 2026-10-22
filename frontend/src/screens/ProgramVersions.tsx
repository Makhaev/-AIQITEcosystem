import { useEffect, useId, useState, type FormEvent } from 'react';
import { APIError, message, request, type User } from '../api';
import { Badge, Button, Card, EmptyState, Input, Modal, Notice, PageHeading, Select } from '../components/ui';
import './program-versions.css';

export type Template = { key: string; topic: string; goals: string; duration_minutes: number; outline: string; exercises: string; equipment: string; materials: string; criteria: string };
export type VersionModule = { key: string; name: string; description: string; lessons: Template[] };
type Section = { key: string; name: string; modules: VersionModule[] };
export type ProgramDocument = { name: string; description: string; sections: Section[] };
export type ProgramVersion = { id: number; program_id: number; number: number; status: string; document: ProgramDocument; revision: number; created_by: number | null; submitted_by: number | null; approved_by: number | null; legacy_import: boolean; source_version_id: number | null; events?: { id: number; actor_id: number | null; action: string; comment: string; created_at: string }[] };
export const versionStatus: Record<string, string> = { draft: 'Черновик', review: 'На проверке', published: 'Опубликована', archived: 'В архиве' };
const actionNames: Record<string, string> = { created: 'Создана', edited: 'Сохранён черновик', submit: 'Отправлена на проверку', return: 'Возвращена на правку', publish: 'Опубликована', archive: 'Архивирована', imported: 'Перенесена из Core' };
export const templateFields = [['goals', 'Цели'], ['outline', 'Конспект'], ['exercises', 'Упражнения'], ['equipment', 'Оборудование'], ['materials', 'Материалы'], ['criteria', 'Критерии']] as const;
export const versionURL = (program: number | string, version: number | string) => `#/programs/${program}/versions/${version}`;
const endpoint = (program: number | string, version?: number | string) => `/programs/${program}/versions${version ? `/${version}` : ''}`;
const newKey = () => crypto.randomUUID();
function Status({ value }: { value: string }) { return <Badge tone={value === 'published' ? 'success' : value === 'review' ? 'warning' : 'neutral'}>{versionStatus[value]}</Badge>; }
function Text({ label, value, onChange, maxLength = 20000 }: { label: string; value: string; onChange: (value: string) => void; maxLength?: number }) {
  const id = useId(); return <div className="field"><label htmlFor={id}>{label}</label><textarea id={id} className="input" rows={3} value={value} maxLength={maxLength} onChange={e => onChange(e.target.value)} /></div>;
}
export function TemplatePlan({ template }: { template: Template }) {
  return <div className="template-plan"><h4>{template.topic}</h4><p className="muted">{template.duration_minutes} мин.</p><dl>{templateFields.map(([key, label]) => <div key={key}><dt>{label}</dt><dd>{template[key] || 'Не указано'}</dd></div>)}</dl></div>;
}
export function ProgramVersions({ programID, user }: { programID: number; user: User }) {
  const [items, setItems] = useState<ProgramVersion[]>([]); const [error, setError] = useState(''); const [loading, setLoading] = useState(true); const [busy, setBusy] = useState(false); const [reload, setReload] = useState(0);
  useEffect(() => { const c = new AbortController(); setLoading(true); setError(''); request<{ items: ProgramVersion[] }>(endpoint(programID), 'GET', undefined, c.signal).then(v => setItems(v.items)).catch(e => { if (!c.signal.aborted) setError(message(e)); }).finally(() => { if (!c.signal.aborted) setLoading(false); }); return () => c.abort(); }, [programID, reload]);
  async function create() { setBusy(true); setError(''); try { const v = await request<ProgramVersion>(endpoint(programID), 'POST', {}); window.location.hash = versionURL(programID, v.id); } catch (e) { setError(message(e)); } finally { setBusy(false); } }
  return <Card><div className="table-heading"><h2>Версии программы</h2>{user.roles.includes('methodologist') && <Button loading={busy} onClick={() => void create()}>Новая версия</Button>}</div>
    <p className="muted">Группы сохраняют закреплённую версию при публикации новой.</p><Notice tone="error">{error}</Notice>
    {loading ? <p role="status">Загружаем версии…</p> : error ? <Button variant="secondary" onClick={() => setReload(v => v + 1)}>Повторить загрузку версий</Button> : !items.length ? <EmptyState title="Версий пока нет" /> : <div className="version-list">{items.map(v => <a className="version-list-item" key={v.id} href={versionURL(programID, v.id)}><div><strong>Версия {v.number} · {v.document.name}</strong>{v.legacy_import && <span className="muted">Снимок существующего Core</span>}</div><Status value={v.status} /></a>)}</div>}
  </Card>;
}
function move<T>(items: T[], index: number, delta: number): T[] { const next = [...items]; const j = index + delta; if (j < 0 || j >= next.length) return items; [next[index], next[j]] = [next[j], next[index]]; return next; }
function Order({ label, index, size, onMove, onDelete }: { label: string; index: number; size: number; onMove: (delta: number) => void; onDelete: () => void }) {
  return <div className="row-actions"><Button variant="ghost" aria-label={`Выше: ${label}`} disabled={index === 0} onClick={() => onMove(-1)}>↑</Button><Button variant="ghost" aria-label={`Ниже: ${label}`} disabled={index === size - 1} onClick={() => onMove(1)}>↓</Button><Button variant="ghost" aria-label={`Удалить: ${label}`} onClick={onDelete}>Удалить</Button></div>;
}
function DocumentEditor({ value, change }: { value: ProgramDocument; change: (d: ProgramDocument) => void }) {
  function section(si: number, s: Section) { change({ ...value, sections: value.sections.map((old, i) => i === si ? s : old) }); }
  function module(si: number, mi: number, m: VersionModule) { const s = value.sections[si]; section(si, { ...s, modules: s.modules.map((old, i) => i === mi ? m : old) }); }
  function template(si: number, mi: number, ti: number, t: Template) { const m = value.sections[si].modules[mi]; module(si, mi, { ...m, lessons: m.lessons.map((old, i) => i === ti ? t : old) }); }
  return <div className="stack"><Input label="Название версии" required maxLength={200} value={value.name} onChange={e => change({ ...value, name: e.target.value })} /><Text label="Описание версии" value={value.description} maxLength={5000} onChange={description => change({ ...value, description })} />
    {value.sections.map((s, si) => <section className="curriculum-section" key={s.key}><div className="table-heading"><h3>Раздел {si + 1}</h3><Order label={`раздел ${si + 1}`} index={si} size={value.sections.length} onMove={d => change({ ...value, sections: move(value.sections, si, d) })} onDelete={() => change({ ...value, sections: value.sections.filter((_, i) => i !== si) })} /></div>
      <Input label={`Название раздела ${si + 1}`} required maxLength={200} value={s.name} onChange={e => section(si, { ...s, name: e.target.value })} />
      {s.modules.map((m, mi) => <section className="curriculum-module" key={m.key}><div className="table-heading"><h4>Модуль {si + 1}.{mi + 1}</h4><Order label={`модуль ${si + 1}.${mi + 1}`} index={mi} size={s.modules.length} onMove={d => section(si, { ...s, modules: move(s.modules, mi, d) })} onDelete={() => section(si, { ...s, modules: s.modules.filter((_, i) => i !== mi) })} /></div>
        <Input label={`Название модуля ${si + 1}.${mi + 1}`} required maxLength={200} value={m.name} onChange={e => module(si, mi, { ...m, name: e.target.value })} /><Text label="Описание модуля" value={m.description} maxLength={5000} onChange={description => module(si, mi, { ...m, description })} />
        {m.lessons.map((t, ti) => <details className="curriculum-template" key={t.key} open><summary>Занятие {ti + 1} · {t.topic || 'Новая тема'}</summary><Order label={`занятие ${si + 1}.${mi + 1}.${ti + 1}`} index={ti} size={m.lessons.length} onMove={d => module(si, mi, { ...m, lessons: move(m.lessons, ti, d) })} onDelete={() => module(si, mi, { ...m, lessons: m.lessons.filter((_, i) => i !== ti) })} />
          <Input label="Тема занятия" required maxLength={200} value={t.topic} onChange={e => template(si, mi, ti, { ...t, topic: e.target.value })} /><Input label="Длительность, минут" type="number" required min={1} max={1440} value={t.duration_minutes} onChange={e => template(si, mi, ti, { ...t, duration_minutes: Number(e.target.value) })} />
          {templateFields.map(([key, label]) => <Text key={key} label={label} value={t[key]} onChange={text => template(si, mi, ti, { ...t, [key]: text })} />)}
        </details>)}
        <Button variant="secondary" onClick={() => module(si, mi, { ...m, lessons: [...m.lessons, { key: newKey(), topic: '', goals: '', duration_minutes: 45, outline: '', exercises: '', equipment: '', materials: '', criteria: '' }] })}>Добавить шаблон занятия</Button>
      </section>)}
      <Button variant="secondary" onClick={() => section(si, { ...s, modules: [...s.modules, { key: newKey(), name: '', description: '', lessons: [] }] })}>Добавить модуль раздела</Button>
    </section>)}
    <Button variant="secondary" onClick={() => change({ ...value, sections: [...value.sections, { key: newKey(), name: '', modules: [] }] })}>Добавить раздел</Button>
  </div>;
}
export function VersionDetails({ programID, id, user }: { programID: string; id: string; user: User }) {
  const [conflict, setConflict] = useState(false);
  const [v, setV] = useState<ProgramVersion | null>(null); const [document, setDocument] = useState<ProgramDocument | null>(null); const [error, setError] = useState(''); const [success, setSuccess] = useState(''); const [busy, setBusy] = useState(false); const [loading, setLoading] = useState(true); const [dirty, setDirty] = useState(false); const [action, setAction] = useState(''); const [reload, setReload] = useState(0);
  const methodologist = user.roles.includes('methodologist'); const reviewer = user.roles.includes('education_leader'); const editable = methodologist && v?.status === 'draft';
  useEffect(() => { const c = new AbortController(); setLoading(true); setError(''); request<ProgramVersion>(endpoint(programID, id), 'GET', undefined, c.signal).then(value => { setV(value); setDocument(value.document); setDirty(false); setConflict(false); }).catch(e => { if (!c.signal.aborted) setError(message(e)); }).finally(() => { if (!c.signal.aborted) setLoading(false); }); return () => c.abort(); }, [programID, id, reload]);
  useEffect(() => {
    if (!dirty) return;
    const warn = (e: BeforeUnloadEvent) => { e.preventDefault(); e.returnValue = ''; };
    const navigate = (e: MouseEvent) => {
      if (e.defaultPrevented || e.button !== 0 || e.ctrlKey || e.metaKey || e.shiftKey || e.altKey) return;
      const link = e.target instanceof Element ? e.target.closest<HTMLAnchorElement>('a[href]') : null;
      if (!link || link.target === '_blank' || link.hasAttribute('download') || link.href === window.location.href) return;
      if (link.origin !== window.location.origin || link.pathname !== window.location.pathname || !link.hash.startsWith('#/')) return;
      if (!window.confirm('Уйти без сохранения черновика?')) { e.preventDefault(); e.stopPropagation(); }
    };
    window.addEventListener('beforeunload', warn);
    window.document.addEventListener('click', navigate, true);
    return () => { window.removeEventListener('beforeunload', warn); window.document.removeEventListener('click', navigate, true); };
  }, [dirty]);
  function reloadCurrent() {
    if (dirty && !window.confirm('Загрузить актуальную версию? Несохранённые правки в этой форме будут заменены.')) return;
    setAction(''); setSuccess(''); setReload(n => n + 1);
  }
  function showFailure(e: unknown) {
    setError(message(e));
    if (e instanceof APIError && e.code === 'stale_revision') setConflict(true);
  }
  async function save(e: FormEvent) {
    e.preventDefault(); if (!v || !document) return;
    const body = { revision: v.revision, document };
    setError(''); setSuccess('');
    if (new TextEncoder().encode(JSON.stringify(body)).byteLength > 1024 * 1024) {
      setError('Объём версии превышает 1 МБ. Сократите текст перед сохранением; ваши правки остаются в форме.'); return;
    }
    setBusy(true);
    try { const result = await request<ProgramVersion>(endpoint(programID, id), 'PUT', body); setV({ ...result, events: v.events }); setDocument(result.document); setDirty(false); setConflict(false); setSuccess('Черновик сохранён.'); setReload(n => n + 1); }
    catch (e) { showFailure(e); } finally { setBusy(false); }
  }
  async function perform(e: FormEvent<HTMLFormElement>) { e.preventDefault(); if (!v) return; const data = new FormData(e.currentTarget); setBusy(true); setError(''); setSuccess(''); try { await request(endpoint(programID, id) + '/actions', 'POST', { revision: v.revision, action, comment: String(data.get('comment') ?? '') }); setAction(''); setSuccess('Статус версии изменён.'); setReload(n => n + 1); } catch (e) { showFailure(e); } finally { setBusy(false); } }
  async function clone() { if (!v) return; setBusy(true); setError(''); try { const result = await request<ProgramVersion>(endpoint(programID), 'POST', { source_version_id: v.id }); window.location.hash = versionURL(programID, result.id); } catch (e) { setError(message(e)); } finally { setBusy(false); } }
  const self = v && (v.created_by === user.id || v.submitted_by === user.id || v.events?.some(e => e.actor_id === user.id && ['created', 'edited', 'submit'].includes(e.action)));
  return <><a className="back-link" href={`#/programs/${programID}`}>← К программе</a><PageHeading title={v ? `Версия ${v.number} · ${v.document.name}` : 'Версия программы'} description="Разделы, модули и планы занятий." />
    {conflict && !action && <Notice tone="warning">Документ изменён другим участником. Ваши правки остаются в форме. Перед продолжением загрузите актуальную версию.<Button variant="secondary" disabled={busy || loading} onClick={reloadCurrent}>Загрузить актуальную версию</Button></Notice>}
    <div className="stack"><Notice tone="error">{error}</Notice><Notice tone="success">{success}</Notice>{loading ? <p role="status">Загружаем версию…</p> : !v || !document ? <Button onClick={() => setReload(n => n + 1)}>Повторить загрузку</Button> : <>
      <Card><div className="table-heading"><Status value={v.status} /><span className="muted">Редакция {v.revision}</span></div>{v.legacy_import && <Notice>Снимок Core перенесён без утверждения и без восстановления неизвестного содержания. Для методической публикации создайте новую версию.</Notice>}
        <div className="row-actions">{methodologist && v.status === 'draft' && <Button disabled={busy || dirty} onClick={() => setAction('submit')}>Отправить на проверку</Button>}{reviewer && v.status === 'review' && <><Button disabled={busy || !!self} onClick={() => setAction('publish')}>Опубликовать</Button><Button variant="secondary" disabled={busy} onClick={() => setAction('return')}>Вернуть на правку</Button></>}{reviewer && v.status === 'published' && <Button variant="secondary" onClick={() => setAction('archive')}>Архивировать версию</Button>}{methodologist && ['published', 'archived'].includes(v.status) && <Button onClick={() => void clone()} loading={busy}>Создать новую на основе этой</Button>}</div>
        {self && v.status === 'review' && reviewer && <Notice>Утверждение должен выполнить другой руководитель образования: вы участвовали в подготовке.</Notice>}{dirty && <Notice>Есть несохранённые изменения. Сохраните черновик перед отправкой.</Notice>}
      </Card>
      <Card>{editable ? <form onSubmit={save}><fieldset disabled={busy} className="version-fieldset"><DocumentEditor value={document} change={d => { setDocument(d); setDirty(true); setSuccess(''); }} /><div className="modal-footer"><Button type="submit" loading={busy}>Сохранить черновик</Button></div></fieldset></form> : <div className="stack"><p className="program-description">{document.description}</p>{document.sections.map((s, i) => <section className="curriculum-section" key={s.key}><h2>{i + 1}. {s.name}</h2>{s.modules.map(m => <section className="curriculum-module" key={m.key}><h3>{m.name}</h3><p className="program-description">{m.description}</p>{m.lessons.map(t => <details className="curriculum-template" key={t.key}><summary>{t.topic} · {t.duration_minutes} мин.</summary><TemplatePlan template={t} /></details>)}{!m.lessons.length && <p className="muted">Шаблоны отсутствуют.</p>}</section>)}</section>)}</div>}</Card>
      {!!v.events?.length && <Card><h2>История версии</h2><ol className="version-history">{v.events.map(ev => <li key={ev.id}><strong>{actionNames[ev.action] ?? ev.action}</strong><span className="muted">{new Date(ev.created_at).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow' })} МСК{ev.actor_id ? ` · Участник № ${ev.actor_id}` : ''}</span>{ev.comment && <p>{ev.comment}</p>}</li>)}</ol></Card>}
    </>}</div>
    {action && <Modal title={{ submit: 'Отправить на проверку?', return: 'Вернуть на правку', publish: 'Опубликовать версию?', archive: 'Архивировать версию?' }[action] ?? ''} description={action === 'publish' ? 'Содержание станет неизменяемым. Группы сохранят свои текущие версии.' : action === 'archive' ? 'Существующие группы и занятия сохранят доступ к этой версии.' : undefined} busy={busy} onClose={() => { setAction(''); setError(''); }}><Notice tone="error">{error}</Notice>{conflict && <Button variant="secondary" disabled={busy} onClick={reloadCurrent}>Загрузить актуальную версию</Button>}<form onSubmit={perform}><Input label={action === 'return' ? 'Комментарий возврата' : 'Комментарий'} name="comment" required={action === 'return'} maxLength={1000} /><div className="modal-footer"><Button variant="secondary" disabled={busy} onClick={() => setAction('')}>Отмена</Button><Button type="submit" loading={busy}>Подтвердить</Button></div></form></Modal>}
  </>;
}
export function VersionPicker({ programID, selected, onChange, disabled = false }: { programID: number; selected: string; onChange: (id: string) => void; disabled?: boolean }) {
  const [items, setItems] = useState<ProgramVersion[]>([]); const [error, setError] = useState(''); const [loading, setLoading] = useState(false);
  useEffect(() => { const c = new AbortController(); setItems([]); setError(''); if (!programID) return; setLoading(true); request<{ items: ProgramVersion[] }>(endpoint(programID), 'GET', undefined, c.signal).then(v => setItems(v.items.filter(i => i.status === 'published'))).catch(e => { if (!c.signal.aborted) setError(message(e)); }).finally(() => { if (!c.signal.aborted) setLoading(false); }); return () => c.abort(); }, [programID]);
  return <><Select label="Опубликованная версия" required value={selected} disabled={disabled || loading || !programID} onChange={e => onChange(e.target.value)}><option value="">{loading ? 'Загрузка…' : 'Выберите версию'}</option>{items.map(v => <option value={v.id} key={v.id}>Версия {v.number} · {v.document.name}{v.legacy_import ? ' · Снимок Core' : ''}</option>)}</Select><Notice tone="error">{error}</Notice>{programID > 0 && !loading && !items.length && !error && <Notice>У этой программы нет опубликованных версий.</Notice>}</>;
}
