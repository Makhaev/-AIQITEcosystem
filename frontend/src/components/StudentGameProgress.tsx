import { useEffect, useId, useState } from 'react';
import { message, request } from '../api';
import { Button, Card, Notice } from './ui';

type Progress = { student_id: number; student_name: string; total: number; reviewed: number; submitted: number; assigned: number; completed_tests?: number; failed_tests?: number; accepted_assignments?: number; completed_lessons?: number; progress_marks?: number };

export function StudentGameProgress({ revision }: { revision: number }) {
  const [items, setItems] = useState<Progress[] | null>(null);
  const [error, setError] = useState('');
  const [retry, setRetry] = useState(0);
  const hintID = useId();
  useEffect(() => {
    const controller = new AbortController(); setItems(null); setError('');
    void request<{ items: Progress[] }>('/learning/progress', 'GET', undefined, controller.signal)
      .then(data => { if (!controller.signal.aborted) setItems(data.items); })
      .catch(err => { if (!controller.signal.aborted) setError(message(err)); });
    return () => controller.abort();
  }, [revision, retry]);

  if (error) return <Card><h2>Прогресс</h2><Notice tone="error">{error}</Notice><Button variant="secondary" onClick={() => setRetry(value => value + 1)}>Обновить прогресс</Button></Card>;
  if (!items) return <Card><p role="status">Загружаем твой прогресс…</p></Card>;
  if (!items.length) return null;
  return <>{items.map(item => {
    const complete = item.total > 0 && item.reviewed === item.total;
    const descriptionID = `${hintID}-${item.student_id}`;
    const marks = item.progress_marks ?? item.reviewed;
    return <Card className={`game-progress${complete ? ' game-progress--complete' : ''}`} key={item.student_id}>
      <div className="game-progress-heading"><div><p className="game-eyebrow">ТВОЁ ПРИКЛЮЧЕНИЕ В AIQ</p><h2>Прогресс</h2><p className="muted">{item.student_name}</p></div><span className="game-heart" aria-hidden="true">♥</span></div>
      <div className="game-hp-label"><strong>{complete ? 'Все задания пройдены!' : 'Заряд знаний'}</strong><span aria-hidden="true">{marks ? `Палочек прогресса: ${marks}` : 'Старт приключения'}</span></div>
      <div className="game-mark-track" role="progressbar" aria-label={`Прогресс заданий: ${item.student_name}`} aria-valuemin={0} aria-valuemax={Math.max(1, item.total + (item.completed_lessons ?? 0))} aria-valuenow={marks} aria-valuetext={`Палочек прогресса: ${marks}`} aria-describedby={descriptionID}>
        {Array.from({ length: Math.min(marks, 1000) }, (_, i) => <span className="test-progress-mark" key={i} aria-hidden="true" />)}
      </div>
      <p id={descriptionID}>{(item.completed_tests ?? 0) > 0 ? `Выполнено заданий и тестов: ${item.reviewed} из ${item.total}. Каждый выполненный тест пополняет заряд.` : !item.total ? 'Твоя история начинается здесь. Принятые педагогом работы и выполненные тесты заполнят шкалу.' : complete ? 'Отличная работа! Все текущие задания приняты педагогом.' : `Принято педагогом: ${item.reviewed} из ${item.total}. Каждая принятая работа пополняет заряд.`}</p>
      <dl className="game-progress-stats"><div><dt>Принято</dt><dd>{item.reviewed}</dd></div><div><dt>На проверке</dt><dd>{item.submitted}</dd></div><div><dt>К выполнению</dt><dd>{item.assigned}</dd></div></dl>
      <p>За каждое успешно выполненное дело — одна палочка.</p>
      {item.completed_tests !== undefined && <p>За тесты: {item.completed_tests} · За принятые ДЗ: {item.accepted_assignments ?? item.reviewed - item.completed_tests} · За занятия: {item.completed_lessons ?? 0}{item.failed_tests ? ` · Тестов с ошибками: ${item.failed_tests}` : ''}</p>}
      <p><a href="#/homework-tests">Открыть тесты и ДЗ →</a></p>
      <div className="game-progress-footer"><a href="#/assignments">{item.assigned ? 'К следующему заданию →' : 'Открыть мои задания →'}</a><details><summary>Как пополняется шкала?</summary><p>За успешно пройденный тест, принятое ДЗ и каждое завершённое занятие, на котором отмечено твоё присутствие или опоздание, даётся ровно одна палочка. Повторный просмотр или отправка не добавляют палочки. У теста одна попытка: ответы с ошибками сохраняются без возможности изменения и не дают палочку. Отменённые занятия исключены.</p></details></div>
    </Card>;
  })}</>;
}
