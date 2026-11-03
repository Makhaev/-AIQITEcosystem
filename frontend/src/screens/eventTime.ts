// Local event time is independent of the operator's computer time zone.
export function eventLocal(instant: string | null, zone: string): string {
  if (!instant) return '';
  const parts = new Intl.DateTimeFormat('en-CA', { timeZone: zone, year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' }).formatToParts(new Date(instant));
  const part = (key: string) => parts.find(p => p.type === key)?.value || '';
  return `${part('year')}-${part('month')}-${part('day')}T${part('hour')}:${part('minute')}:${part('second')}`;
}
export function eventInstant(local: string, zone: string): string | null {
  if (!local) return null;
  const normalized = local.length === 16 ? local + ':00' : local;
  const wall = new Date(normalized + 'Z').getTime();
  if (!Number.isFinite(wall)) throw new Error('Проверьте дату и время.');
  const offsets = new Set<number>();
  // Collect offsets around the date to detect both sides of DST changes.
  for (let hours = -36; hours <= 36; hours += 6) {
    const probe = wall + hours * 3600_000;
    offsets.add(new Date(eventLocal(new Date(probe).toISOString(), zone) + 'Z').getTime() - probe);
  }
  const candidates = [...offsets].map(offset => new Date(wall - offset).toISOString()).filter(value => eventLocal(value, zone) === normalized);
  if (candidates.length !== 1) throw new Error(candidates.length ? 'Это время повторяется при переводе часов. Выберите время вне повторяющегося часа.' : 'Такого местного времени нет в выбранном часовом поясе. Проверьте дату и перевод часов.');
  return candidates[0];
}
