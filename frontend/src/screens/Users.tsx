import { useEffect, useRef, useState, type FormEvent } from 'react';
import { APIError, message, request, roles, type Page, type Role, type User } from '../api';
import { Badge, Button, Card, EmptyState, Input, Modal, Notice, PageHeading, Pagination, Table, type Column } from '../components/ui';
import { passwordError } from './Auth';

function UserEditor({ user, close, saved }: { user?: User; close: () => void; saved: (user: User) => void }) {
  const [error, setError] = useState(''); const [fields, setFields] = useState<Record<string, string>>({}); const [busy, setBusy] = useState(false); const [selected, setSelected] = useState<Role[]>(user?.roles ?? ['teacher']);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const data = new FormData(event.currentTarget); setError(''); setFields({});
    const password = String(data.get('password') ?? ''); const invalid = password && passwordError(password);
    if (invalid) { setError('Проверьте введённые данные.'); setFields({ password: invalid }); return; }
    if (!selected.length) { setError('Выберите хотя бы одну роль.'); return; }
    setBusy(true); const body: Record<string, unknown> = { name: String(data.get('name') ?? '').trim() };
    if (!user) { body.email = data.get('email'); body.password = password; body.roles = selected; }
    else { if ([...selected].sort().join() !== [...user.roles].sort().join()) body.roles = selected; if (password) body.password = password; }
    try { saved(await request<User>(user ? `/users/${user.id}` : '/users', user ? 'PATCH' : 'POST', body)); }
    catch (err) { setError(err instanceof APIError ? err.message : message(err)); if (err instanceof APIError) setFields(err.fields ?? {}); } finally { setBusy(false); }
  }
  return <Modal title={user ? 'Изменить пользователя' : 'Новый пользователь'} description="Пользователь сменит временный пароль при первом входе." onClose={close} busy={busy}><Notice tone="error">{error}</Notice><form onSubmit={submit}><Input label="Имя и фамилия" name="name" defaultValue={user?.name} required maxLength={200} disabled={busy} autoFocus error={fields.name} /><Input label="Email" name="email" type="email" defaultValue={user?.email} required maxLength={254} disabled={busy || !!user} autoComplete="off" error={fields.email} /><fieldset disabled={busy}><legend>Роли</legend><div className="role-options">{(Object.keys(roles) as Role[]).map(role => <label key={role} className="checkbox"><input name="roles" type="checkbox" value={role} checked={selected.includes(role)} onChange={event => setSelected(event.target.checked ? [...selected, role] : selected.filter(value => value !== role))} />{roles[role]}</label>)}</div>{fields.roles && <p className="field-error">{fields.roles}</p>}</fieldset><Input label={user ? 'Новый временный пароль' : 'Временный пароль'} hint={user ? 'Оставьте пустым, чтобы сохранить пароль. Сброс пароля завершит все сессии пользователя.' : 'Не менее 12 символов. Не более 72 байт UTF-8.'} name="password" type="password" autoComplete="new-password" required={!user} minLength={12} maxLength={72} disabled={busy} error={fields.password} /><div className="modal-footer"><Button variant="secondary" onClick={close} disabled={busy}>Отмена</Button><Button type="submit" loading={busy} disabled={!selected.length}>{busy ? 'Сохраняем…' : user ? 'Сохранить изменения' : 'Создать пользователя'}</Button></div></form></Modal>;
}

