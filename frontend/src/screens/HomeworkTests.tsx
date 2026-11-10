import { useEffect, useId, useRef, useState, type FormEvent } from 'react';
import { message, request, type User } from '../api';
import { Badge, Button, Card, EmptyState, Input, Notice, PageHeading, Pagination, Select } from '../components/ui';
import { StudentGameProgress } from '../components/StudentGameProgress';

type Question = { prompt: string; options: string[]; correct: number };
type Homework = { id: number; title: string; instructions: string; student_name: string; author_name: string; can_submit: boolean; questions: Omit<Question, 'correct'>[]; answers: number[]; attempts: number; correct_count: number; due_at: string | null; completed_at: string | null };
const emptyQuestion = (): Question => ({ prompt: '', options: ['', ''], correct: 0 });
const at = (value: string) => new Date(value).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow' });

function TestEditor({ user, onSaved }: { user: User; onSaved: () => void }) {
  const [questions, setQuestions] = useState<Question[]>([emptyQuestion()]);
  const [students, setStudents] = useState<{ id: number; name: string }[]>([]);
  const [selected, setSelected] = useState<number[]>([]);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [retry, setRetry] = useState(0);
  const [busy, setBusy] = useState(false);
  const [selectionError, setSelectionError] = useState('');
  const recipients = useRef<HTMLFieldSetElement>(null);
  const selectionHintID = useId();
  const curatorOnly = user.roles.includes('curator') && !user.roles.some(role => role === 'admin' || role === 'methodist');
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError('');
    void request<{ items: typeof students }>('/homework-tests/students', 'GET', undefined, controller.signal)
      .then(data => { if (!controller.signal.aborted) setStudents(data.items); })
      .catch(err => { if (!controller.signal.aborted) setError(message(err)); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [retry]);
  function update(index: number, patch: Partial<Question>) { setQuestions(items => items.map((q, i) => i === index ? { ...q, ...patch } : q)); }
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const data = new FormData(event.currentTarget); setError('');
    if (!selected.length) { setSelectionError('Выберите хотя бы одного ученика, которому нужно выдать тест.'); recipients.current?.focus(); return; }
    setSelectionError(''); setBusy(true);
    try {
      await request('/homework-tests', 'POST', { title: data.get('title'), instructions: data.get('instructions'), questions, student_ids: selected, due_at: data.get('due_at') ? new Date(`${String(data.get('due_at'))}+03:00`).toISOString() : null });
      onSaved();
    } catch (err) { setError(message(err)); } finally { setBusy(false); }
  }
  return <Card><h2>Создать тест и выдать как ДЗ</h2><p>В каждом вопросе один правильный ответ. У ученика одна попытка: после отправки ответы нельзя изменить. За все правильные ответы он получит одну палочку прогресса.</p>
    <Notice tone="error">{error}</Notice>{error && <Button variant="secondary" onClick={() => setRetry(v => v + 1)}>Обновить список учеников</Button>}
    <form onSubmit={save}><fieldset disabled={busy} className="stack">
      <Input label="Название теста" name="title" required maxLength={200} />
      <Input label="Инструкция (необязательно)" name="instructions" maxLength={5000} />
      {questions.map((q, i) => <fieldset className="test-question stack" key={i}><legend>Вопрос {i + 1}</legend>
        <Input label={`Текст вопроса ${i + 1}`} required maxLength={2000} value={q.prompt} onChange={e => update(i, { prompt: e.target.value })} />
        {q.options.map((option, j) => <Input key={j} label={`Вопрос ${i + 1} · вариант ${j + 1}`} required maxLength={500} value={option} onChange={e => update(i, { options: q.options.map((v, k) => k === j ? e.target.value : v) })} />)}
        <Select label={`Правильный ответ на вопрос ${i + 1}`} value={q.correct} onChange={e => update(i, { correct: Number(e.target.value) })}>{q.options.map((_, j) => <option key={j} value={j}>Вариант {j + 1}</option>)}</Select>
        <div className="row-actions">{q.options.length < 6 && <Button type="button" variant="secondary" onClick={() => update(i, { options: [...q.options, ''] })}>Добавить вариант</Button>}{q.options.length > 2 && <Button type="button" variant="ghost" onClick={() => update(i, { options: q.options.slice(0, -1), correct: Math.min(q.correct, q.options.length - 2) })}>Убрать последний вариант</Button>}{questions.length > 1 && <Button type="button" variant="ghost" onClick={() => setQuestions(items => items.filter((_, index) => index !== i))}>Удалить вопрос {i + 1}</Button>}</div>
      </fieldset>)}
      {questions.length < 30 && <Button type="button" variant="secondary" onClick={() => setQuestions(q => [...q, emptyQuestion()])}>Добавить вопрос</Button>}
      <Input type="datetime-local" label="Срок выполнения · МСК (необязательно)" name="due_at" />
      <fieldset className="test-question" ref={recipients} tabIndex={-1} aria-describedby={selectionHintID}><legend>Кому выдать тест</legend>
        <p id={selectionHintID}>{curatorOnly ? 'Выберите учеников своих групп.' : 'Выберите учеников, которым нужно выдать тест.'} Можно выбрать до 100 учеников.</p>
        <Notice tone="error">{selectionError}</Notice>
        {loading ? <p role="status">Загружаем учеников…</p> : !students.length ? <p>{curatorOnly ? 'В ваших группах пока нет активных учеников. Попросите администратора проверить назначение куратора и состав групп.' : 'Активных учеников пока нет. Добавьте учеников перед выдачей теста.'}</p> : <>
          <div className="row-actions"><Button type="button" variant="secondary" onClick={() => { setSelected(students.slice(0, 100).map(s => s.id)); setSelectionError(''); }}>Выбрать всех{students.length > 100 ? ' (первые 100)' : ''}</Button><Button type="button" variant="ghost" disabled={!selected.length} onClick={() => setSelected([])}>Снять выбор</Button></div>
          <div className="test-students">{students.map(s => <label className="checkbox" key={s.id}><input type="checkbox" checked={selected.includes(s.id)} disabled={!selected.includes(s.id) && selected.length >= 100} onChange={e => { setSelected(ids => e.target.checked ? [...ids, s.id] : ids.filter(id => id !== s.id)); setSelectionError(''); }} />{s.name}</label>)}</div>
        </>}
        <p role="status">Выбрано учеников: {selected.length}</p>
      </fieldset>
      <Button type="submit" loading={busy} disabled={loading || !students.length}>Выдать тест как ДЗ{selected.length > 0 ? ` · ученикам: ${selected.length}` : ''}</Button>
      {!loading && students.length > 0 && !selected.length && <p className="field-hint">Перед выдачей отметьте учеников в списке выше или нажмите «Выбрать всех».</p>}
    </fieldset></form>
  </Card>;
}

