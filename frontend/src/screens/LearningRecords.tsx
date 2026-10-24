import { useEffect, useState, type FormEvent } from 'react';
import { APIError, message, request } from '../api';
import { Button, Card, Input, Notice, PageHeading, Select } from '../components/ui';
import type { EducationBlock } from './StudentEducation';

type Source = { id: number; kind: string; revision: number; title: string; date: string; url: string; outside_period: boolean };
type Content = { title: string; fact?: string; interpretation?: string; context?: string; occurred_on?: string; starts_on?: string; ends_on?: string; due_on?: string; model_version?: string; skill_key?: string; skill_name?: string; instruction?: string; criteria?: string; format?: string; answer?: string; channel?: string; sender?: string; contribution?: string; program_version_id?: number; template_key?: string; allow_late?: boolean };
export type LearningRecord = { id: number; student_id: number; kind: string; revision: number; state: string; published: boolean; created_at: string; content: Content; source: Source; sources: Source[]; parent?: Source; successor?: Source; files: { id: number; name: string }[]; basis_unavailable: boolean; needs_review: boolean; actions: string[]; author_id: number; reviewer_id?: number };
type LearningCard = LearningRecord & { history: { id: number; revision: number; action: string; reason: string; actor_id: number; created_at: string }[] };
const states: Record<string, string> = { planned: 'Запланирована', active: 'Активна', achieved: 'Достигнута', cancelled: 'Отменена', revised: 'Пересмотрена', draft: 'Черновик', submitted: 'На проверке', reviewed: 'Проверено', assigned: 'Назначено', revision: 'На доработке', accepted: 'Принято', completed: 'Завершено', confirmed: 'Подтверждено', withdrawn: 'Отозвано' };
const actions: Record<string, string> = { activate: 'Активировать', achieve: 'Подтвердить достижение цели', cancel: 'Отменить', submit: 'Отправить на проверку', approve: 'Подтвердить', return: 'Вернуть на доработку', publish: 'Опубликовать в Student 360', unpublish: 'Снять публикацию', withdraw: 'Отозвать', correct: 'Исправить', created: 'Создано', corrected: 'Исправлено', attempt_review: 'Проверена попытка' };
const labels: Record<string, string> = { fact: 'Наблюдаемый факт', interpretation: 'Трактовка', context: 'Контекст', occurred_on: 'Дата факта', starts_on: 'Начало участия', ends_on: 'Окончание участия', due_on: 'Срок', model_version: 'Версия модели навыков', skill_key: 'Ключ навыка', skill_name: 'Навык', instruction: 'Инструкция', criteria: 'Критерии проверки', format: 'Формат ответа', answer: 'Ответ', channel: 'Канал поступления', sender: 'Отправитель', contribution: 'Индивидуальный вклад', program_version_id: 'Версия программы', template_key: 'Ключ шаблона' };
const stamp = (v: string) => new Intl.DateTimeFormat('ru-RU', { timeZone: 'Europe/Moscow', dateStyle: 'short', timeStyle: 'short' }).format(new Date(v));
const date = (v: string) => v.split('-').reverse().join('.');

