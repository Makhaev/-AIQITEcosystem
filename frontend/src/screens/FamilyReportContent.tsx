import type { FamilyReport } from './FamilyPortal';

const dateTime = (v: string) => new Date(v).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow' });
export type ReportComposition = {
 version?: number; state?: string; published_version?: number; student_id?: number;
 approved_by?: number; approved_at?: string; legacy?: boolean; actions?: string[];
 decisions?: { version: number; action: string; reason: string; actor_id: number; actor_name: string; created_at: string }[];
 snapshot?: {
  student_name: string; from: string; to: string; collected_at: string;
  attendance: { present: number; late: number; absent: number; excused: number; total: number; percent: number | null };
  elements: { assignment_id: number; template_key: string; title: string }[];
  works: { id: number; revision: number; kind: string; title: string; date: string }[];
 };
 next_focus?: string; reviewer_id?: number | null; reviewer_name?: string;
 author_name?: string; author_id?: number; updated_by?: number;
 created_at?: string | null; updated_at?: string; published_at?: string | null; withdrawn_at?: string | null;
};

export function FamilyReportContent({ report: r }: { report: FamilyReport }) {
 const s = r.snapshot;
 return <div className="stack" style={{ overflowWrap: 'anywhere' }}>
  {s && <><p>Ученик: {s.student_name}. Период: {s.from} — {s.to} (обе даты включены, Москва).</p>
   <div><h4>Посещаемость</h4><p>{s.attendance.percent === null ? 'Нет учтённых отметок' : `${s.attendance.percent.toLocaleString('ru-RU', { maximumFractionDigits: 1 })}%`} · Присутствовал: {s.attendance.present}, опоздал: {s.attendance.late}, отсутствовал: {s.attendance.absent}, уважительная причина: {s.attendance.excused}.</p></div>
   <div><h4>Пройденные элементы программы</h4><p>Подтверждённые элементы занятий группы, в состав которых включён ученик. Это не оценка личного освоения.</p>{s.elements.length ? <ul>{s.elements.map(e => <li key={`${e.assignment_id}/${e.template_key}`}>{e.title || e.template_key}</li>)}</ul> : <p>Нет подтверждённых элементов за период.</p>}</div>
   <div><h4>Работы и достижения</h4>{s.works.length ? <ul>{s.works.map(w => <li key={w.id}>{w.kind === 'achievements' ? 'Подтверждённое достижение' : 'Принятая работа'}: {w.title} · {w.date} · версия {w.revision}</li>)}</ul> : <p>Нет принятых работ и подтверждённых достижений за период.</p>}</div>
  </>}
  <div><h4>Подготовленный для семьи текст</h4><p style={{ whiteSpace: 'pre-wrap' }}>{r.body}</p></div>
  {s && <div><h4>Следующий образовательный фокус</h4><p style={{ whiteSpace: 'pre-wrap' }}>{r.next_focus || 'Пока не указан'}</p></div>}
  <p>Версия: {r.version || 1} · Редакция: {r.revision}{r.author_name && ` · Автор: ${r.author_name}`}{` · Проверяющий: ${r.reviewer_name || 'Не назначен'}`}</p>
  {r.approved_at && <p>Утверждено: {dateTime(r.approved_at)} (Москва), сотрудник №{r.approved_by}.</p>}
  {r.legacy && <p>Перенесённая публикация. Сведения о прежнем утверждении не зарегистрированы.</p>}
  {(r.created_at || r.updated_at || r.published_at || r.withdrawn_at || s) && <p>{r.created_at && `Создан: ${dateTime(r.created_at)}. `}{r.updated_at && `Изменён: ${dateTime(r.updated_at)}. `}{r.published_at && `Последняя публикация: ${dateTime(r.published_at)}. `}{r.withdrawn_at && `Последнее снятие публикации: ${dateTime(r.withdrawn_at)}. `}{s && `Данные собраны: ${dateTime(s.collected_at)}. `}(Москва)</p>}
 </div>;
}
