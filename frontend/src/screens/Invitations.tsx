import { useEffect, useState, type FormEvent } from 'react';
import { APIError, getCSRF, message, request, type User } from '../api';
import { Brand } from '../components/Layout';
import { Button, Card, EmptyState, Input, Notice, PageHeading, Pagination, PasswordInput, Select, Table, type Column } from '../components/ui';
import { PasswordForm, passwordError } from './Auth';

type Invitation = { id: number; email: string; role: 'parent' | 'student'; card_id: number; created_by: number; created_at: string; expires_at: string; status: 'pending' | 'expired' | 'revoked' | 'used' };
type InvitationPage = { items: Invitation[]; total: number; page: number; page_size: number };
type Preview = { email: string; role: 'parent' | 'student'; expires_at: string; requires_login: boolean };
type Link = { role: 'parent' | 'student'; card_id: number; name: string };
const roleName = { parent: 'Родитель', student: 'Ученик' };
const statuses = { pending: 'Ожидает принятия', expired: 'Срок истёк', revoked: 'Отозвано', used: 'Использовано' };
const date = (value: string) => new Intl.DateTimeFormat('ru-RU', { timeZone: 'Europe/Moscow', dateStyle: 'short', timeStyle: 'short' }).format(new Date(value));

export function Invitations() {
  const [data, setData] = useState<InvitationPage | null>(null), [page, setPage] = useState(1), [version, setVersion] = useState(0);
  const [role, setRole] = useState<'parent' | 'student'>('parent'), [search, setSearch] = useState(''), [cardPage, setCardPage] = useState(1), [selected, setSelected] = useState('');
  const [cards, setCards] = useState<{ items: { id: number; name: string }[]; total: number } | null>(null), [cardError, setCardError] = useState('');
  const [error, setError] = useState(''), [success, setSuccess] = useState(''), [busy, setBusy] = useState(false);
  useEffect(() => { const c = new AbortController(); setData(null); request<InvitationPage>(`/invitations?page=${page}`, 'GET', undefined, c.signal).then(setData).catch(e => { if (!c.signal.aborted) setError(message(e)); }); return () => c.abort(); }, [page, version]);
  useEffect(() => { const c = new AbortController(); setCards(null); setCardError(''); request<{ items: { id: number; name: string }[]; total: number }>(`/${role === 'parent' ? 'parents' : 'students'}?status=active&page=${cardPage}&page_size=20&search=${encodeURIComponent(search)}`, 'GET', undefined, c.signal).then(setCards).catch(e => { if (!c.signal.aborted) setCardError(message(e)); }); return () => c.abort(); }, [role, search, cardPage, version]);
  async function send(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const form = event.currentTarget; const input = new FormData(form); setBusy(true); setError(''); setSuccess('');
    try { await request('/invitations', 'POST', { email: input.get('email'), role, card_id: Number(selected) }); setSuccess('Приглашение создано. Письмо передано почтовому серверу; ссылка действует 48 часов.'); form.reset(); setSelected(''); setPage(1); }
    catch (e) { setError(message(e)); } finally { setBusy(false); setVersion(v => v + 1); }
  }
  async function action(inv: Invitation, kind: 'resend' | 'revoke') {
    if (!window.confirm(kind === 'resend' ? `Отправить новое приглашение на ${inv.email}? Предыдущая неиспользованная ссылка перестанет действовать.` : 'Отозвать приглашение? Его ссылка перестанет действовать.')) return;
    setBusy(true); setError(''); setSuccess('');
    try { await request(`/invitations/${inv.id}/${kind}`, 'POST', {}); setSuccess(kind === 'resend' ? 'Новое письмо передано почтовому серверу. Прежняя ссылка отозвана.' : 'Приглашение отозвано.'); setPage(1); }
    catch (e) { setError(message(e)); } finally { setBusy(false); setVersion(v => v + 1); }
  }
  const columns: Column<Invitation>[] = [
    { key: 'recipient', title: 'Получатель', render: i => <>{i.email}<p>{roleName[i.role]} · <a href={`#/${i.role === 'parent' ? 'parents' : 'students'}/${i.card_id}`}>Карточка №{i.card_id}</a></p></> },
    { key: 'dates', title: 'Срок (МСК)', render: i => <>Создано: {date(i.created_at)}<p>До: {date(i.expires_at)}</p><p>Автор №{i.created_by}</p></> },
    { key: 'status', title: 'Статус', render: i => statuses[i.status] },
    { key: 'actions', title: 'Действия', render: i => i.status === 'used' ? 'Связь активирована' : <div className="row-actions"><Button variant="secondary" disabled={busy} onClick={() => void action(i, 'resend')}>Отправить повторно</Button>{i.status !== 'revoked' && <Button variant="ghost" disabled={busy} onClick={() => void action(i, 'revoke')}>Отозвать</Button>}</div> },
  ];
  return <><PageHeading title="Приглашения" description="Подключение родителей и учеников к существующим карточкам по email." /><div className="stack"><Notice tone="error">{error}</Notice><Notice tone="success">{success}</Notice>
    <Card><h2>Новое приглашение</h2><p>Ссылка действует 48 часов и используется один раз. Совпадение контактов не создаёт семейную связь. Повторное приглашение для этого email и роли или этой карточки отзывает прежнюю неиспользованную ссылку.</p>
      <form onSubmit={send} className="stack"><Input label="Email получателя" type="email" name="email" required maxLength={254} disabled={busy} />
        <Select label="Роль получателя" value={role} disabled={busy} onChange={e => { setRole(e.target.value as 'parent' | 'student'); setSelected(''); setSearch(''); setCardPage(1); }}><option value="parent">Родитель</option><option value="student">Ученик</option></Select>
        <Input label="Поиск существующей карточки" value={search} maxLength={200} disabled={busy} onChange={e => { setSearch(e.target.value); setCardPage(1); setSelected(''); }} />
        <Notice tone="error">{cardError}</Notice>{cardError && <Button variant="secondary" onClick={() => setVersion(v => v + 1)}>Повторить поиск карточек</Button>}
        <Select label="Существующая карточка" value={selected} required disabled={busy || !cards} onChange={e => setSelected(e.target.value)}><option value="">{cards ? 'Выберите карточку' : 'Загрузка…'}</option>{cards?.items.map(c => <option value={c.id} key={c.id}>{c.name} · №{c.id}</option>)}</Select>
        {cards && <Pagination page={cardPage} pageSize={20} total={cards.total} disabled={busy} onChange={p => { setCardPage(p); setSelected(''); }} />}
        <Button type="submit" disabled={busy || !selected || !!cardError}>{busy ? 'Отправляем…' : 'Отправить приглашение'}</Button>
      </form>
    </Card>
    <Card className="table-card"><div className="table-heading"><h2>История приглашений</h2></div>{!data ? error ? <Button onClick={() => { setError(''); setVersion(v => v + 1); }}>Повторить загрузку</Button> : <p role="status">Загружаем приглашения…</p> : <>{data.items.length ? <Table caption="Приглашения родителей и учеников" columns={columns} items={data.items} rowKey={i => i.id} /> : <EmptyState title="Приглашений пока нет" />}<Pagination page={page} pageSize={20} total={data.total} disabled={busy} onChange={setPage} /></>}</Card>
  </div></>;
}