export function LearningWork({ studentID }: { studentID: string }) {
  const [data,setData]=useState<EducationBlock|null>(null),[error,setError]=useState(''),[page,setPage]=useState(1),[version,setVersion]=useState(0);
  useEffect(()=>{const c=new AbortController();setData(null);setError('');request<EducationBlock>(`/students/${studentID}/education/observations?view=work&education_from=0001-01-01&education_to=9999-12-30&page=${page}`,'GET',undefined,c.signal).then(v=>{if(!c.signal.aborted)setData(v);}).catch(e=>{if(!c.signal.aborted)setError(message(e));});return()=>c.abort();},[studentID,page,version]);
  return <><PageHeading title="Рабочие наблюдения и очередь проверки" description="Доступные черновики и решения. В Student 360 попадают только проверенные опубликованные наблюдения." /><a href={`#/students/${studentID}`}>Вернуться к ученику</a><Notice tone="error">{error}</Notice>{error?<Button onClick={()=>setVersion(v=>v+1)}>Повторить загрузку</Button>:!data?<p role="status">Загружаем наблюдения…</p>:<Card>{!data.items.length?<p>Нет доступных наблюдений.</p>:<ul>{data.items.map(r=><li key={r.id}><a href={`#/learning/${r.id}`}>{r.content.title}</a> · {states[r.state]||r.state} · редакция {r.revision}</li>)}</ul>}<Button disabled={page===1} onClick={()=>setPage(v=>v-1)}>Назад</Button><Button disabled={page*data.page_size>=data.total} onClick={()=>setPage(v=>v+1)}>Далее</Button></Card>}</>;
}
function SourceLink({ source }: { source: Source }) { return <p><a href={source.url}>{source.title}</a> · {source.kind} · {date(source.date)} · редакция {source.revision}{source.outside_period && ' · Вне выбранного периода'}</p>; }
export function LearningSummary({ record: r }: { record: LearningRecord }) {
  return <><h3><a href={r.source.url}>{r.content.title}</a></h3><p>Текущее состояние: {states[r.state] || r.state}{r.kind === 'observations' && (r.published ? ' · Опубликовано' : ' · Не опубликовано')} · редакция {r.revision}</p>
    <p>Создано: {stamp(r.created_at)} (Москва) · автор #{r.author_id}{r.reviewer_id && ` · проверяющий #${r.reviewer_id}`}</p>
    {Object.entries(r.content).filter(([key, value]) => key !== 'title' && key !== 'allow_late' && value !== '').map(([key, value]) => <p key={key}><strong>{labels[key] || key}: </strong>{key.endsWith('_on') ? date(String(value)) : String(value)}</p>)}
    {r.content.allow_late !== undefined && <p>Поздняя сдача: {r.content.allow_late ? 'разрешена' : 'запрещена'}</p>}
    {r.content.due_on && ['active', 'assigned', 'revision'].includes(r.state) && r.content.due_on < new Intl.DateTimeFormat('en-CA', { timeZone: 'Europe/Moscow', year: 'numeric', month: '2-digit', day: '2-digit' }).format(new Date()) && <p>Срок истёк</p>}
    {r.needs_review && <p role="status">Основания изменились; требуется повторная проверка.</p>}{r.basis_unavailable && <p>Основание недоступно для просмотра.</p>}
    {r.successor && <><h4>Новая цель после пересмотра</h4><SourceLink source={r.successor} /></>}{r.parent && <><h4>Связанная запись</h4><SourceLink source={r.parent} /></>}{r.sources.length > 0 && <><h4>Основания</h4>{r.sources.map(s => <SourceLink key={`${s.id}/${s.revision}`} source={s} />)}</>}
    {r.files.map(f => <p key={f.id}><a href={`/api/v1/files/${f.id}/content`}>{f.name}</a></p>)}
  </>;
}
function refs(text: string) { if (!text.trim()) return []; return text.split(',').map(v => { const [id, revision, extra] = v.trim().split(':'); if (extra !== undefined || !/^\d+$/.test(id) || !/^\d+$/.test(revision || '') || Number(id) < 1 || Number(revision) < 1) throw new Error('Каждое основание задаётся как ID:редакция, через запятую.'); return { id: Number(id), revision: Number(revision) }; }); }

export function LearningForm({ studentID, kind, original, onSaved }: { studentID: number; kind: string; original?: LearningRecord; onSaved: (r: LearningRecord) => void }) {
  const [error, setError] = useState(''), [busy, setBusy] = useState(false);
  const fields: Record<string, string[]> = { observations: ['fact', 'interpretation', 'context', 'occurred_on', 'model_version', 'skill_key', 'skill_name'], goals: ['due_on', 'model_version', 'skill_key', 'skill_name'], assignments: ['instruction', 'criteria', 'due_on', 'program_version_id', 'template_key'], attempts: ['answer', 'channel', 'sender'], projects: ['starts_on', 'ends_on', 'contribution', 'criteria'], achievements: ['occurred_on'] };
  async function save(e: FormEvent<HTMLFormElement>) {
    e.preventDefault(); setBusy(true); setError(''); const f = new FormData(e.currentTarget);
    try {
      const content: Record<string, unknown> = { title: f.get('title') };
      for (const field of fields[kind]) if (f.get(field)) content[field] = field === 'program_version_id' ? Number(f.get(field)) : f.get(field);
      if (kind === 'assignments') { content.format = f.get('format'); content.allow_late = f.has('allow_late'); }
      const files = String(f.get('file_ids') || '').split(',').map(v => v.trim()).filter(Boolean).map(v => { if (!/^\d+$/.test(v) || Number(v) < 1) throw new Error('Укажите ID файлов через запятую.'); return Number(v); });
      const body = { kind, content, reviewer_id: f.get('reviewer_id') ? Number(f.get('reviewer_id')) : null, lesson_id: f.get('lesson_id') ? Number(f.get('lesson_id')) : null, parent_id: f.get('parent_id') ? Number(f.get('parent_id')) : null, sources: refs(String(f.get('sources') || '')), file_ids: files, revision: original?.revision || 0, reason: f.get('reason') || '' };
      onSaved(await request<LearningRecord>(original ? `/learning/${original.id}` : `/students/${studentID}/education`, original ? 'PUT' : 'POST', body));
    } catch (err) { setError(err instanceof Error && !(err instanceof APIError) ? err.message : message(err)); } finally { setBusy(false); }
  }
  return <form onSubmit={save} className="stack"><Notice tone="error">{error}</Notice><Input label="Название / наблюдаемый результат" name="title" defaultValue={original?.content.title} required maxLength={2000} disabled={busy} />
    {fields[kind].map(field => <Input key={field} label={labels[field]} name={field} type={field.endsWith('_on') ? 'date' : field === 'program_version_id' ? 'number' : 'text'} defaultValue={String(original?.content[field as keyof Content] || '')} required={!['interpretation', 'ends_on', 'answer', 'skill_name'].includes(field) || field === 'skill_name' && kind === 'observations'} disabled={busy} />)}
    {kind === 'assignments' && <><Select label="Формат ответа" name="format" defaultValue="text"><option value="text">Текст</option><option value="link">Ссылка HTTP(S)</option><option value="file">Приватный файл</option></Select><label><input name="allow_late" type="checkbox" />Разрешить позднюю сдачу</label></>}
    {!original && <><Input label="ID занятия (для педагогической записи)" name="lesson_id" type="number" min={1} />{kind !== 'goals' && kind !== 'attempts' && <Input label="ID проверяющего сотрудника" name="reviewer_id" type="number" min={1} required />}{kind === 'attempts' && <Input label="ID назначения задания" name="parent_id" type="number" min={1} required />}</>}
    <Input label="Основания: ID:редакция через запятую" name="sources" defaultValue={original?.sources.map(s => `${s.id}:${s.revision}`).join(', ')} required={kind === 'achievements'} /><Input label="ID приватных файлов через запятую" name="file_ids" defaultValue={original?.files.map(f => f.id).join(', ')} />
    <p className="muted">Используйте идентификаторы доступных карточек и файлов. Сервер проверяет права и актуальность каждого основания.</p>
    {original && <Input label="Причина исправления" name="reason" required />}
    <Button type="submit" disabled={busy}>{busy ? 'Сохраняем…' : 'Сохранить образовательную запись'}</Button>
  </form>;
}

