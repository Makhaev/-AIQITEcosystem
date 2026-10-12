import { useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import '@fontsource/noto-sans/latin-400.css';
import '@fontsource/noto-sans/latin-600.css';
import '@fontsource/noto-sans/cyrillic-400.css';
import '@fontsource/noto-sans/cyrillic-600.css';
import { getCSRF, message, request, roles, type User, APIError } from './api';
import { Brand, Layout, type NavigationItem } from './components/Layout';
import { Button, Card, Notice, PageHeading } from './components/ui';
import { Login, PasswordForm } from './screens/Auth';
import { Users } from './screens/Users';
import { Teachers } from './screens/Teachers';
import { Families, FamilyDetails } from './screens/Families';
import { Programs, ProgramDetails } from './screens/Programs';
import { GroupDetails } from './screens/Groups';
import { Lessons, LessonDetail } from './screens/Lessons';
import { AuditLog, Dashboard, EntityScreen, sections } from './screens/Education';
import './style.css';

function Workspace({ user, onLogout }: { user: User; onLogout: (notice?: string) => void }) {
  const [route, setRoute] = useState(window.location.hash); const [error, setError] = useState(''); const [busy, setBusy] = useState(false);
  const admin = user.roles.includes('admin'); const leader = user.roles.includes('leader'); const academic = admin || leader;
  const items: NavigationItem[] = user.must_change_password ? [] : [
    ...(academic ? [{ href: '#/dashboard', label: 'Обзор' }] : []),
    ...(admin ? [{ href: '#/users', label: 'Пользователи' }] : []),
    ...(academic || user.roles.includes('teacher') ? [{ href: '#/teachers', label: 'Педагоги' }] : []),
    { href: '#/students', label: 'Ученики' },
    ...(academic || user.roles.includes('curator') ? [{ href: '#/parents', label: 'Родители' }] : []),
    { href: '#/groups', label: 'Группы' }, { href: '#/programs', label: 'Программы' }, { href: '#/lessons', label: 'Расписание' }, { href: '#/tasks', label: 'Задачи' },
    ...(admin ? [{ href: '#/audit', label: 'История действий' }] : []),
  ];
  items.push({ href: '#/account', label: 'Мой аккаунт' });
  const defaultRoute = admin ? '#/users' : leader ? '#/dashboard' : '#/account';
  const current = route || defaultRoute; const [kind, id] = current.replace(/^#\//, '').split('/'); const allowed = items.some(item => item.href === `#/${kind}`);
  useEffect(() => { const change = () => setRoute(window.location.hash); window.addEventListener('hashchange', change); return () => window.removeEventListener('hashchange', change); }, []);
  useEffect(() => { const frame = requestAnimationFrame(() => document.querySelector<HTMLElement>('[data-page-title]')?.focus({ preventScroll: true })); window.scrollTo(0, 0); return () => cancelAnimationFrame(frame); }, [current]);
  async function logout() { setBusy(true); setError(''); try { await request('/auth/logout', 'POST'); onLogout(); } catch (err) { setError(message(err)); } finally { setBusy(false); } }
  let screen;
  if (user.must_change_password) screen = <><PageHeading title="Безопасность аккаунта" description="Перед началом работы смените временный пароль." /><PasswordForm forced onChanged={() => onLogout('Пароль изменён. Войдите с новым паролем.')} /></>;
  else if (!allowed) screen = <><PageHeading title="Раздел недоступен" /><Card><p>Раздел не найден или у вашей учётной записи нет доступа к нему.</p><a href={defaultRoute}>Перейти в рабочее пространство</a></Card></>;
  else if (kind === 'users') screen = <Users currentUser={user} sessionEnded={() => onLogout('Доступ изменён. Войдите заново.')} />;
  else if (kind === 'teachers') screen = <Teachers user={user} />;
  else if (kind === 'programs' && !id) screen = <Programs user={user} />;
  else if (kind === 'lessons' && !id) screen = <Lessons user={user} />;
  else if (kind === 'programs' && id && /^\d+$/.test(id)) screen = <ProgramDetails key={current} id={id} user={user} />;
  else if ((kind === 'students' || kind === 'parents') && !id) screen = <Families key={kind} kind={kind} user={user} />;
  else if ((kind === 'students' || kind === 'parents') && id && /^\d+$/.test(id)) screen = <FamilyDetails key={current} kind={kind} id={id} user={user} />;
  else if (kind === 'account') screen = <><PageHeading title="Мой аккаунт" description={`${user.email} · ${user.roles.map(role => roles[role]).join(', ')}`} /><PasswordForm forced={false} onChanged={() => onLogout('Пароль изменён. Войдите с новым паролем.')} /></>;
  else if (kind === 'dashboard') screen = <Dashboard />;
  else if (kind === 'audit') screen = <AuditLog />;
  else if (id && /^\d+$/.test(id) && kind === 'lessons') screen = <LessonDetail key={current} id={id} user={user} />;
  else if (id && /^\d+$/.test(id) && kind === 'groups') screen = <GroupDetails key={current} id={id} user={user} />;
  else if (sections[kind] && !id) screen = <EntityScreen key={kind} kind={kind} user={user} />;
  else screen = <PageHeading title="Страница не найдена" />;
  return <Layout user={user} items={items} current={user.must_change_password ? '#/account' : `#/${kind}`} onLogout={() => void logout()} busy={busy}><Notice tone="error">{error}</Notice>{screen}</Layout>;
}

function App() {
  const [user, setUser] = useState<User | null>(null); const [loading, setLoading] = useState(true); const [error, setError] = useState(''); const [notice, setNotice] = useState('');
  const logout = (notice = '') => { setUser(null); setNotice(notice); };
  async function init() { setLoading(true); setError(''); try { await getCSRF(); setUser(await request<User>('/auth/me')); } catch (err) { if (err instanceof APIError && err.status === 401) setNotice(''); else setError(message(err)); } finally { setLoading(false); } }
  useEffect(() => { void init(); const end = () => logout('Сессия завершена. Войдите заново.'); window.addEventListener('aiq-session-ended', end); return () => window.removeEventListener('aiq-session-ended', end); }, []);
  useEffect(() => { if (!user) return; const controller = new AbortController(); const timer = window.setInterval(() => { void request<User>('/auth/me', 'GET', undefined, controller.signal).then(value => { if (!controller.signal.aborted) setUser(value); }).catch(() => {}); }, 60_000); return () => { controller.abort(); window.clearInterval(timer); }; }, [user?.id]);
  if (loading) return <main className="boot"><Brand /><p role="status">Открываем пространство…</p></main>;
  if (error) return <main className="boot"><Brand /><Notice tone="error">{error}</Notice><Button onClick={() => void init()}>Повторить</Button></main>;
  return user ? <Workspace user={user} onLogout={logout} /> : <Login initialNotice={notice} onLogin={value => { setUser(value); setNotice(''); window.location.hash = value.roles.includes('admin') ? '/users' : '/account'; }} />;
}
createRoot(document.getElementById('root')!).render(<App />);