function TestWork({ homework, onCompleted }: { homework: Homework; onCompleted: () => void }) {
  const [opened, setOpened] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [result, setResult] = useState('');
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const data = new FormData(event.currentTarget); setBusy(true); setError(''); setResult('');
    try {
      const response = await request<{ completed: boolean; awarded: boolean; correct_count: number; total: number }>(`/homework-tests/${homework.id}/submit`, 'POST', { answers: homework.questions.map((_, i) => Number(data.get(`answer-${i}`))) });
      setResult(response.completed ? response.awarded ? 'Тест выполнен! +1 палочка прогресса.' : 'Тест уже выполнен. Палочка прогресса начислена ранее.' : `Правильных ответов: ${response.correct_count} из ${response.total}. Тест завершён с ошибками. Ответы сохранены, изменить их нельзя. Палочка не начислена.`);
      onCompleted();
    } catch (err) { setError(message(err)); } finally { setBusy(false); }
  }
  return <Card><div className="section-heading"><h2>{homework.title}</h2><Badge tone={homework.completed_at ? 'success' : homework.attempts > 0 ? 'warning' : 'neutral'}>{homework.completed_at ? 'Выполнен · +1 палочка' : homework.attempts > 0 ? 'Завершён с ошибками' : 'К выполнению'}</Badge></div>
    <p>{homework.student_name} · Автор: {homework.author_name}</p>{homework.instructions && <p className="pilot-prose">{homework.instructions}</p>}
    <p>Вопросов: {homework.questions.length} · Попыток: {homework.attempts}{homework.due_at ? ` · Срок · МСК: ${at(homework.due_at)}` : ''}</p>
    {homework.attempts > 0 && <p>Последний результат: {homework.correct_count} из {homework.questions.length}{homework.completed_at ? ` · Выполнен ${at(homework.completed_at)} МСК` : ''}</p>}
    <Notice tone="error">{error}</Notice><Notice tone={homework.completed_at ? 'success' : 'info'}>{result}</Notice>
    <Button variant="secondary" onClick={() => setOpened(v => !v)} aria-expanded={opened} aria-controls={`test-${homework.id}`}>{opened ? 'Свернуть тест' : homework.can_submit && homework.attempts === 0 && !homework.completed_at ? 'Выполнить тест' : 'Посмотреть тест'}</Button>
    <div id={`test-${homework.id}`} hidden={!opened}>{homework.attempts === 0 ? <p>У тебя одна попытка. Проверь ответы перед отправкой: после проверки их нельзя будет изменить, даже если допущена ошибка.</p> : <p>Ответы отправлены и сохранены. Изменение и повторное прохождение недоступны.</p>}<form onSubmit={submit}><fieldset disabled={busy || !homework.can_submit || homework.attempts > 0 || !!homework.completed_at} className="stack">
      {homework.questions.map((q, i) => <fieldset className="test-question" key={i}><legend>{i + 1}. {q.prompt}</legend>{q.options.map((option, j) => <label className="checkbox" key={j}><input type="radio" name={`answer-${i}`} required value={j} defaultChecked={homework.answers[i] === j} />{option}</label>)}</fieldset>)}
      {homework.can_submit && homework.attempts === 0 && !homework.completed_at && <Button type="submit" loading={busy}>Проверить ответы</Button>}
    </fieldset></form></div>
  </Card>;
}