export function AccountLinks() {
  const [items, setItems] = useState<Link[] | null>(null), [error, setError] = useState(''), [reload, setReload] = useState(0);
  useEffect(() => { const c = new AbortController(); setError(''); request<{ items: Link[] }>('/account/links', 'GET', undefined, c.signal).then(v => setItems(v.items)).catch(e => { if (!c.signal.aborted) setError(message(e)); }); return () => c.abort(); }, [reload]);
  return <Card><h2>Подключённые карточки</h2><Notice tone="error">{error}</Notice>{error ? <Button onClick={() => setReload(v => v + 1)}>Повторить загрузку связей</Button> : !items ? <p role="status">Загружаем связи…</p> : items.length ? <ul>{items.map(i => <li key={i.role}>{roleName[i.role]}: {i.name} · карточка №{i.card_id}</li>)}</ul> : <p>Активных привязок нет.</p>}<p>Подключение аккаунта подтверждено для указанных карточек. Учебные разделы личного кабинета будут доступны после их подключения.</p></Card>;
}

export function AcceptInvitation({ token, user, onUser }: { token: string; user: User | null; onUser: (user: User | null) => void }) {
  const [preview, setPreview] = useState<Preview | null>(null), [error, setError] = useState(''), [busy, setBusy] = useState(false), [reload, setReload] = useState(0), [accepted, setAccepted] = useState(false);
  useEffect(() => { const c = new AbortController(); setPreview(null); setError(''); if (!token) { setError('В ссылке нет токена приглашения. Откройте ссылку из письма или обратитесь к администратору за новым приглашением.'); return; } request<Preview>('/invitations/inspect', 'POST', { token }, c.signal).then(v => { if (!c.signal.aborted) setPreview(v); }).catch(e => { if (!c.signal.aborted) setError(message(e)); }); return () => c.abort(); }, [token, reload]);
  async function logout() { setBusy(true); setError(''); try { await request('/auth/logout', 'POST'); onUser(null); await getCSRF(); setReload(v => v + 1); } catch (e) { setError(message(e)); } finally { setBusy(false); } }
  async function login(event: FormEvent<HTMLFormElement>) { event.preventDefault(); if (!preview) return; const f = new FormData(event.currentTarget); setBusy(true); setError(''); try { const value = await request<{ user: User }>('/auth/login', 'POST', { email: preview.email, password: f.get('password') }); onUser(value.user); } catch (e) { setError(message(e)); } finally { setBusy(false); } }
  async function accept(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (!preview) return; const form = new FormData(event.currentTarget), password = String(form.get('password') || ''); setError('');
    if (!user) { const invalid = passwordError(password); if (invalid || password !== form.get('confirmation')) { setError(invalid || 'Пароли не совпадают.'); return; } }
    setBusy(true);
    try {
      await request('/invitations/accept', 'POST', user ? { token } : { token, password });
      // Activation has already committed; a later login failure must not invite a retry of the token.
      try { const current = user ? await request<User>('/auth/me') : (await request<{ user: User }>('/auth/login', 'POST', { email: preview.email, password })).user; onUser(current); }
      catch { onUser(null); }
      window.history.replaceState(null, '', '#/invitation');
      setAccepted(true);
    } catch (e) { setError(message(e)); if (e instanceof APIError && e.code === 'invitation_login_required') setPreview({ ...preview, requires_login: true }); }
    finally { setBusy(false); }
  }
  const mismatch = user && preview && user.email.toLowerCase() !== preview.email;
  return <main className="boot"><Brand /><PageHeading title="Принятие приглашения" /><Card><Notice tone="error">{error}</Notice>
    {accepted ? <><h2>Приглашение принято</h2><Notice tone="success">Аккаунт связан с назначенной карточкой.</Notice><a href="#/account">{user ? 'Открыть мой аккаунт' : 'Войти в аккаунт'}</a></> : !preview ? <>{!error && <p role="status">Проверяем приглашение…</p>}{error && token && <Button onClick={() => { void getCSRF().then(() => setReload(v => v + 1)).catch(e => setError(message(e))); }}>Проверить ещё раз</Button>}<p>Использованную, отозванную или просроченную ссылку нужно заменить новым приглашением администратора.</p></> : <>
      <p>Получатель: <strong>{preview.email}</strong></p><p>Роль: {roleName[preview.role]} · действует до {date(preview.expires_at)} (МСК)</p>
      {mismatch ? <><p>Сейчас выполнен вход как {user.email}. Для принятия нужен аккаунт с email приглашения.</p><Button disabled={busy} onClick={() => void logout()}>Выйти из другого аккаунта</Button></> : user?.must_change_password ? <PasswordForm forced onChanged={() => { onUser(null); void getCSRF().then(() => setReload(v => v + 1)).catch(e => setError(message(e))); }} /> : !user && preview.requires_login ? <form onSubmit={login}><p>Аккаунт уже существует. Войдите в него; пароль при принятии не изменяется.</p><PasswordInput label="Пароль существующего аккаунта" name="password" required autoComplete="current-password" disabled={busy} /><Button type="submit" loading={busy}>Войти для принятия</Button></form> : <form onSubmit={accept}>
        {!user && <><PasswordInput label="Новый пароль" name="password" required minLength={12} maxLength={72} autoComplete="new-password" disabled={busy} hint="Не менее 12 символов и не более 72 байт UTF-8." /><PasswordInput label="Повторите пароль" name="confirmation" required minLength={12} maxLength={72} autoComplete="new-password" disabled={busy} /></>}
        <p>Роль и карточка определены приглашением администратора.</p><Button type="submit" loading={busy}>{busy ? 'Подключаем…' : 'Принять приглашение'}</Button>
      </form>}
    </>}
  </Card></main>;
}
