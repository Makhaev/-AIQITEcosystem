import { useContext, useEffect, useState } from 'react';
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
import { Features } from './features';
import { Invitations, PublicAccess } from './screens/Access';
import { Learning } from './screens/Learning';
import { TeacherWorkspace, TeacherGroups, TeacherStudents } from './screens/TeacherWorkspace';
import { CuratorWorkspace, CuratorStudents, CuratorStudent, CuratorGroups } from './screens/CuratorWorkspace';
import { CuratorReportDetail, CuratorReportsPage } from './screens/CuratorReports';
import { Curators } from './screens/Curators';
import { HomeworkTests } from './screens/HomeworkTests';
import { StudentRating } from './screens/StudentRating';
import { MethodistWorkspace, MethodistStudents, MethodistStudent, MethodistReports, MethodistPrograms, MethodistImplementationNav } from './screens/MethodistWorkspace';
import './style.css';

function Workspace({ user, onLogout }: { user: User; onLogout: (notice?: string) => void }) {
  const [route, setRoute] = useState(window.location.hash); const [error, setError] = useState(''); const [busy, setBusy] = useState(false);
  const admin = user.roles.includes('admin'); const leader = user.roles.includes('leader'); const academic = admin || leader;
  const features = useContext(Features);
  const staff = user.roles.some(role => ['admin', 'leader', 'teacher', 'curator'].includes(role));
  const family = user.roles.some(role => ['parent', 'student'].includes(role));
  const teacher = user.roles.includes('teacher');
  const curator = user.roles.includes('curator');
  const methodist = user.roles.includes('methodist');
  const methodistShell = methodist && !academic && !teacher && !curator;
  const curatorShell = curator && !academic && !teacher && !methodist;
  let items: NavigationItem[] = user.must_change_password ? [] : [
    ...(methodist ? [{href:'#/methodology',label:'Обзор'},{href:'#/methodist-programs',label:'Реализация программ'}] : []),
    ...(teacher ? [{ href: '#/teaching', label: 'Кабинет педагога' }, { href: '#/my-groups', label: 'Мои группы' }, { href: '#/my-students', label: 'Мои ученики' }] : []),
    ...(curator ? [{ href: '#/curation', label: 'Кабинет куратора' }, { href: '#/curator-groups', label: 'Группы куратора' }, { href: '#/curator-students', label: 'Ученики моих групп' }] : []),
    ...(academic && features.dashboard ? [{ href: '#/dashboard', label: 'Обзор' }] : []),
    ...(admin ? [{ href: '#/users', label: 'Пользователи' }] : []),
    ...(family ? [{ href: '#/portal', label: 'Семейный кабинет' }] : []),
    ...(academic || teacher ? [{ href: '#/teachers', label: academic ? 'Педагоги' : 'Мой профиль' }] : []),
    ...(curator || academic ? [{ href: '#/curator-reports', label: 'Кураторы' }] : []),
    ...(academic || user.roles.includes('curator') ? [{ href: '#/students', label: 'Ученики' }] : []),
    ...(academic || user.roles.includes('curator') ? [{ href: '#/parents', label: 'Родители' }] : []),
    ...(academic || user.roles.includes('curator') ? [{ href: '#/groups', label: 'Группы' }] : []),
    ...(methodist && !staff ? [{href:'#/programs',label:'Программы'}] : []),
    ...(staff ? [{ href: '#/programs', label: 'Программы' }, { href: '#/lessons', label: teacher && !academic ? 'Моё расписание' : 'Расписание' }] : []),
    ...(staff || user.roles.includes('student') ? [{ href: '#/assignments', label: 'Задания' }] : []),
    ...(!methodist || staff || family ? [{ href: '#/reports', label: 'Отчёты' }] : []),
    ...(staff && features.tasks ? [{ href: '#/tasks', label: 'Задачи' }] : []),
    ...(admin && features.audit ? [{ href: '#/audit', label: 'История действий' }] : []),
  ];
  if (curatorShell && !user.must_change_password) items = [
    { href: '#/curation', label: 'Обзор' },
    { href: '#/curator-students', label: 'Мои ученики' },
    { href: '#/curator-groups', label: 'Мои группы' },
    { href: '#/lessons', label: 'Расписание' },
    { href: '#/curator-reports', label: 'Отчёты родителям' },
    ...(family ? [{ href: '#/portal', label: 'Семейный кабинет' }] : []),
  ];
  if (methodistShell && !user.must_change_password) items = [{ href: '#/methodology', label: 'Обзор' }, { href: '#/programs', label: 'Программы' }, { href: '#/methodist-programs', label: 'Реализация программ' }, ...(family ? [{ href: '#/portal', label: 'Семейный кабинет' }] : [])];
  if (!user.must_change_password && (academic || curator || methodist || user.roles.includes('student'))) items.push({ href: '#/homework-tests', label: academic ? 'Рейтинг учеников' : 'Тесты и ДЗ' });
  if (!curatorShell && !methodistShell) items.push({ href: '#/account', label: 'Мой аккаунт' });
  if (admin && !user.must_change_password) items.push({ href: '#/invitations', label: 'Приглашения и доступ', placement: 'bottom' });
  const defaultRoute = admin ? '#/users' : methodist ? '#/methodology' : curator ? '#/curation' : teacher ? '#/teaching' : family ? '#/portal' : staff ? '#/lessons' : '#/account';
  const current = route || defaultRoute; const [kind, id] = current.split('?')[0].replace(/^#\//, '').split('/'); const allowed = (!user.must_change_password && methodist && ['methodist-students','methodist-teacher-reports','methodist-curator-reports','profile','account'].includes(kind)) || items.some(item => item.href === `#/${kind}`) || (!user.must_change_password && curatorShell && (kind === 'account' || kind === 'profile' || (['groups','students','parents','programs'].includes(kind) && !!id && /^\d+$/.test(id)))) || (!user.must_change_password && teacher && (!id || /^\d+$/.test(id)) && (kind === 'groups' || kind === 'students')) || (!user.must_change_password && (curator || academic || user.roles.includes('parent')) && kind === 'family-report' && !!id && /^\d+$/.test(id));
  useEffect(() => { const change = () => setRoute(window.location.hash); window.addEventListener('hashchange', change); return () => window.removeEventListener('hashchange', change); }, []);
  useEffect(() => { const previous = document.activeElement; const frame = requestAnimationFrame(() => { if (document.activeElement === previous) document.querySelector<HTMLElement>('[data-page-title]')?.focus({ preventScroll: true }); }); window.scrollTo(0, 0); return () => cancelAnimationFrame(frame); }, [current]);
  async function logout() { setBusy(true); setError(''); try { await request('/auth/logout', 'POST'); onLogout(); } catch (err) { setError(message(err)); } finally { setBusy(false); } }
  let screen;
  if (user.must_change_password) screen = <><PageHeading title="Безопасность аккаунта" description="Перед началом работы смените временный пароль." /><PasswordForm forced onChanged={() => onLogout('Пароль изменён. Войдите с новым паролем.')} /></>;
  else if (!allowed) screen = <><PageHeading title="Раздел недоступен" /><Card><p>Раздел не найден или у вашей учётной записи нет доступа к нему.</p><a href={defaultRoute}>Перейти в рабочее пространство</a></Card></>;
  else if (kind === 'users') screen = <Users currentUser={user} sessionEnded={() => onLogout('Доступ изменён. Войдите заново.')} />;
  else if (kind === 'teachers') screen = <Teachers user={user} />;
  else if (kind === 'homework-tests') screen = academic ? <StudentRating /> : <HomeworkTests key={current} user={user} />;
  else if (kind === 'methodology') screen = <MethodistWorkspace />;
  else if (kind === 'methodist-programs') screen = <MethodistPrograms />;
  else if (kind === 'methodist-students' && !id) screen = <MethodistStudents />;
  else if (kind === 'methodist-students' && id && /^\d+$/.test(id)) screen = <MethodistStudent key={current} id={id} />;
  else if (kind === 'methodist-teacher-reports' || kind === 'methodist-curator-reports') screen = <MethodistReports key={current} curator={kind === 'methodist-curator-reports'} />;
  else if (kind === 'teaching') screen = <TeacherWorkspace />;
  else if (kind === 'curation') screen = <CuratorWorkspace />;
  else if (kind === 'curator-groups' && !id) screen = <CuratorGroups />;
  else if (kind === 'curator-groups' && id && /^\d+$/.test(id)) screen = <CuratorStudents key={current} groupID={id} />;
  else if (kind === 'curator-students' && !id) screen = <CuratorStudents key={current} />;
  else if (kind === 'curator-students' && id && /^\d+$/.test(id)) screen = <CuratorStudent key={current} id={id} user={user} />;
  else if (kind === 'curator-reports') screen = academic ? <Curators key={current} id={id} /> : <CuratorReportsPage />;
  else if (kind === 'family-report' && id && /^\d+$/.test(id)) screen = <CuratorReportDetail key={current} id={id} user={user} />;
  else if (kind === 'my-groups' && !id) screen = <TeacherGroups />;
  else if ((kind === 'my-students' && !id) || (kind === 'my-groups' && id && /^\d+$/.test(id))) screen = <TeacherStudents key={current} groupID={id} />;
  else if (kind === 'invitations') screen = <Invitations />;
  else if (kind === 'portal' || kind === 'assignments' || kind === 'reports') screen = <Learning key={current} user={user} mode={kind} />;
  else if (kind === 'programs' && !id) screen = <Programs user={user} />;
  else if (kind === 'lessons' && !id) screen = <Lessons user={user} />;
  else if (kind === 'programs' && id && /^\d+$/.test(id)) screen = <ProgramDetails key={current} id={id} user={user} />;
  else if ((kind === 'students' || kind === 'parents') && !id) screen = <Families key={kind} kind={kind} user={user} />;
  else if ((kind === 'students' || kind === 'parents') && id && /^\d+$/.test(id)) screen = <FamilyDetails key={current} kind={kind} id={id} user={user} />;
  else if (kind === 'profile') screen = <><PageHeading title="Мой профиль" description="Данные вашей учётной записи." /><Card><dl className="detail-list"><div><dt>Имя</dt><dd>{user.name}</dd></div><div><dt>Email</dt><dd>{user.email}</dd></div><div><dt>Роль</dt><dd>{user.roles.map(role => roles[role]).join(', ')}</dd></div></dl><a href="#/account">Настройки аккаунта</a></Card></>;
  else if (kind === 'account') screen = <><PageHeading title="Мой аккаунт" description={`${user.email} · ${user.roles.map(role => roles[role]).join(', ')}`} /><PasswordForm forced={false} onChanged={() => onLogout('Пароль изменён. Войдите с новым паролем.')} /></>;
  else if (kind === 'dashboard') screen = <Dashboard />;
  else if (kind === 'audit') screen = <AuditLog />;
  else if (id && /^\d+$/.test(id) && kind === 'lessons') screen = <LessonDetail key={current} id={id} user={user} />;
  else if (id && /^\d+$/.test(id) && kind === 'groups') screen = <GroupDetails key={current} id={id} user={user} />;
  else if (sections[kind] && !id) screen = <EntityScreen key={kind} kind={kind} user={user} />;
  else screen = <PageHeading title="Страница не найдена" />;
  const methodistSection = !user.must_change_password && methodist && ['methodist-programs','methodist-students','methodist-teacher-reports','methodist-curator-reports'].includes(kind);
  return <Layout methodistMode={methodistShell && !user.must_change_password} pageTitle={methodistShell ? (methodistSection ? 'Реализация программ' : items.find(item => item.href === `#/${kind}`)?.label || 'Кабинет методиста') : undefined} curatorMode={curatorShell && !user.must_change_password} user={user} items={items} current={user.must_change_password ? '#/account' : methodistSection ? '#/methodist-programs' : `#/${kind}`} onLogout={() => void logout()} busy={busy}><Notice tone="error">{error}</Notice>{methodistSection && <MethodistImplementationNav current={kind} />}{screen}</Layout>;
}

function App() {
  const [user, setUser] = useState<User | null>(null); const [loading, setLoading] = useState(true); const [error, setError] = useState(''); const [notice, setNotice] = useState('');
  const [features, setFeatures] = useState<Record<string, boolean>>({});
  const [publicRoute, setPublicRoute] = useState(window.location.hash);
  const logout = (notice = '') => { setUser(null); setNotice(notice); };
  async function init() { setLoading(true); setError(''); try { setFeatures(await request<Record<string, boolean>>('/features')); await getCSRF(); setUser(await request<User>('/auth/me')); } catch (err) { if (err instanceof APIError && err.status === 401) setNotice(''); else setError(message(err)); } finally { setLoading(false); } }
  useEffect(() => { const change = () => setPublicRoute(window.location.hash); window.addEventListener('hashchange', change); return () => window.removeEventListener('hashchange', change); }, []);
  useEffect(() => { void init(); const end = () => logout('Сессия завершена. Войдите заново.'); window.addEventListener('aiq-session-ended', end); return () => window.removeEventListener('aiq-session-ended', end); }, []);
  useEffect(() => { if (!user) return; const controller = new AbortController(); const timer = window.setInterval(() => { void request<User>('/auth/me', 'GET', undefined, controller.signal).then(value => { if (!controller.signal.aborted) setUser(value); }).catch(() => {}); }, 60_000); return () => { controller.abort(); window.clearInterval(timer); }; }, [user?.id]);
  if (loading) return <main className="boot"><Brand /><p role="status">Открываем пространство…</p></main>;
  if (error) return <main className="boot"><Brand /><Notice tone="error">{error}</Notice><Button onClick={() => void init()}>Повторить</Button></main>;
  if (publicRoute.startsWith('#/access/')) return <PublicAccess key={publicRoute} token={publicRoute.slice('#/access/'.length)} />;
  if (!user && publicRoute === '#/recover') return <PublicAccess recovery />;
  return <Features.Provider value={features}>{user ? <Workspace user={user} onLogout={logout} /> : <Login initialNotice={notice} onLogin={value => { setUser(value); setNotice(''); window.location.hash = /^#\/family-report\/[1-9]\d*$/.test(window.location.hash) ? window.location.hash : value.roles.includes('admin') ? '/users' : value.roles.includes('methodist') ? '/methodology' : value.roles.includes('curator') ? '/curation' : value.roles.includes('teacher') ? '/teaching' : value.roles.some(r => r === 'parent' || r === 'student') ? '/portal' : '/lessons'; }} />}</Features.Provider>;
}
createRoot(document.getElementById('root')!).render(<App />);