export function HomeworkTests({ user }: { user: User }) {
  const creator = user.roles.some(r => ['admin', 'methodist', 'curator'].includes(r));
  const student = user.roles.includes('student');
  const [editing, setEditing] = useState(false);
  const [page, setPage] = useState(1);
  const [version, setVersion] = useState(0);
  const [data, setData] = useState<{ items: Homework[]; total: number } | null>(null);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  useEffect(() => {
    const controller = new AbortController(); setError('');
    void request<{ items: Homework[]; total: number }>(`/homework-tests?page=${page}`, 'GET', undefined, controller.signal)
      .then(value => { if (!controller.signal.aborted) setData(value); })
      .catch(err => { if (!controller.signal.aborted) setError(message(err)); });
    return () => controller.abort();
  }, [page, version]);
  return <><PageHeading title="Тесты и ДЗ" description={creator ? 'Создавайте тесты, выдавайте ученикам и следите за выполнением.' : 'Выполни тест без ошибок и получи одну палочку прогресса.'} action={creator && <Button onClick={() => setEditing(v => !v)}>{editing ? 'Закрыть создание теста' : 'Создать тест'}</Button>} />
    <div className="stack"><Notice tone="error">{error}</Notice><Notice tone="success">{notice}</Notice>{error && <Button variant="secondary" onClick={() => setVersion(v => v + 1)}>Повторить загрузку</Button>}
      {editing && <TestEditor user={user} onSaved={() => { setEditing(false); setPage(1); setVersion(v => v + 1); setNotice('Тест создан и выдан выбранным ученикам.'); }} />}
      {!data && !error && <p role="status">Загружаем тесты…</p>}
      {data && <>{data.items.length ? data.items.map(h => <TestWork key={h.id} homework={h} onCompleted={() => setVersion(v => v + 1)} />) : <Card><EmptyState title="Тестов пока нет" description={creator ? 'Создайте первый тест и выберите учеников.' : 'Здесь появятся тесты от методиста или куратора.'} /></Card>}<Pagination page={page} total={data.total} pageSize={20} onChange={p => { setData(null); setPage(p); }} /></>}
      {student && <StudentGameProgress revision={version} />}
    </div>
  </>;
}
