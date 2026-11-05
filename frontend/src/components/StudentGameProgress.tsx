import { useEffect, useId, useState, type CSSProperties } from 'react';
import { message, request } from '../api';
import { Button, Card, Notice } from './ui';

type Progress = { student_id: number; student_name: string; total: number; reviewed: number; submitted: number; assigned: number };

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
    const percent = item.total ? Math.round(item.reviewed * 100 / item.total) : 0;
    const complete = item.total > 0 && item.reviewed === item.total;
    const descriptionID = `${hintID}-${item.student_id}`;
    return <Card className={`game-progress${complete ? ' game-progress--complete' : ''}`} key={item.student_id}>
      <div className="game-progress-heading"><div><p className="game-eyebrow">ТВОЁ ПРИКЛЮЧЕНИЕ В AIQ</p><h2>Прогресс</h2><p className="muted">{item.student_name}</p></div><span className="game-heart" aria-hidden="true">♥</span></div>
      <div className="game-hp-label"><strong>{complete ? 'Все задания пройдены!' : 'Заряд знаний'}</strong><span aria-hidden="true">{item.total ? `${percent} / 100 HP` : 'Старт приключения'}</span></div>
      <div className="game-hp-track" role="progressbar" aria-label={`Прогресс заданий: ${item.student_name}`} aria-valuemin={0} aria-valuemax={100} aria-valuenow={item.total ? percent : undefined} aria-valuetext={item.total ? `Принято ${item.reviewed} из ${item.total} работ, ${percent}%` : 'Заданий пока нет'} aria-describedby={descriptionID}>
        <div className="game-hp-fill" style={{ '--hp': `${percent}%` } as CSSProperties} /><div className="game-hp-segments" aria-hidden="true" />
      </div>
      <p id={descriptionID}>{!item.total ? 'Твоя история начинается здесь. Принятые педагогом работы заполнят шкалу.' : complete ? 'Отличная работа! Все текущие задания приняты педагогом.' : `Принято педагогом: ${item.reviewed} из ${item.total}. Каждая принятая работа пополняет заряд.`}</p>
      <dl className="game-progress-stats"><div><dt>Принято</dt><dd>{item.reviewed}</dd></div><div><dt>На проверке</dt><dd>{item.submitted}</dd></div><div><dt>К выполнению</dt><dd>{item.assigned}</dd></div></dl>
      <div className="game-progress-footer"><a href="#/assignments">{item.assigned ? 'К следующему заданию →' : 'Открыть мои задания →'}</a><details><summary>Как пополняется шкала?</summary><p>HP — доля принятых работ среди всех твоих заданий. Ответы на проверке ещё не учитываются как принятые. Новое задание увеличивает общее число работ, поэтому процент может измениться. Отменённые занятия исключены.</p></details></div>
    </Card>;
  })}</>;
}
