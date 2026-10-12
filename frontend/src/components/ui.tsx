import { useEffect, useId, useRef, useState, type ButtonHTMLAttributes, type HTMLAttributes, type InputHTMLAttributes, type ReactNode, type SelectHTMLAttributes } from 'react';

export function Button({ variant = 'primary', loading = false, className = '', disabled, children, type = 'button', ...props }: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: 'primary' | 'secondary' | 'ghost' | 'danger'; loading?: boolean }) {
  return <button {...props} type={type} className={`button button--${variant} ${className}`} disabled={disabled || loading} aria-busy={loading || undefined}>{children}</button>;
}
type FieldProps = { label: string; hint?: string; error?: string };
export function Input({ label, hint, error, id, className = '', ...props }: InputHTMLAttributes<HTMLInputElement> & FieldProps) {
  const generated = useId(); const inputID = id ?? generated;
  return <div className={`field ${className}`}><label htmlFor={inputID}>{label}</label><input {...props} id={inputID} className="input" aria-invalid={!!error || undefined} aria-describedby={[props['aria-describedby'], hint ? `${inputID}-hint` : '', error ? `${inputID}-error` : ''].filter(Boolean).join(' ') || undefined} />{hint && <p id={`${inputID}-hint`} className="field-hint">{hint}</p>}{error && <p id={`${inputID}-error`} className="field-error">{error}</p>}</div>;
}
export function PasswordInput({ id, label, disabled, ...props }: Omit<InputHTMLAttributes<HTMLInputElement>, 'type'> & FieldProps) {
  const generated = useId(); const inputID = id ?? generated;
  const [visible, setVisible] = useState(false);
  const action = visible ? 'Скрыть пароль' : 'Показать пароль';
  return <div className="password-field"><Input {...props} id={inputID} label={label} type={visible ? 'text' : 'password'} disabled={disabled} /><div className="password-actions"><Button variant="ghost" type="button" onClick={() => setVisible(value => !value)} aria-controls={inputID} aria-pressed={visible} aria-label={`${action}: ${label}`} disabled={disabled}>{action}</Button></div></div>;
}
export function Select({ label, hint, error, id, className = '', children, ...props }: SelectHTMLAttributes<HTMLSelectElement> & FieldProps) {
  const generated = useId(); const inputID = id ?? generated;
  return <div className={`field ${className}`}><label htmlFor={inputID}>{label}</label><select {...props} id={inputID} className="input" aria-invalid={!!error || undefined} aria-describedby={[props['aria-describedby'], hint ? `${inputID}-hint` : '', error ? `${inputID}-error` : ''].filter(Boolean).join(' ') || undefined}>{children}</select>{hint && <p id={`${inputID}-hint`} className="field-hint">{hint}</p>}{error && <p id={`${inputID}-error`} className="field-error">{error}</p>}</div>;
}
export function Card({ className = '', children, ...props }: HTMLAttributes<HTMLElement>) {
  return <section {...props} className={`card ${className}`}>{children}</section>;
}
export type Column<T> = { key: string; title: string; render: (item: T) => ReactNode };
export function Table<T>({ caption, columns, items, rowKey }: { caption: string; columns: Column<T>[]; items: T[]; rowKey: (item: T) => string | number }) {
  return <div className="table-container"><table role="table" className="table"><caption className="sr-only">{caption}</caption><thead role="rowgroup"><tr role="row">{columns.map(column => <th role="columnheader" scope="col" key={column.key}>{column.title}</th>)}</tr></thead><tbody role="rowgroup">{items.map(item => <tr role="row" key={rowKey(item)}>{columns.map(column => <td role="cell" data-label={column.title} key={column.key}><div className="table-cell">{column.render(item)}</div></td>)}</tr>)}</tbody></table></div>;
}
export function Modal({ title, description, children, onClose, busy = false }: { title: string; description?: string; children: ReactNode; onClose: () => void; busy?: boolean }) {
  const dialog = useRef<HTMLDialogElement>(null); const id = useId();
  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const element = dialog.current; const overflow = document.body.style.overflow;
    element?.showModal(); document.body.style.overflow = 'hidden';
    element?.querySelector<HTMLElement>('input:not(:disabled), select:not(:disabled), textarea:not(:disabled)')?.focus();
    return () => { element?.close(); document.body.style.overflow = overflow; if (previous?.isConnected) previous.focus(); };
  }, []);
  return <dialog ref={dialog} className="modal" aria-labelledby={`${id}-title`} aria-describedby={description ? `${id}-description` : undefined} onKeyDown={event => {
    if (event.key !== 'Tab') return;
    const controls = Array.from(event.currentTarget.querySelectorAll<HTMLElement>('a[href], button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex="0"]')).filter(control => control.getClientRects().length);
    const first = controls[0], last = controls.at(-1);
    if (!first) { event.preventDefault(); return; }
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
    if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
  }} onCancel={event => { event.preventDefault(); if (!busy) onClose(); }}><div className="modal-heading"><h2 id={`${id}-title`}>{title}</h2><Button variant="ghost" onClick={onClose} disabled={busy} aria-label="Закрыть окно" className="close-button">×</Button></div>{description && <p id={`${id}-description`} className="muted modal-description">{description}</p>}{children}</dialog>;
}
export function Badge({ children, tone = 'neutral' }: { children: ReactNode; tone?: 'neutral' | 'success' | 'warning' | 'danger' }) {
  return <span className={`badge badge--${tone}`}>{children}</span>;
}
export function Pagination({ page, total, pageSize, onChange, disabled = false }: { page: number; total: number; pageSize: number; onChange: (page: number) => void; disabled?: boolean }) {
  const pages = Math.max(1, Math.ceil(total / pageSize));
  return <nav className="pagination" aria-label="Страницы списка"><p aria-live="polite">Страница {page} из {pages}<span className="pagination-total"> · Всего: {total}</span></p><div className="button-group"><Button variant="secondary" onClick={() => onChange(page - 1)} disabled={disabled || page <= 1} aria-label="Предыдущая страница">Назад</Button><Button variant="secondary" onClick={() => onChange(page + 1)} disabled={disabled || page >= pages} aria-label="Следующая страница">Далее</Button></div></nav>;
}
export function Notice({ children, tone = 'info' }: { children?: ReactNode; tone?: 'info' | 'success' | 'warning' | 'error' }) {
  return children ? <div className={`notice notice--${tone}`} role={tone === 'error' ? 'alert' : 'status'}>{children}</div> : null;
}
export function PageHeading({ title, description, action }: { title: string; description?: string; action?: ReactNode }) {
  return <div className="page-heading"><div><h1 tabIndex={-1} data-page-title>{title}</h1>{description && <p className="muted">{description}</p>}</div>{action}</div>;
}
export function EmptyState({ title, description, action }: { title: string; description?: string; action?: ReactNode }) {
  return <div className="empty-state"><h2>{title}</h2>{description && <p className="muted">{description}</p>}{action}</div>;
}
