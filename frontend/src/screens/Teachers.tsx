import { useEffect, useState, type FormEvent } from 'react';
import { APIError, message, request, type Page as UsersPage, type User } from '../api';
import { Badge, Button, Card, EmptyState, Input, Modal, Notice, PageHeading, Pagination, Select, Table, type Column } from '../components/ui';

type Teacher = {
  id: number; name: string; phone: string; email: string; specialization: string;
  status: 'active' | 'archived'; user_id: number | null; created_at: string; updated_at: string;
};
type TeacherPage = { items: Teacher[]; total: number; page: number; page_size: number };

function TeacherStatus({ status }: { status: Teacher['status'] }) {
  return <Badge tone={status === 'active' ? 'success' : 'neutral'}>{status === 'active' ? 'Активен' : 'В архиве'}</Badge>;
}

function TeacherEditor({ teacher, onClose, onSaved }: { teacher?: Teacher; onClose: () => void; onSaved: () => void }) {
  const [users, setUsers] = useState<User[]>([]);
  const [loading, setLoading] = useState(true);
  const [lookupError, setLookupError] = useState('');
  const [error, setError] = useState('');
  const [fields, setFields] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    (async () => {
      try {
        const options: User[] = [];
        let page = 1, total = 0;
        do {
          const result = await request<UsersPage>(`/users?role=teacher&page=${page}&page_size=100`, 'GET', undefined, controller.signal);
          options.push(...result.items); total = result.total; page++;
        } while (options.length < total && !controller.signal.aborted);
        if (!controller.signal.aborted) setUsers(options);
      } catch (err) { if (!controller.signal.aborted) setLookupError(message(err)); }
      finally { if (!controller.signal.aborted) setLoading(false); }
    })();
    return () => controller.abort();
  }, []);
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    setBusy(true); setError(''); setFields({});
    const body = {
      name: String(data.get('name') ?? '').trim(), phone: data.get('phone'), email: data.get('email'),
      specialization: data.get('specialization'), status: data.get('status'),
      user_id: data.get('user_id') ? Number(data.get('user_id')) : null,
    };
    try { await request(teacher ? `/teachers/${teacher.id}` : '/teachers', teacher ? 'PATCH' : 'POST', body); onSaved(); }
    catch (err) {
      setError(err instanceof APIError ? err.message : message(err));
      if (err instanceof APIError) setFields(err.fields ?? {});
    } finally { setBusy(false); }
  }
  return <Modal title={teacher ? 'Изменить педагога' : 'Новый педагог'} description="Профиль педагога и его учётная запись — отдельные записи. Связь с аккаунтом необязательна." onClose={onClose} busy={busy}>
    <Notice tone="error">{lookupError || error}</Notice>
    <form onSubmit={save}>
      <Input label="Имя и фамилия" name="name" defaultValue={teacher?.name} required maxLength={200} disabled={busy} autoFocus error={fields.name} />
      <Input label="Телефон" name="phone" type="tel" autoComplete="tel" placeholder="+7 900 123-45-67" defaultValue={teacher?.phone} maxLength={100} disabled={busy} error={fields.phone} hint="Необязательно. Российский номер с началом 8 будет сохранён в формате +7." />
      <Input label="Email" name="email" type="email" autoComplete="email" defaultValue={teacher?.email} maxLength={254} disabled={busy} error={fields.email} />
      <Input label="Специализация" name="specialization" defaultValue={teacher?.specialization} maxLength={500} disabled={busy} error={fields.specialization} />
      <Select label="Статус" name="status" defaultValue={teacher?.status ?? 'active'} disabled={busy} error={fields.status}>
        <option value="active">Активен</option><option value="archived">В архиве</option>
      </Select>
      <Select label="Учётная запись" name="user_id" defaultValue={teacher?.user_id == null ? '' : String(teacher.user_id)} disabled={busy || loading || !!lookupError} error={fields.user_id} hint="Выберите пользователя с ролью педагога. Один аккаунт можно связать только с одним профилем.">
        <option value="">{loading ? 'Загрузка учётных записей…' : 'Без учётной записи'}</option>
        {teacher?.user_id != null && !users.some(user => user.id === teacher.user_id) && <option value={teacher.user_id}>Текущая учётная запись</option>}
        {users.map(user => <option value={user.id} key={user.id} disabled={user.blocked}>{user.name} · {user.email}{user.blocked ? ' · Заблокирован' : ''}</option>)}
      </Select>
      <div className="modal-footer"><Button variant="secondary" onClick={onClose} disabled={busy}>Отмена</Button><Button type="submit" loading={busy} disabled={loading || !!lookupError}>{busy ? 'Сохраняем…' : 'Сохранить педагога'}</Button></div>
    </form>
  </Modal>;
}

function TeacherDetails({ id, close }: { id: number; close: () => void }) {
  const [teacher, setTeacher] = useState<Teacher | null>(null); const [error, setError] = useState('');
  useEffect(() => { const controller = new AbortController(); request<Teacher>(`/teachers/${id}`, 'GET', undefined, controller.signal).then(setTeacher).catch(err => { if (!controller.signal.aborted) setError(message(err)); }); return () => controller.abort(); }, [id]);
  return <Modal title={teacher?.name ?? 'Профиль педагога'} onClose={close}><Notice tone="error">{error}</Notice>{teacher ? <dl className="detail-list">
    <div><dt>Статус</dt><dd><TeacherStatus status={teacher.status} /></dd></div>
    <div><dt>Телефон</dt><dd>{teacher.phone || 'Не указан'}</dd></div>
    <div><dt>Email</dt><dd>{teacher.email || 'Не указан'}</dd></div>
    <div><dt>Специализация</dt><dd>{teacher.specialization || 'Не указана'}</dd></div>
    <div><dt>Учётная запись</dt><dd>{teacher.user_id == null ? 'Не связана' : 'Связана с профилем'}</dd></div>
  </dl> : !error && <p role="status">Загружаем профиль…</p>}<div className="modal-footer"><Button variant="secondary" onClick={close}>Закрыть</Button></div></Modal>;
}

