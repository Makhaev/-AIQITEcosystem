import { useEffect, useState, type ReactNode } from 'react';
import type { User } from '../api';
import { Button, Modal } from './ui';

export type NavigationItem = { href: string; label: string };
export function Brand() {
  return <div className="brand"><img src="/brand/aiq-logo.png" alt="AIQ" width={120} height={137} /><p>Рабочее пространство</p></div>;
}
function Navigation({ items, current, onNavigate }: { items: NavigationItem[]; current: string; onNavigate?: () => void }) {
  return <nav className="navigation" aria-label="Основная навигация">{items.map(item => <a key={item.href} href={item.href} aria-current={current === item.href ? 'page' : undefined} onClick={onNavigate}>{item.label}</a>)}</nav>;
}
export function Layout({ user, items, current, onLogout, busy, children }: { user: User; items: NavigationItem[]; current: string; onLogout: () => void; busy: boolean; children: ReactNode }) {
  const [menuOpen, setMenuOpen] = useState(false);
  useEffect(() => { const media = window.matchMedia('(min-width: 1024px)'); const close = () => { if (media.matches) setMenuOpen(false); }; media.addEventListener('change', close); return () => media.removeEventListener('change', close); }, []);
  return <div className="layout"><a className="skip-link" href="#main-content" onClick={event => { event.preventDefault(); document.getElementById('main-content')?.focus(); }}>Перейти к содержимому</a><aside className="sidebar"><Brand /><Navigation items={items} current={current} /><p className="sidebar-footer">AIQ Core</p></aside><div className="layout-main"><header className="app-header"><div className="header-product"><Button variant="secondary" className="menu-toggle" onClick={() => setMenuOpen(true)} aria-haspopup="dialog">Меню</Button><span className="product-name">AIQ Core</span></div><div className="header-account"><span className="account-name">{user.name}</span><Button variant="secondary" onClick={onLogout} loading={busy}>{busy ? 'Выходим…' : 'Выйти'}</Button></div></header><main id="main-content" className="content" tabIndex={-1}>{children}</main></div>{menuOpen && <Modal title="Разделы AIQ" onClose={() => setMenuOpen(false)}><Navigation items={items} current={current} onNavigate={() => setMenuOpen(false)} /></Modal>}</div>;
}
