import { useEffect, useState, type ReactNode } from 'react';
import { CuratorNotifications } from './CuratorNotifications';
import { MethodistNotifications } from './MethodistNotifications';
import type { User } from '../api';
import { Button, Modal } from './ui';

export type NavigationItem = { href: string; label: string; placement?: 'bottom' };
export function Brand() {
  return <div className="brand"><img src="/brand/aiq-logo.png" alt="AIQ" width={120} height={137} /><p>Рабочее пространство</p></div>;
}
function Navigation({ items, current, onNavigate }: { items: NavigationItem[]; current: string; onNavigate?: () => void }) {
  const link = (item: NavigationItem) => <a key={item.href} href={item.href} aria-current={current === item.href ? 'page' : undefined} onClick={onNavigate}>{item.label}</a>;
  const bottom = items.filter(item => item.placement === 'bottom');
  const main = <nav className="navigation" aria-label="Основная навигация">{items.filter(item => item.placement !== 'bottom').map(link)}</nav>;
  return bottom.length ? <div className="navigation-sections">{main}<nav className="navigation navigation--bottom" aria-label="Управление доступом">{bottom.map(link)}</nav></div> : main;
}
export function Layout({ user, items, current, onLogout, busy, children, curatorMode = false, methodistMode = false, pageTitle }: { pageTitle?: string; methodistMode?: boolean; curatorMode?: boolean; user: User; items: NavigationItem[]; current: string; onLogout: () => void; busy: boolean; children: ReactNode }) {
  const [menuOpen, setMenuOpen] = useState(false);
  useEffect(() => { const media = window.matchMedia('(min-width: 1024px)'); const close = () => { if (media.matches) setMenuOpen(false); }; media.addEventListener('change', close); return () => media.removeEventListener('change', close); }, []);
  const footer = <div className="curator-profile"><a href="#/profile" onClick={() => setMenuOpen(false)}><strong>{user.name}</strong><span>Мой профиль</span></a><a href="#/account" onClick={() => setMenuOpen(false)}>Настройки аккаунта</a><Button variant="secondary" onClick={onLogout} loading={busy}>Выйти</Button></div>;
  return <div className={`layout${curatorMode || methodistMode ? ' layout--curator' : ''}`}><a className="skip-link" href="#main-content" onClick={event => { event.preventDefault(); document.getElementById('main-content')?.focus(); }}>Перейти к содержимому</a><aside className="sidebar"><Brand /><Navigation items={items} current={current} />{curatorMode || methodistMode ? footer : <p className="sidebar-footer">AIQ Core</p>}</aside><div className="layout-main"><header className="app-header"><div className="header-product"><Button variant="secondary" className="menu-toggle" onClick={() => setMenuOpen(true)} aria-haspopup="dialog">Меню</Button><span className="product-name">{pageTitle || 'AIQ Core'}</span></div><div className="header-account"><span className="account-name">{methodistMode ? <a href="#/profile">{user.name}</a> : user.name}</span>{methodistMode ? <MethodistNotifications /> : curatorMode ? <CuratorNotifications /> : <Button variant="secondary" onClick={onLogout} loading={busy}>{busy ? 'Выходим…' : 'Выйти'}</Button>}</div></header><main id="main-content" className="content" tabIndex={-1}>{children}</main></div>{menuOpen && <Modal className={curatorMode || methodistMode ? "navigation-drawer" : undefined} title="Разделы AIQ" onClose={() => setMenuOpen(false)}><Navigation items={items} current={current} onNavigate={() => setMenuOpen(false)} />{(curatorMode || methodistMode) && footer}</Modal>}</div>;
}
