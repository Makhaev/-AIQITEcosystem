import { useId, type ReactNode } from 'react';
import { Badge } from './ui';

export type CardFact = { label: string; value: ReactNode };
export function EntityCard({ title, titleHref, status, tone = 'neutral', facts = [], description, action, children, compact = false, highlight = false, icon, id }: {
  title: string; titleHref?: string; status?: string; tone?: 'neutral' | 'success' | 'warning' | 'danger'; facts?: CardFact[];
  description?: string; action?: ReactNode; children?: ReactNode; compact?: boolean; highlight?: boolean; icon?: ReactNode; id?: string;
}) {
  const heading = useId();
  return <article id={id} className={`card entity-card${compact ? ' entity-card--compact' : ''}${highlight ? ' entity-card--highlight' : ''}`} aria-labelledby={heading}>
    {icon && <div className="entity-card__icon" aria-hidden="true">{icon}</div>}
    <header className="entity-card__heading"><h2 id={heading}>{titleHref ? <a href={titleHref}>{title}</a> : title}</h2>{status && <Badge tone={tone}>{status}</Badge>}</header>
    {description && <p className="entity-card__description">{description.length > 180 ? `${description.slice(0, 180)}…` : description}</p>}
    {!!facts.length && <dl className="entity-card__facts">{facts.map(fact => <div key={fact.label}><dt>{fact.label}</dt><dd>{fact.value}</dd></div>)}</dl>}
    {action && <footer className="entity-card__action">{action}</footer>}
    {children}
  </article>;
}

export function StudentSummary({ groups, curators, nextLesson, attendance, period, contact, status }: {
  groups: ReactNode; curators: ReactNode; nextLesson: ReactNode; attendance: ReactNode; period: string; contact?: ReactNode; status: ReactNode;
}) {
  return <section className="card student-summary" aria-label="Кратко об ученике"><div className="student-summary__heading"><h2>Кратко об ученике</h2>{status}</div><dl className="student-summary__facts">
    <div><dt>Группа</dt><dd>{groups}</dd></div><div><dt>Куратор</dt><dd>{curators}</dd></div>
    <div><dt>Ближайшее занятие · МСК</dt><dd>{nextLesson}</dd></div>
    <div><dt>Посещаемость · {period}</dt><dd>{attendance}</dd></div>
    {contact !== undefined && <div><dt>Контакт родителя</dt><dd>{contact}</dd></div>}
  </dl></section>;
}

export function CardTabs({ tabs, selected, onChange, prefix }: { tabs: { id: string; label: string }[]; selected: string; onChange: (id: string) => void; prefix: string }) {
  return <div className="card-tabs" role="tablist" aria-label="Разделы карточки ученика">{tabs.map((tab, index) => <button key={tab.id} type="button" role="tab" id={`${prefix}-tab-${tab.id}`} aria-selected={selected === tab.id} aria-controls={`${prefix}-panel-${tab.id}`} tabIndex={selected === tab.id ? 0 : -1} onClick={() => onChange(tab.id)} onKeyDown={event => {
    const target = event.key === 'ArrowRight' ? (index + 1) % tabs.length : event.key === 'ArrowLeft' ? (index + tabs.length - 1) % tabs.length : event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1 : -1;
    if (target < 0) return; event.preventDefault(); onChange(tabs[target].id); document.getElementById(`${prefix}-tab-${tabs[target].id}`)?.focus();
  }}>{tab.label}</button>)}</div>;
}
