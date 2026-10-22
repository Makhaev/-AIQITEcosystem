import { useEffect, useState, type ReactNode } from 'react';
import { roles, type User } from '../api';
import { Button, Modal } from './ui';
import { Icon, type IconName } from './Icon';

export type NavigationItem = { href: string; label: string };
export function Brand() {
  return <div className="brand"><img src="/brand/aiq-logo.png" alt="AIQ" width={120} height={137} /><p>Рабочее пространство</p></div>;
}
const navigationIcons: Record<string, IconName> = { dashboard: 'overview', users: 'users', teachers: 'account', students: 'students', parents: 'heart', groups: 'groups', programs: 'book', lessons: 'calendar', tasks: 'tasks', audit: 'history', account: 'account' };
const navigationGroups = [
  { label: 'Рабочий день', routes: ['dashboard', 'lessons', 'tasks'] },
  { label: 'Обучение', routes: ['students', 'parents', 'groups', 'programs', 'teachers'] },
  { label: 'Управление', routes: ['users', 'audit', 'account'] },
];
function Navigation({ items, current, onNavigate }: { items: NavigationItem[]; current: string; onNavigate?: () => void }) {
  return <nav className="navigation" aria-label="Основная навигация">{navigationGroups.map(group => {
    const visible = group.routes.flatMap(route => items.filter(item => item.href === `#/${route}`));
    return visible.length > 0 && <div className="navigation-group" key={group.label}><span className="navigation-label">{group.label}</span>{visible.map(item => <a key={item.href} href={item.href} aria-current={current === item.href ? 'page' : undefined} onClick={onNavigate}><Icon name={navigationIcons[item.href.slice(2)] ?? 'overview'} /><span>{item.label}</span>{current === item.href && <span className="navigation-indicator" aria-hidden="true" />}</a>)}</div>;
  })}</nav>;
}
export function Layout({ user, items, current, onLogout, busy, children }: { user: User; items: NavigationItem[]; current: string; onLogout: () => void; busy: boolean; children: ReactNode }) {
  const [menuOpen, setMenuOpen] = useState(false);
  useEffect(() => { const media = window.matchMedia('(min-width: 1024px)'); const close = () => { if (media.matches) setMenuOpen(false); }; media.addEventListener('change', close); return () => media.removeEventListener('change', close); }, []);
  const initials = user.name.trim().split(/\s+/).slice(0, 2).map(part => [...part][0]).join('').toLocaleUpperCase('ru-RU');
  const currentLabel = items.find(item => item.href === current)?.label ?? 'Рабочее пространство';
  return <div className="layout">
    <a className="skip-link" href="#main-content" onClick={event => { event.preventDefault(); document.getElementById('main-content')?.focus(); }}>Перейти к содержимому</a>
    <aside className="sidebar"><Brand /><Navigation items={items} current={current} /><div className="sidebar-footer"><span className="workspace-symbol" aria-hidden="true"><Icon name="book" /></span><div><strong>AIQ Core</strong><span>Пространство команды</span></div></div></aside>
    <div className="layout-main">
      <header className="app-header">
        <div className="header-product"><Button variant="ghost" className="menu-toggle" onClick={() => setMenuOpen(true)} aria-haspopup="dialog" aria-label="Меню"><Icon name="menu" /></Button><span className="product-name">Рабочее пространство</span><Icon name="chevron" className="breadcrumb-separator" /><span className="header-section">{currentLabel}</span></div>
        <div className="header-account"><span className="account-avatar" aria-hidden="true">{initials}</span><div className="account-info"><span className="account-name">{user.name}</span><span className="account-role">{user.roles.map(role => roles[role]).join(' · ')}</span></div><Button variant="ghost" className="logout-button" onClick={onLogout} loading={busy}><Icon name="logout" /><span>{busy ? 'Выходим…' : 'Выйти'}</span></Button></div>
      </header>
      <main id="main-content" className="content" tabIndex={-1}>{children}</main>
    </div>
    {menuOpen && <Modal title="Разделы AIQ" onClose={() => setMenuOpen(false)}><Navigation items={items} current={current} onNavigate={() => setMenuOpen(false)} /></Modal>}
  </div>;
}
