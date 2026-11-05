import { useEffect, useState, type FormEvent } from 'react';
import { message, request } from '../api';
import { Brand } from '../components/Layout';
import { Button, Card, Input, Notice, PageHeading, PasswordInput, Select } from '../components/ui';
import { passwordError } from './Auth';

type Option = { id: number; name: string; email?: string; blocked?: boolean };
type Link = { url: string; expires_at: string };
type Recovery = { user_id: number; name: string; email: string; requested_at: string };
async function options(path: string, signal: AbortSignal) {
  const items: Option[] = [];
  for (let page = 1; ; page++) {
    const result = await request<{ items: Option[]; total: number }>(`${path}?page_size=100&page=${page}`, 'GET', undefined, signal);
    items.push(...result.items);
    if (items.length >= result.total || !result.items.length) return items;
  }
}
function LinkResult({ value }: { value: Link | null }) {
  const [notice, setNotice] = useState('');
  if (!value) return null;
  return <Card><h2>Одноразовая ссылка готова</h2><p>Передайте её пользователю лично после проверки получателя. Ссылка действует до {new Date(value.expires_at).toLocaleString('ru-RU', { timeZone: 'Europe/Moscow' })} МСК.</p><Input label="Ссылка доступа" value={value.url} readOnly onFocus={event => event.currentTarget.select()} /><Button variant="secondary" onClick={() => { void navigator.clipboard.writeText(value.url).then(() => setNotice('Ссылка скопирована.')).catch(() => setNotice('Выделите и скопируйте ссылку из поля.')); }}>Скопировать ссылку</Button><Notice>{notice}</Notice></Card>;
}
export function Invitations() {
  const [role, setRole] = useState('parent'); const [profiles, setProfiles] = useState<Option[]>([]); const [users, setUsers] = useState<Option[]>([]); const [requests, setRequests] = useState<Recovery[]>([]);
  const [profile, setProfile] = useState(''); const [recoveryID, setRecoveryID] = useState(''); const [error, setError] = useState(''); const [loading, setLoading] = useState(true); const [busy, setBusy] = useState(false); const [version, setVersion] = useState(0); const [link, setLink] = useState<Link | null>(null);
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError(''); setProfiles([]); setProfile('');
    const path = role === 'parent' ? '/parents' : role === 'student' ? '/students' : '/teachers';
    void Promise.all([options(path, controller.signal), options('/users', controller.signal), request<{ items: Recovery[] }>('/access-requests', 'GET', undefined, controller.signal)])
      .then(([p, u, r]) => { setProfiles(p); setUsers(u); setRequests(r.items); }).catch(err => { if (!controller.signal.aborted) setError(message(err)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [role, version]);
  async function invite(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const form = event.currentTarget; const data = new FormData(form); const selected = profiles.find(p => p.id === Number(profile)); if (!selected) return;
    setBusy(true); setError(''); setLink(null);
    try { setLink(await request<Link>('/invitations', 'POST', { role, profile_id: selected.id, name: selected.name, email: data.get('email') })); form.reset(); setVersion(v => v + 1); }
    catch (err) { setError(message(err)); } finally { setBusy(false); }
  }
  async function recover(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setBusy(true); setError(''); setLink(null);
    try { setLink(await request<Link>('/access-links', 'POST', { user_id: Number(recoveryID) })); setVersion(v => v + 1); }
    catch (err) { setError(message(err)); } finally { setBusy(false); }
  }
  return <><PageHeading title="Приглашения и доступ" description="Сначала создайте карточку ученика, родителя или педагога. Затем пригласите пользователя в его кабинет." /><div className="stack"><Notice tone="error">{error}</Notice>{error && <Button variant="secondary" onClick={() => setVersion(v => v + 1)}>Повторить загрузку</Button>}<LinkResult key={link?.url} value={link} />
    <Card><h2>Пригласить пользователя</h2><form onSubmit={invite}><Select label="Роль приглашённого" value={role} onChange={event => { setRole(event.target.value); setLink(null); }} disabled={busy}><option value="parent">Родитель</option><option value="student">Ученик</option><option value="teacher">Педагог</option></Select><Select label="Карточка приглашённого" value={profile} onChange={event => setProfile(event.target.value)} required disabled={busy || loading}><option value="">Выберите карточку</option>{profiles.map(p => <option key={p.id} value={p.id}>{p.name} · №{p.id}</option>)}</Select><Input key={profile} label="Email для входа" name="email" type="email" autoComplete="off" required maxLength={254} defaultValue={profiles.find(p => p.id === Number(profile))?.email ?? ''} disabled={busy || loading} /><p className="muted">Новый аккаунт будет связан с выбранной карточкой. Для уже существующего аккаунта используйте восстановление доступа.</p><Button type="submit" loading={busy} disabled={loading || !profile}>Создать приглашение</Button></form></Card>
    <Card><h2>Восстановить доступ</h2><p>Проверьте личность пользователя перед передачей ссылки. Выпуск новой ссылки отменяет предыдущую.</p><form onSubmit={recover}><Select label="Учётная запись" value={recoveryID} onChange={event => setRecoveryID(event.target.value)} required disabled={busy || loading}><option value="">Выберите пользователя</option>{users.filter(u => !u.blocked).map(u => <option key={u.id} value={u.id}>{u.name} · {u.email}</option>)}</Select><Button type="submit" loading={busy} disabled={loading || !recoveryID}>Создать ссылку восстановления</Button></form></Card>
    <Card><div className="table-heading"><h2>Запросы восстановления</h2><Button variant="secondary" onClick={() => setVersion(v => v + 1)} disabled={loading || busy}>Обновить запросы</Button></div>{loading ? <p role="status">Загружаем запросы…</p> : requests.length ? requests.map(item => <div key={item.user_id} className="pilot-list-row"><p>{item.name} · {item.email}</p><Button variant="secondary" onClick={() => { setRecoveryID(String(item.user_id)); setLink(null); }}>Выбрать пользователя</Button></div>) : <p>Новых запросов нет.</p>}</Card></div></>;
}
export function PublicAccess({ token, recovery }: { token?: string; recovery?: boolean }) {
  const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [notice, setNotice] = useState(''); const [done, setDone] = useState(false);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const data = new FormData(event.currentTarget); setError('');
    if (!recovery) { const invalid = passwordError(String(data.get('password'))); if (invalid || data.get('password') !== data.get('confirmation')) { setError(invalid || 'Пароли не совпадают.'); return; } }
    setBusy(true);
    try {
      if (recovery) { const result = await request<{ message: string }>('/auth/recovery-request', 'POST', { email: data.get('email') }); setNotice(result.message); }
      else { await request('/auth/accept-link', 'POST', { token, password: data.get('password') }); setNotice('Пароль установлен. Войдите с вашим email и новым паролем.'); window.dispatchEvent(new Event('aiq-session-ended')); }
      setDone(true);
    } catch (err) { setError(message(err)); } finally { setBusy(false); }
  }
  return <main className="boot"><Brand /><Card className="login-card"><h1>{recovery ? 'Восстановление доступа' : 'Установка пароля'}</h1><Notice tone="error">{error}</Notice><Notice tone="success">{notice}</Notice>{!done && <form onSubmit={submit}>{recovery ? <><p>Введите email учётной записи, затем свяжитесь с администратором для получения одноразовой ссылки.</p><Input label="Email" type="email" name="email" autoComplete="username" maxLength={254} required disabled={busy} /></> : <><p>Задайте пароль для входа в AIQ. Ссылка работает один раз.</p><PasswordInput label="Новый пароль" name="password" autoComplete="new-password" minLength={12} maxLength={72} required disabled={busy} /><PasswordInput label="Повторите пароль" name="confirmation" autoComplete="new-password" minLength={12} maxLength={72} required disabled={busy} /></>}<Button type="submit" loading={busy}>{recovery ? 'Запросить восстановление' : 'Установить пароль'}</Button></form>}<p><a href="#/login">Перейти ко входу</a></p></Card></main>;
}
