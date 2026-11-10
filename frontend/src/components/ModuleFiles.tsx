import { useEffect, useState, type FormEvent } from 'react';
import { message, request } from '../api';
import { Button, Input, Notice } from './ui';

type Attachment = { id: number; name: string; size: number; created_at: string };
const maxSize = 10 * 1024 * 1024;
function size(bytes: number) { return bytes < 1024 ? `${bytes} Б` : bytes < 1024 * 1024 ? `${(bytes / 1024).toLocaleString('ru-RU', { maximumFractionDigits: 1 })} КБ` : `${(bytes / 1024 / 1024).toLocaleString('ru-RU', { maximumFractionDigits: 1 })} МБ`; }

export function ModuleFiles({ programID, moduleID, editable }: { programID: number; moduleID: number; editable: boolean }) {
  const path = `/programs/${programID}/modules/${moduleID}/files`;
  const [files, setFiles] = useState<Attachment[]>([]); const [loading, setLoading] = useState(true);
  const [error, setError] = useState(''); const [notice, setNotice] = useState(''); const [busy, setBusy] = useState(false);
  const [revision, setRevision] = useState(0); const [removing, setRemoving] = useState<Attachment | null>(null);
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setFiles([]); setError('');
    void request<{ items: Attachment[] }>(path, 'GET', undefined, controller.signal).then(data => { if (!controller.signal.aborted) setFiles(data.items); }).catch(err => { if (!controller.signal.aborted) setError(message(err)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [path, revision]);
  async function upload(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const form = event.currentTarget; const data = new FormData(form); const file = data.get('file');
    setError(''); setNotice('');
    if (!(file instanceof File) || !file.size) { setError('Выберите непустой файл.'); return; }
    if (file.size > maxSize) { setError('Файл должен быть не больше 10 МБ.'); return; }
    setBusy(true);
    try { await request(path, 'POST', data); form.reset(); setNotice('Файл добавлен в модуль.'); setRevision(value => value + 1); }
    catch (err) { setError(message(err)); } finally { setBusy(false); }
  }
  async function remove() {
    if (!removing) return; setError(''); setNotice(''); setBusy(true);
    try { await request(`${path}/${removing.id}`, 'DELETE'); setRemoving(null); setNotice('Файл удалён из модуля.'); setRevision(value => value + 1); }
    catch (err) { setError(message(err)); } finally { setBusy(false); }
  }
  return <section className="module-files stack" aria-label="Файлы модуля"><h3>Файлы модуля</h3>
    <Notice tone="error">{error}</Notice><Notice tone="success">{notice}</Notice>
    {loading ? <p role="status">Загружаем файлы…</p> : files.length ? <ul className="module-files__list">{files.map(file => <li key={file.id}><div><a href={`/api/v1${path}/${file.id}`} download={file.name}>{file.name}</a><p className="muted">{size(file.size)} · {new Date(file.created_at).toLocaleDateString('ru-RU', { timeZone: 'Europe/Moscow' })}</p></div>{editable && <Button variant="ghost" disabled={busy} onClick={() => setRemoving(file)} aria-label={`Удалить файл ${file.name}`}>Удалить</Button>}</li>)}</ul> : !error && <p className="muted">Файлов пока нет.</p>}
    {error && <Button variant="secondary" disabled={busy} onClick={() => setRevision(value => value + 1)}>Обновить список файлов</Button>}
    {editable && <form onSubmit={upload}><fieldset disabled={busy || loading}><Input label="Файл для модуля" name="file" type="file" required hint="До 10 МБ на файл, до 20 файлов в модуле. Документы, изображения и другие учебные материалы." /><Button type="submit" loading={busy}>{busy ? 'Сохраняем…' : 'Загрузить файл'}</Button></fieldset></form>}
    {removing && <div className="notice stack" role="group" aria-label="Удалить файл из модуля?"><p>Удалить файл «{removing.name}»?</p><div className="row-actions"><Button variant="secondary" disabled={busy} onClick={() => setRemoving(null)}>Отмена</Button><Button variant="danger" loading={busy} onClick={() => void remove()}>Удалить файл</Button></div></div>}
  </section>;
}