export function Users({ currentUser, sessionEnded }: { currentUser: User; sessionEnded: () => void }) {
  const [data, setData] = useState<Page | null>(null); const [error, setError] = useState(''); const [success, setSuccess] = useState(''); const [loading, setLoading] = useState(true); const [page, setPage] = useState(1); const [version, setVersion] = useState(0); const [editor, setEditor] = useState<User | 'new' | null>(null); const [confirm, setConfirm] = useState<User | null>(null); const [busy, setBusy] = useState(false); const refresh = () => setVersion(value => value + 1); const active = useRef(true);
  useEffect(() => { active.current = true; return () => { active.current = false; }; }, []);
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError('');
    request<Page>(`/users?page=${page}&page_size=20`, 'GET', undefined, controller.signal).then(setData).catch(err => { if (!controller.signal.aborted) setError(message(err)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [page, version]);
  async function toggle() {
    if (!confirm) return; setBusy(true); setError(''); setSuccess('');
    try { await request(`/users/${confirm.id}`, 'PATCH', { blocked: !confirm.blocked }); if (confirm.id === currentUser.id && !confirm.blocked) { sessionEnded(); return; } setSuccess(confirm.blocked ? 'Пользователь разблокирован.' : 'Пользователь заблокирован. Все его сессии завершены.'); setConfirm(null); refresh(); }
    catch (err) { setError(message(err)); } finally { if (active.current) setBusy(false); }
  }
  const columns: Column<User>[] = [
    { key: 'person', title: 'Пользователь', render: user => <><strong>{user.name}</strong><span className="muted">{user.email}</span></> },
    { key: 'roles', title: 'Роли', render: user => <div className="tags">{user.roles.map(role => <Badge key={role}>{roles[role]}</Badge>)}</div> },
    { key: 'status', title: 'Статус', render: user => <><Badge tone={user.blocked ? 'danger' : 'success'}>{user.blocked ? 'Заблокирован' : 'Активен'}</Badge>{user.must_change_password && <Badge tone="warning">Смена пароля</Badge>}</> },
    { key: 'actions', title: 'Действия', render: user => <div className="row-actions"><Button variant="secondary" onClick={() => setEditor(user)} disabled={busy} aria-label={`Изменить пользователя ${user.name}`}>Изменить</Button><Button variant="ghost" onClick={() => { setError(''); setConfirm(user); }} disabled={busy} aria-label={`${user.blocked ? 'Разблокировать' : 'Заблокировать'} пользователя ${user.name}`}>{user.blocked ? 'Разблокировать' : 'Блокировать'}</Button></div> },
  ];
  return <><PageHeading title="Пользователи" description="Учётные записи команды и права доступа к рабочему пространству." action={<Button onClick={() => { setSuccess(''); setEditor('new'); }}>Добавить пользователя</Button>} /><div className="stack"><Notice tone="error">{!confirm && error}</Notice><Notice tone="success">{success}</Notice><Card className="table-card" aria-busy={loading}><div className="table-heading"><h2>Команда AIQ</h2><span className="muted">Всего: {data?.total ?? '—'}</span></div>{loading ? <p className="empty-state" role="status">Загружаем пользователей…</p> : error ? <EmptyState title="Не удалось загрузить список" action={<Button variant="secondary" onClick={refresh}>Повторить загрузку</Button>} /> : data?.items.length ? <Table caption="Пользователи AIQ" columns={columns} items={data.items} rowKey={user => user.id} /> : <EmptyState title="Пользователей пока нет" description="Добавьте первую учётную запись команды." />}{data && <Pagination page={page} total={data.total} pageSize={20} onChange={setPage} disabled={loading} />}</Card></div>{editor && <UserEditor user={editor === 'new' ? undefined : editor} close={() => setEditor(null)} saved={user => { const old = editor === 'new' ? undefined : editor; setEditor(null); if (user.id === currentUser.id && ([...(old?.roles ?? [])].sort().join() !== [...user.roles].sort().join() || user.must_change_password)) { sessionEnded(); return; } setSuccess(old ? 'Изменения сохранены.' : 'Пользователь создан. Передайте ему временный пароль лично.'); refresh(); }} />}{confirm && <Modal title={confirm.blocked ? 'Разблокировать пользователя?' : 'Заблокировать пользователя?'} description={confirm.blocked ? `${confirm.name} сможет снова войти в систему.` : `${confirm.name} потеряет доступ к системе. Все его сессии завершатся.`} onClose={() => { setConfirm(null); setError(''); }} busy={busy}><Notice tone="error">{error}</Notice><div className="modal-footer"><Button variant="secondary" onClick={() => { setConfirm(null); setError(''); }} disabled={busy}>Отмена</Button><Button variant={confirm.blocked ? 'primary' : 'danger'} onClick={() => void toggle()} loading={busy}>{busy ? 'Сохраняем…' : confirm.blocked ? 'Разблокировать' : 'Заблокировать'}</Button></div></Modal>}</>;
}
