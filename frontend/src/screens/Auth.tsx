import { useState, type FormEvent } from 'react';
import { APIError, message, request, type User } from '../api';
import { Brand } from '../components/Layout';
import { Button, Card, Input, Notice, PasswordInput } from '../components/ui';
import { Icon } from '../components/Icon';

export function passwordError(password: string) {
  return [...password].length < 12 || new TextEncoder().encode(password).length > 72 ? 'Пароль должен содержать не менее 12 символов и не более 72 байт UTF-8.' : '';
}
export function Login({ onLogin, initialNotice }: { onLogin: (user: User) => void; initialNotice: string }) {
  const [error, setError] = useState(''); const [busy, setBusy] = useState(false);
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const data = new FormData(event.currentTarget); setError(''); setBusy(true);
    try { const result = await request<{ user: User }>('/auth/login', 'POST', { email: data.get('email'), password: data.get('password') }); onLogin(result.user); }
    catch (err) { setError(message(err)); } finally { setBusy(false); }
  }
  return <main className="login-page">
    <section className="login-intro">
      <div className="login-brand-row"><Brand /><span className="login-edition">AIQ Core<br /><span>Для вашей команды</span></span></div>
      <div className="login-story"><span className="eyebrow">Образование начинается с людей</span><h1>Больше внимания<br />людям.<br /><span>Меньше рутины.</span></h1><p>Ученики, команда и образовательный процесс — в одном рабочем пространстве AIQ.</p></div>
      <div className="login-features"><div><Icon name="students" /><span>Каждый ученик в фокусе</span></div><div><Icon name="calendar" /><span>Весь учебный день под рукой</span></div><div><Icon name="chart" /><span>Решения на основе данных</span></div></div>
      <div className="login-art" aria-hidden="true"><span /><span /><span /><span /></div>
      <p className="login-mission">Развивать человека. Создавать будущее.</p>
    </section>
    <div className="login-form-area"><Card className="login-card"><span className="login-welcome"><Icon name="shield" /> Пространство AIQ</span><h2>Вход в AIQ Core</h2><p className="muted">Рады видеть вас. Войдите, чтобы продолжить работу.</p><Notice>{initialNotice}</Notice><Notice tone="error">{error}</Notice><form onSubmit={submit} aria-label="Вход в систему"><Input label="Email" name="email" type="email" autoComplete="username" placeholder="name@aiq.ru" required maxLength={254} disabled={busy} /><PasswordInput label="Пароль" name="password" autoComplete="current-password" required maxLength={72} disabled={busy} /><Button type="submit" loading={busy}>{busy ? 'Входим…' : 'Войти'}{!busy && <Icon name="arrow" />}</Button></form><p className="muted login-help">Нет доступа или забыли пароль? Обратитесь к администратору AIQ.</p></Card><p className="login-footer">AIQ Core · С вниманием к каждому</p></div>
  </main>;
}
export function PasswordForm({ forced, onChanged }: { forced: boolean; onChanged: () => void }) {
  const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [fields, setFields] = useState<Record<string, string>>({});
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const form = event.currentTarget; const data = new FormData(form); setError(''); setFields({});
    const password = String(data.get('password')); const invalid = passwordError(password);
    if (invalid || password !== data.get('confirmation')) { setFields(invalid ? { password: invalid } : { confirmation: 'Новые пароли не совпадают.' }); setError('Проверьте введённые данные.'); return; }
    setBusy(true);
    try { await request('/auth/change-password', 'POST', { old_password: data.get('old_password'), password }); onChanged(); }
    catch (err) { setError(err instanceof APIError ? err.message : message(err)); if (err instanceof APIError) setFields(err.fields ?? {}); }
    finally { setBusy(false); }
  }
  return <Card className="password-card"><h2>{forced ? 'Смените временный пароль' : 'Смена пароля'}</h2><p className="muted">После сохранения все ваши сессии завершатся. Войдите заново с новым паролем.</p><Notice tone="error">{error}</Notice><form onSubmit={submit} aria-label="Смена пароля"><PasswordInput label={forced ? 'Временный пароль' : 'Текущий пароль'} name="old_password" autoComplete="current-password" required maxLength={72} disabled={busy} error={fields.old_password} /><PasswordInput label="Новый пароль" name="password" autoComplete="new-password" required minLength={12} maxLength={72} disabled={busy} hint="Не менее 12 символов. Не более 72 байт UTF-8." error={fields.password} /><PasswordInput label="Повторите новый пароль" name="confirmation" autoComplete="new-password" required minLength={12} maxLength={72} disabled={busy} error={fields.confirmation} /><Button type="submit" loading={busy}>{busy ? 'Сохраняем…' : 'Сохранить пароль'}</Button></form></Card>;
}