export function Teachers({ user }: { user: User }) {
  const admin = user.roles.includes('admin');
  const [data, setData] = useState<TeacherPage | null>(null); const [page, setPage] = useState(1);
  const [filters, setFilters] = useState(''); const [version, setVersion] = useState(0);
  const [loading, setLoading] = useState(true); const [error, setError] = useState(''); const [success, setSuccess] = useState('');
  const [editor, setEditor] = useState<Teacher | 'new' | null>(null); const [details, setDetails] = useState<number | null>(null);
  const [archive, setArchive] = useState<Teacher | null>(null); const [archiveError, setArchiveError] = useState(''); const [busy, setBusy] = useState(false);
  const refresh = () => setVersion(value => value + 1);
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError('');
    request<TeacherPage>(`/teachers?page=${page}&page_size=20${filters}`, 'GET', undefined, controller.signal).then(setData)
      .catch(err => { if (!controller.signal.aborted) setError(message(err)); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [page, filters, version]);
  function apply(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const data = new FormData(event.currentTarget); const query = new URLSearchParams();
    for (const key of ['search', 'status']) { const value = String(data.get(key) ?? '').trim(); if (value) query.set(key, value); }
    setPage(1); setFilters(query.size ? `&${query}` : '');
  }
  async function archiveProfile() {
    if (!archive) return; setBusy(true); setArchiveError('');
    try { await request(`/teachers/${archive.id}`, 'PATCH', { status: 'archived' }); setArchive(null); setSuccess('Педагог перенесён в архив. Его профиль и история сохранены.'); refresh(); }
    catch (err) { setArchiveError(message(err)); } finally { setBusy(false); }
  }
  const columns: Column<Teacher>[] = [
    { key: 'person', title: 'Педагог', render: teacher => <><strong>{teacher.name}</strong><span className="muted">{teacher.specialization || 'Специализация не указана'}</span></> },
    { key: 'contacts', title: 'Контакты', render: teacher => <><span>{teacher.phone || 'Телефон не указан'}</span><span>{teacher.email || 'Email не указан'}</span></> },
    { key: 'status', title: 'Статус', render: teacher => <><TeacherStatus status={teacher.status} /><span className="muted">{teacher.user_id == null ? 'Без учётной записи' : 'Учётная запись связана'}</span></> },
    { key: 'actions', title: 'Действия', render: teacher => <div className="row-actions">
      <Button variant="secondary" onClick={() => setDetails(teacher.id)} aria-label={`Открыть профиль ${teacher.name}`}>Подробнее</Button>
      {admin && <><Button variant="secondary" onClick={() => setEditor(teacher)} aria-label={`Изменить педагога ${teacher.name}`}>Изменить</Button>{teacher.status === 'active' && <Button variant="ghost" onClick={() => { setArchiveError(''); setArchive(teacher); }} aria-label={`Архивировать педагога ${teacher.name}`}>В архив</Button>}</>}
    </div> },
  ];
  return <><PageHeading title="Педагоги" description={user.roles.some(role => role === 'admin' || role === 'leader') ? 'Профили, контакты и специализации команды педагогов.' : 'Ваш профиль педагога и контактные данные.'} action={admin && <Button onClick={() => setEditor('new')}>Добавить педагога</Button>} />
    <form className="filters" onSubmit={apply}><Input label="Поиск по ФИО" name="search" maxLength={200} placeholder="Имя или фамилия" /><Select label="Статус" name="status"><option value="">Все статусы</option><option value="active">Активен</option><option value="archived">В архиве</option></Select><Button type="submit" variant="secondary">Применить</Button></form>
    <div className="stack"><Notice tone="success">{success}</Notice><Notice tone="error">{error}</Notice><Card className="table-card" aria-busy={loading}>
      <div className="table-heading"><h2>Справочник педагогов</h2><span className="muted">Всего: {data?.total ?? '—'}</span></div>
      {loading ? <p className="empty-state" role="status">Загружаем педагогов…</p> : error ? <EmptyState title="Не удалось загрузить педагогов" action={<Button variant="secondary" onClick={refresh}>Повторить загрузку</Button>} /> : data?.items.length ? <Table caption="Педагоги AIQ" items={data.items} columns={columns} rowKey={teacher => teacher.id} /> : <EmptyState title="Педагогов пока нет" description="Если применены фильтры, попробуйте изменить их." />}
      {data && <Pagination page={page} total={data.total} pageSize={20} onChange={setPage} disabled={loading} />}
    </Card></div>
    {editor && <TeacherEditor teacher={editor === 'new' ? undefined : editor} onClose={() => setEditor(null)} onSaved={() => { setEditor(null); setSuccess('Профиль педагога сохранён.'); refresh(); }} />}
    {details != null && <TeacherDetails id={details} close={() => setDetails(null)} />}
    {archive && <Modal title="Перенести педагога в архив?" description={`${archive.name} останется в истории. Архивного педагога нельзя назначать на новые занятия.`} onClose={() => setArchive(null)} busy={busy}><Notice tone="error">{archiveError}</Notice><div className="modal-footer"><Button variant="secondary" onClick={() => setArchive(null)} disabled={busy}>Отмена</Button><Button onClick={() => void archiveProfile()} loading={busy}>Перенести в архив</Button></div></Modal>}
  </>;
}
