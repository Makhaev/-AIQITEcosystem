import { useEffect, useState, type FormEvent } from 'react';
import { message, request, type User } from '../api';
import { Button, Card, EmptyState, Input, Notice, Pagination } from '../components/ui';

type PrivateFile = { id: number; name: string; size: number; media_type: string; created_at: string; can_manage: boolean };
type Files = { items: PrivateFile[]; total: number; page: number; page_size: number };
const MAX_SIZE = 10 * 1024 * 1024;

export function StudentFiles({ studentID, user }: { studentID: number; user: User }) {
  const [data, setData] = useState<Files | null>(null), [page, setPage] = useState(1), [version, setVersion] = useState(0);
  const [error, setError] = useState(''), [busy, setBusy] = useState(false), [loading, setLoading] = useState(true), [success, setSuccess] = useState('');
  useEffect(() => {
    const controller = new AbortController(); setData(null); setLoading(true); setError('');
    request<Files>(`/students/${studentID}/files?page=${page}`, 'GET', undefined, controller.signal)
      .then(value => { if (!controller.signal.aborted) setData(value); })
      .catch(err => { if (!controller.signal.aborted) setError(message(err)); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [studentID, page, version, user.id, user.roles.join(',')]);
  async function upload(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const form = event.currentTarget;
    const file = new FormData(form).get('attachment');
    if (!(file instanceof File) || !file.size || file.size > MAX_SIZE) { setError('Выберите файл от 1 байта до 10 МиБ.'); return; }
    setBusy(true); setError(''); setSuccess('');
    try {
      const content = await new Promise<string>((resolve, reject) => { const reader = new FileReader(); reader.onload = () => resolve(String(reader.result).split(',')[1]); reader.onerror = () => reject(reader.error); reader.readAsDataURL(file); });
      await request('/files', 'POST', { student_id: studentID, name: file.name, data: content });
      form.reset(); setSuccess('Файл загружен. Доступ предоставляется отдельно.'); setPage(1); setVersion(v => v + 1);
    } catch (err) { setError(message(err)); } finally { setBusy(false); }
  }
  async function change(id: number, body?: { user_id: number; allow: boolean }) {
    setBusy(true); setError(''); setSuccess('');
    try { await request(`/files/${id}/${body ? 'access' : 'revoke'}`, body ? 'PUT' : 'POST', body); setSuccess(body ? 'Права на файл обновлены.' : 'Файл отозван. Скачивание больше недоступно.'); setVersion(v => v + 1); }
    catch (err) { setData(null); setError(message(err)); }
    finally { setBusy(false); }
  }
  return <Card><h2>Приватные файлы ученика</h2>
    <p className="muted">До 10 МиБ: PDF, текст UTF-8, PNG/JPEG/GIF/WebP, MP3/WAV/OGG, MP4/WebM. Файл доступен только сотрудникам с отдельным разрешением и текущим доступом к ученику.</p>
    <Notice tone="error">{error}</Notice><Notice tone="success">{success}</Notice>
    <form onSubmit={upload}><Input label="Файл для загрузки" type="file" name="attachment" required disabled={busy || loading} /><Button type="submit" disabled={busy || loading}>Загрузить приватный файл</Button></form>
    {loading ? <p role="status">Загружаем файлы…</p> : !data ? <Button variant="secondary" onClick={() => setVersion(v => v + 1)}>Повторить загрузку файлов</Button> : <>
      {data.items.length ? <ul className="stack">{data.items.map(file => <li key={file.id}>
        <a href={`/api/v1/files/${file.id}/content`}>{file.name}</a> <span className="muted">{new Intl.NumberFormat('ru-RU').format(file.size)} байт</span>
        {file.can_manage && <><form className="filters" onSubmit={event => { event.preventDefault(); const form = new FormData(event.currentTarget); void change(file.id, { user_id: Number(form.get('user_id')), allow: (event.nativeEvent as SubmitEvent).submitter?.getAttribute('data-allow') === 'true' }); }}>
          <Input label={`ID сотрудника для доступа к файлу ${file.name}`} type="number" min="1" name="user_id" required disabled={busy} />
          <Button type="submit" variant="secondary" data-allow="true" disabled={busy}>Предоставить доступ</Button>
          <Button type="submit" variant="secondary" data-allow="false" disabled={busy}>Отозвать доступ сотрудника</Button>
        </form><Button variant="ghost" disabled={busy} onClick={() => { if (window.confirm(`Отозвать файл «${file.name}» для всех? Повторное открытие будет недоступно.`)) void change(file.id); }}>Отозвать файл для всех</Button></>}
      </li>)}</ul> : <EmptyState title="Нет доступных приватных файлов" />}
      <Pagination page={page} pageSize={20} total={data.total} onChange={setPage} disabled={busy} />
    </>}
  </Card>;
}
