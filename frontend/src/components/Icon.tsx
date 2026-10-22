import type { CSSProperties } from 'react';

const paths = {
  overview: 'M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z',
  users: 'M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2 M16 3a4 4 0 0 1 0 8 M22 21v-2a4 4 0 0 0-3-3.87 M13 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z',
  students: 'm2 9 10-5 10 5-10 5-10-5Z M6 11v6c3 3 9 3 12 0v-6 M22 9v7',
  heart: 'M20.8 4.6a5.5 5.5 0 0 0-7.8 0L12 5.7l-1.1-1.1a5.5 5.5 0 0 0-7.8 7.8L12 21l8.8-8.6a5.5 5.5 0 0 0 0-7.8Z',
  groups: 'M3 3h7v7H3z M14 3h7v7h-7z M8.5 14h7v7h-7z M6.5 10v2h11v-2 M12 12v2',
  book: 'M12 5v16 M3 3h5a4 4 0 0 1 4 2 4 4 0 0 1 4-2h5v16h-5a4 4 0 0 0-4 2 4 4 0 0 0-4-2H3Z',
  calendar: 'M8 2v4 M16 2v4 M3 10h18 M5 4h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2Z M7 14h2 M15 14h2 M7 18h2',
  tasks: 'M9 5h11 M9 12h11 M9 19h11 M3 5l1 1 2-2 M3 12l1 1 2-2 M3 19l1 1 2-2',
  history: 'M3 11a9 9 0 1 1 2.8 7 M3 4v7h7 M12 7v5l3 2',
  account: 'M20 21a8 8 0 0 0-16 0 M16 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z',
  arrow: 'M5 12h14 M13 6l6 6-6 6',
  chevron: 'm9 5 7 7-7 7',
  menu: 'M4 6h16 M4 12h16 M4 18h16',
  logout: 'M9 4H4v16h5 M10 12h11 M17 8l4 4-4 4',
  shield: 'm12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6l8-3Z m-4 9 3 3 5-6',
  chart: 'M4 3v17h17 M8 15v-4 M13 15V7 M18 15v-7',
  check: 'm5 12 4 4L19 6',
  inbox: 'M4 4h16l2 12v4H2v-4L4 4Z M2 16h6l2 3h4l2-3h6',
} as const;

export type IconName = keyof typeof paths;
export function Icon({ name, className = '', style }: { name: IconName; className?: string; style?: CSSProperties }) {
  return <svg className={`icon ${className}`} style={style} width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false"><path d={paths[name]} /></svg>;
}
