import { useId, useState, type FormEvent } from 'react';
import { message, request } from '../api';
import { Button, Input, Notice, SectionHeading } from '../components/ui';
import type { FamilyReport } from './FamilyPortal';

export function FamilyReportEditor({ sid, editing, onSaved, onCancel }: { sid: string; editing: FamilyReport | null; onSaved: (r: FamilyReport) => void; onCancel: () => void }) {
 const [busy, setBusy] = useState(false);
 const [error, setError] = useState('');
 const fieldID = useId();
 async function submit(e: FormEvent<HTMLFormElement>) {
  e.preventDefault(); const form = e.currentTarget; const d = new FormData(form);
  const generate = !editing || (e.nativeEvent as SubmitEvent).submitter?.getAttribute('value') === 'generate';
  const from = String(d.get('from') || ''), to = String(d.get('to') || '');
  if (!from || !to || from > to) { setError('Укажите обе даты в правильном порядке.'); return; }
  setBusy(true); setError('');
  try {
   const r = await request<FamilyReport>(editing ? `/family-reports/${editing.id}` : `/students/${sid}/family-reports`, editing ? 'PUT' : 'POST', {
    from, to, generate, title: d.get('title') || '', body: d.get('body') || '', next_focus: d.get('next_focus') || '',
    reviewer_id: d.get('reviewer_id') ? Number(d.get('reviewer_id')) : null,
    revision: editing?.revision || 0, file_ids: String(d.get('files') || '').split(',').map(v => v.trim()).filter(Boolean).map(Number),
   });
   onSaved(r);
  } catch (err) { setError(message(err)); } finally { setBusy(false); }
 }
 return <form onSubmit={submit} className="stack"><SectionHeading autoFocus={!!editing}>{editing ? 'Изменение отчёта' : 'Собрать новый черновик по шаблону'}</SectionHeading>
  <Notice tone="error">{error}</Notice>
  <p>Шаблон без ИИ использует учебные факты за период. Внутренние заметки и комментарии автоматически не включаются. Проверьте состав и подготовьте текст перед публикацией.</p>
  <fieldset className="stack" disabled={busy}><legend className="sr-only">Данные отчёта</legend><Input label="Начало периода отчёта" name="from" type="date" defaultValue={editing?.snapshot?.from || ''} required />
  <Input label="Окончание периода отчёта" name="to" type="date" defaultValue={editing?.snapshot?.to || ''} required />
  <Input label="ID проверяющего" name="reviewer_id" type="number" min={1} step={1} defaultValue={editing?.reviewer_id || ''} />
  <p>Проверяющий — действующий администратор или руководитель образования, который не создавал и не редактировал эту версию.</p>
  {editing && <><Input label="Название отчёта" name="title" defaultValue={editing.title} maxLength={200} required /><div className="field"><label htmlFor={`${fieldID}-body`}>Текст для семьи</label><textarea id={`${fieldID}-body`} className="input" name="body" defaultValue={editing.body} maxLength={20000} required /></div></>}
  <div className="field"><label htmlFor={`${fieldID}-focus`}>Следующий образовательный фокус</label><textarea id={`${fieldID}-focus`} className="input" name="next_focus" defaultValue={editing?.next_focus || ''} maxLength={5000} /></div>
  <Input label="ID файлов через запятую" name="files" defaultValue={editing?.files.map(f => f.id).join(',') || ''} />
  <Button type="submit" loading={busy}>{editing ? 'Сохранить черновик' : 'Собрать черновик без ИИ'}</Button>
  {editing && <><p>Повторная сборка заменит текст шаблоном и обновит учебные данные. Смена периода также обновляет учебные данные. Прежняя опубликованная версия остаётся доступна семье до замены или отзыва.</p><Button type="submit" variant="secondary" value="generate" disabled={busy}>Пересобрать данные и текст</Button><Button variant="secondary" disabled={busy} onClick={onCancel}>Завершить редактирование</Button></>}
 </fieldset></form>;
}
