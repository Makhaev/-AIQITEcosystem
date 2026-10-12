import { useId, useState, type FormEvent } from 'react';
import { APIError, message, request, type User } from '../api';
import { Brand } from '../components/Layout';
import { Button, Card, Input, Notice } from '../components/ui';

export function passwordError(password: string) {
  return [...password].length < 12 || new TextEncoder().encode(password).length > 72 ? 'Пароль должен содержать не менее 12 символов и не более 72 байт UTF-8.' : '';
}
export function Login({ onLogin, initialNotice }: { onLogin: (user: User) => void; initialNotice: string }) {
  const [error, setError] = useState(''); const [busy, setBusy] = useState(false); const [show, setShow] = useState(false); const passwordID = useId();
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const data = new FormData(event.currentTarget); setError(''); setBusy(true);
    try { const result = await request<{ user: User }>('/auth/login', 'POST', { email: data.get('email'), password: data.get('password') }); onLogin(result.user); }
    catch (err) { setError(message(err)); } finally { setBusy(false); }
  }
  return <main className="login-page"><section className="login-intro"><Brand /><h1>Люди и обучение.<br />В одном пространстве.</h1><p className="muted">Ученики, команда и образовательный процесс AIQ — с вниманием к каждому.</p></section><Card className="login-card"><h2>Вход в AIQ Core</h2><p className="muted">Используйте учётную запись вашей команды.</p><Notice>{initialNotice}</Notice><Notice tone="error">{error}</Notice><form onSubmit={submit} aria-label="Вход в систему"><Input label="Email" name="email" type="email" autoComplete="username" placeholder="name@aiq.ru" required maxLength={254} disabled={busy} /><Input id={passwordID} label="Пароль" name="password" type={show ? 'text' : 'password'} autoComplete="current-password" required maxLength={72} disabled={busy} /><div className="password-actions"><Button variant="ghost" onClick={() => setShow(!show)} aria-controls={passwordID} aria-pressed={show} disabled={busy}>{show ? 'Скрыть пароль' : 'Показать пароль'}</Button></div><Button type="submit" loading={busy}>{busy ? 'Входим…' : 'Войти'}</Button></form><p className="muted login-help">Нет доступа или забыли пароль? Обратитесь к администратору AIQ.</p></Card></main>;
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
  return <Card className="password-card"><h2>{forced ? 'Смените временный пароль' : 'Смена пароля'}</h2><p className="muted">После сохранения все ваши сессии завершатся. Войдите заново с новым паролем.</p><Notice tone="error">{error}</Notice><form onSubmit={submit} aria-label="Смена пароля"><Input label={forced ? 'Временный пароль' : 'Текущий пароль'} name="old_password" type="password" autoComplete="current-password" required maxLength={72} disabled={busy} error={fields.old_password} /><Input label="Новый пароль" name="password" type="password" autoComplete="new-password" required minLength={12} maxLength={72} disabled={busy} hint="Не менее 12 символов. Не более 72 байт UTF-8." error={fields.password} /><Input label="Повторите новый пароль" name="confirmation" type="password" autoComplete="new-password" required minLength={12} maxLength={72} disabled={busy} error={fields.confirmation} /><Button type="submit" loading={busy}>{busy ? 'Сохраняем…' : 'Сохранить пароль'}</Button></form></Card>;
}