export function LearningDetails({ id, revision }: { id: string; revision: string }) {
  const [data, setData] = useState<LearningCard | null>(null), [error, setError] = useState(''), [version, setVersion] = useState(0), [busy, setBusy] = useState(false), [edit, setEdit] = useState(false);
  useEffect(() => { const c = new AbortController(); setData(null); setError(''); request<LearningCard>(`/learning/${id}${revision ? `?revision=${encodeURIComponent(revision)}` : ''}`, 'GET', undefined, c.signal).then(v => { if (!c.signal.aborted) setData(v); }).catch(e => { if (!c.signal.aborted) setError(message(e)); }); return () => c.abort(); }, [id, revision, version]);
  async function act(e: FormEvent<HTMLFormElement>) { e.preventDefault(); if (!data) return; const f = new FormData(e.currentTarget); setBusy(true); setError(''); try { const result = await request<LearningCard>(`/learning/${id}/actions`, 'POST', { revision: data.revision, action: f.get('action'), reason: f.get('reason'), sources: refs(String(f.get('sources') || '')) }); setData(result); } catch (err) { if (err instanceof APIError && [401, 403, 404].includes(err.status)) setData(null); setError(err instanceof Error && !(err instanceof APIError) ? err.message : message(err)); } finally { setBusy(false); } }
  return <><PageHeading title="Образовательная запись" /><Notice tone="error">{error}</Notice>{error && <Button onClick={() => { if (!data || window.confirm('Обновить карточку? Несохранённый ввод будет потерян.')) setVersion(v => v + 1); }}>Обновить карточку</Button>}
    {!data ? !error && <p role="status">Загружаем источник…</p> : <><a href={`#/students/${data.student_id}`}>Вернуться к ученику</a><Card><p>Запись #{data.id}</p>{revision && <p>Сохранённое содержимое редакции {revision}. Статус и актуальность показаны на сейчас. <a href={`#/learning/${id}`}>Открыть текущую редакцию</a></p>}<LearningSummary record={data} /></Card>
      {!revision && data.actions.length > 0 && <Card><h2>Решение по записи</h2>{data.actions.includes('correct') && <><Button onClick={() => setEdit(v => !v)}>Исправить содержимое</Button>{edit && <LearningForm studentID={data.student_id} kind={data.kind} original={data} onSaved={() => { setEdit(false); setVersion(v => v + 1); }} />}</>}
        {data.actions.some(a => a !== 'correct') && <form onSubmit={act} key={data.revision}><Select label="Действие" name="action">{data.actions.filter(a => a !== 'correct').map(a => <option value={a} key={a}>{actions[a] || a}</option>)}</Select><Input label="Причина решения" name="reason" /><Input label="Основания достижения цели: ID:редакция" name="sources" /><Button type="submit" disabled={busy}>Сохранить решение</Button></form>}
      </Card>}
      <Card><h2>История решений</h2><ol>{data.history.map(h => <li key={h.id}><a href={`#/learning/${id}?revision=${h.revision}`}>Редакция {h.revision}</a> · {actions[h.action] || h.action} · {stamp(h.created_at)} (Москва) · сотрудник #{h.actor_id}<p>{h.reason}</p></li>)}</ol></Card>
    </>}
  </>;
}
