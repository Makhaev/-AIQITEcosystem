import { useEffect, useState, type FormEvent } from 'react';
import { message, request } from '../api';
import { Button, Card, EmptyState, Input, Notice } from '../components/ui';
import { LearningForm, LearningSummary, type LearningRecord } from './LearningRecords';

export const educationBlocks = [['goals', 'Цели'], ['observations', 'Наблюдения'], ['skills', 'Навыки'], ['assignments', 'Задания'], ['attempts', 'Попытки'], ['projects', 'Проекты'], ['achievements', 'Достижения']] as const;
type BlockKey = typeof educationBlocks[number][0];
export type EducationBlock = { state: 'ready' | 'error'; can_create?: boolean; items: LearningRecord[]; total: number; page: number; page_size: number; from: string; to: string };
export type EducationOverview = { from: string; to: string; current_goals: EducationBlock; current_assignments: EducationBlock } & Record<BlockKey, EducationBlock>;

function EducationSection({ studentID, kind, title, initial, from, to, current = false }: { studentID: number; kind: BlockKey; title: string; initial?: EducationBlock; from: string; to: string; current?: boolean }) {
  const [data, setData] = useState(initial), [page, setPage] = useState(1), [version, setVersion] = useState(0), [error, setError] = useState(initial?.state === 'error' ? 'Не удалось загрузить раздел.' : ''), [create, setCreate] = useState(false);
  useEffect(() => {
    if (page === 1 && version === 0) { setData(initial); setError(initial?.state === 'error' ? 'Не удалось загрузить раздел.' : ''); return; }
    const c = new AbortController(); setData(undefined); setError('');
    const q = new URLSearchParams({ education_from: from, education_to: to, page: String(page), current: String(current) });
    request<EducationBlock>(`/students/${studentID}/education/${kind}?${q}`, 'GET', undefined, c.signal).then(v => { if (!c.signal.aborted) { setData(v); setError(v.state === 'error' ? 'Не удалось загрузить раздел.' : ''); } }).catch(e => { if (!c.signal.aborted) setError(message(e)); });
    return () => c.abort();
  }, [studentID, kind, from, to, current, page, version, initial]);
  return <Card><h2>{title}</h2><p className="muted">{current ? 'Сейчас — без ограничения периодом' : kind === 'skills' ? 'Текущая хронология подтверждений; фильтр периода не меняет её состав.' : `Период: ${from} — ${to} (Москва)`}</p>
    <Notice tone="error">{error}</Notice>
    {error ? <Button onClick={() => setVersion(v => v + 1)}>Повторить загрузку: {title}</Button> : !data ? <p role="status">Загружаем раздел…</p> : data.items.length ? <>
      <ul className="stack">{data.items.map(item => <li key={item.id}>{kind === 'skills' ? <><h3>{item.content.skill_name}</h3><p>Модель: {item.content.model_version} · {item.content.skill_key}</p><p>Уровень не определён: утверждённая методика расчёта не подключена.</p>{item.sources.map(s => <p key={`${s.id}/${s.revision}`}><a href={s.url}>{s.title}</a> · {s.date} · редакция {s.revision}{s.outside_period && ' · Вне выбранного периода'}</p>)}</> : <LearningSummary record={item} />}</li>)}</ul>
      <div className="actions"><Button variant="secondary" disabled={page === 1} onClick={() => setPage(p => p - 1)}>Назад: {title}</Button><span>Страница {data.page} · доступных записей: {data.total}</span><Button variant="secondary" disabled={page * data.page_size >= data.total} onClick={() => setPage(p => p + 1)}>Далее: {title}</Button></div>
    </> : <EmptyState title={current ? 'Нет доступных открытых записей' : 'Нет доступных записей за период'} description="Отсутствие записей не является оценкой результата или уровня навыка." />}
    {kind === 'observations' && <p><a href={`#/learning-work/${studentID}`}>Рабочие наблюдения и очередь проверки</a></p>}
    {!current && kind !== 'skills' && data?.can_create && <><Button variant="secondary" onClick={() => setCreate(v => !v)}>{create ? 'Закрыть форму' : `Добавить: ${title}`}</Button>{create && <LearningForm studentID={studentID} kind={kind} onSaved={record => { window.location.hash = `#/learning/${record.id}`; }} />}</>}
  </Card>;
}

export function StudentEducation({ data, studentID, applyPeriod }: { data?: EducationOverview; studentID: number; applyPeriod: (query: string) => void }) {
  const [error, setError] = useState('');
  function filter(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const form = new FormData(event.currentTarget);
    const from = String(form.get('education_from')), to = String(form.get('education_to'));
    if (!from || !to || from > to) { setError('Начало образовательного периода не должно быть позже окончания.'); return; }
    setError(''); applyPeriod(new URLSearchParams({ education_from: from, education_to: to }).toString());
  }
  return <section aria-label="Образовательный контекст" className="stack">
    <Card><h2>Образовательный период</h2><p className="muted">Московские даты включительно. Посещаемость имеет отдельный период.</p><Notice tone="error">{error}</Notice>
      {data && <form className="filters" onSubmit={filter} key={`${data.from}/${data.to}`}><Input label="Образовательный период с" type="date" name="education_from" defaultValue={data.from} required /><Input label="Образовательный период по" type="date" name="education_to" defaultValue={data.to} required /><Button type="submit" variant="secondary">Показать образовательный период</Button></form>}
    </Card>
    {!data ? <EmptyState title="Образовательные данные не загружены" /> : <div key={`${studentID}/${data.from}/${data.to}`} className="stack">
      <EducationSection studentID={studentID} kind="goals" title="Текущие цели" initial={data.current_goals} from={data.from} to={data.to} current />
      <EducationSection studentID={studentID} kind="assignments" title="Текущие задания" initial={data.current_assignments} from={data.from} to={data.to} current />
      {educationBlocks.map(([kind, title]) => <EducationSection key={kind} studentID={studentID} kind={kind} title={title} initial={data[kind]} from={data.from} to={data.to} />)}
    </div>}
  </section>;
}
