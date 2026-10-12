import { useEffect, useState, type FormEvent } from 'react';
import { APIError, message, request, type User, type Page as UserPage } from '../api';
import { Badge, Button, Card, EmptyState, Input, Modal, Notice, PageHeading, Pagination, Select, Table, type Column } from '../components/ui';

type Kind = 'students' | 'parents';
type Person = { id: number; name: string; status: 'active' | 'archived'; birth_date?: string | null; curator_id?: number | null; curator_name?: string | null; phone?: string; email?: string };
type FamilyLink = Person & { student_id: number; parent_id: number; relationship: string; is_primary: boolean };
type FamilyCard = Person & { parents?: FamilyLink[]; children?: FamilyLink[] };
type PersonPage = { items: Person[]; total: number; page: number; page_size: number };
const labels = { students: { title: 'Ученики', singular: 'ученика', create: 'Новый ученик', add: 'Добавить ученика' }, parents: { title: 'Родители', singular: 'родителя', create: 'Новый родитель', add: 'Добавить родителя' } };
function Status({ status }: { status: Person['status'] }) { return <Badge tone={status === 'active' ? 'success' : 'neutral'}>{status === 'active' ? 'Активен' : 'В архиве'}</Badge>; }
function date(value?: string | null) { return value ? value.split('-').reverse().join('.') : 'Не указана'; }

function Editor({ kind, person, close, saved }: { kind: Kind; person?: Person; close: () => void; saved: () => void }) {
  const student = kind === 'students'; const [curators, setCurators] = useState<User[]>([]);
  const [loading, setLoading] = useState(student); const [lookupError, setLookupError] = useState('');
  const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [fields, setFields] = useState<Record<string, string>>({});
  useEffect(() => {
    if (!student) return; const controller = new AbortController();
    (async () => {
      try {
        const items: User[] = []; let page = 1, total = 0;
        do { const result = await request<UserPage>(`/users?role=curator&page=${page++}&page_size=100`, 'GET', undefined, controller.signal); if (!result.items.length && items.length < result.total) throw new Error('Не удалось загрузить список кураторов.'); items.push(...result.items); total = result.total; } while (items.length < total && !controller.signal.aborted);
        if (!controller.signal.aborted) setCurators(items);
      } catch (err) { if (!controller.signal.aborted) setLookupError(message(err)); }
      finally { if (!controller.signal.aborted) setLoading(false); }
    })(); return () => controller.abort();
  }, [student]);
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const data = new FormData(event.currentTarget); setError(''); setFields({});
    const body = student ? { name: data.get('name'), status: data.get('status'), birth_date: data.get('birth_date') || null, curator_id: data.get('curator_id') ? Number(data.get('curator_id')) : null } : { name: data.get('name'), status: data.get('status'), phone: String(data.get('phone') ?? '').trim(), email: String(data.get('email') ?? '').trim() };
    if (!student && 'phone' in body && !body.phone && !body.email) { setError('Укажите хотя бы один контакт: телефон или email.'); setFields({ phone: 'Телефон или email обязателен.' }); return; }
    setBusy(true);
    try { await request(person ? `/${kind}/${person.id}` : `/${kind}`, person ? 'PATCH' : 'POST', body); saved(); }
    catch (err) { setError(message(err)); if (err instanceof APIError) setFields(err.fields ?? {}); }
    finally { setBusy(false); }
  }
  const today = new Intl.DateTimeFormat('sv-SE', { timeZone: 'Europe/Moscow' }).format(new Date());
  return <Modal title={person ? `Изменить ${labels[kind].singular}` : labels[kind].create} description="Карточка не создаёт учётную запись. Совпадающее ФИО не объединяет записи." onClose={close} busy={busy}>
    <Notice tone="error">{lookupError || error}</Notice><form onSubmit={save}>
      <Input label="Имя и фамилия" name="name" defaultValue={person?.name} required maxLength={200} disabled={busy} error={fields.name} />
      {student ? <><Input label="Дата рождения" name="birth_date" type="date" max={today} defaultValue={person?.birth_date ?? ''} disabled={busy} error={fields.birth_date} hint="Необязательно." />
        <Select label="Куратор" name="curator_id" defaultValue={person?.curator_id ?? ''} disabled={busy || loading || !!lookupError} error={fields.curator_id}>
          <option value="">{loading ? 'Загружаем кураторов…' : 'Не назначен'}</option>
          {person?.curator_id != null && !curators.some(value => value.id === person.curator_id) && <option value={person.curator_id}>{person.curator_name || 'Текущий куратор'}</option>}
          {curators.map(value => <option value={value.id} key={value.id} disabled={value.blocked && value.id !== person?.curator_id}>{value.name}{value.blocked ? ' · Заблокирован' : ''}</option>)}
        </Select></> : <><Input label="Телефон" name="phone" type="tel" placeholder="+7 900 123-45-67" defaultValue={person?.phone} maxLength={100} disabled={busy} error={fields.phone} hint="Нужен телефон или email. Можно указать оба контакта." /><Input label="Email" name="email" type="email" defaultValue={person?.email} maxLength={254} disabled={busy} error={fields.email} /></>}
      <Select label="Статус" name="status" defaultValue={person?.status ?? 'active'} disabled={busy} error={fields.status}><option value="active">Активен</option><option value="archived">В архиве</option></Select>
      <div className="modal-footer"><Button variant="secondary" onClick={close} disabled={busy}>Отмена</Button><Button type="submit" loading={busy} disabled={loading || !!lookupError}>Сохранить {labels[kind].singular}</Button></div>
    </form>
  </Modal>;
}

function LinkEditor({ studentID, link, close, saved }: { studentID: number; link?: FamilyLink; close: () => void; saved: () => void }) {
  const [options, setOptions] = useState<PersonPage | null>(null); const [search, setSearch] = useState(''); const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<Person | null>(link ?? null); const [loading, setLoading] = useState(!link); const [lookupError, setLookupError] = useState('');
  const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [fields, setFields] = useState<Record<string, string>>({});
  useEffect(() => {
    if (link) return; const controller = new AbortController(); setLoading(true); setLookupError('');
    const timer = window.setTimeout(() => {
      request<PersonPage>(`/parents?status=active&page=${page}&page_size=20&search=${encodeURIComponent(search)}`, 'GET', undefined, controller.signal).then(setOptions).catch(err => { if (!controller.signal.aborted) setLookupError(message(err)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    }, 250); return () => { clearTimeout(timer); controller.abort(); };
  }, [link, page, search]);
  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const data = new FormData(event.currentTarget); setBusy(true); setError(''); setFields({});
    const body = { relationship: data.get('relationship'), is_primary: data.get('is_primary') === 'true', ...(!link ? { parent_id: selected?.id } : {}) };
    try { await request(`/students/${studentID}/parents${link ? `/${link.parent_id}` : ''}`, link ? 'PATCH' : 'POST', body); saved(); }
    catch (err) { setError(message(err)); if (err instanceof APIError) setFields(err.fields ?? {}); } finally { setBusy(false); }
  }
  return <Modal title={link ? 'Изменить связь с родителем' : 'Связать с родителем'} description="Основной контакт может быть только один. Для смены сначала снимите этот признак у текущего контакта." onClose={close} busy={busy}>
    <Notice tone="error">{error || lookupError}</Notice><form onSubmit={save}>
      {!link ? <><Input label="Поиск родителя" value={search} onChange={event => { setSearch(event.target.value); setPage(1); }} maxLength={200} disabled={busy} hint="По ФИО, телефону или email. Новую карточку можно создать в разделе «Родители»." />
        <Select label="Родитель" value={selected?.id ?? ''} onChange={event => setSelected(options?.items.find(value => value.id === Number(event.target.value)) ?? null)} required disabled={busy || loading || !!lookupError} error={fields.parent_id}>
          <option value="">{loading ? 'Загружаем родителей…' : 'Выберите родителя'}</option>
          {selected && !options?.items.some(value => value.id === selected.id) && <option value={selected.id}>{selected.name} · {selected.phone || selected.email} · №{selected.id}</option>}
          {options?.items.map(value => <option key={value.id} value={value.id}>{value.name} · {value.phone || value.email} · №{value.id}</option>)}
        </Select>{options && <Pagination page={page} total={options.total} pageSize={20} onChange={setPage} disabled={busy || loading} />}</> : <p><strong>{link.name}</strong></p>}
      <Input label="Тип представительства" name="relationship" defaultValue={link?.relationship ?? 'Родитель'} required maxLength={100} disabled={busy} error={fields.relationship} hint="Например: мать, отец, опекун, законный представитель." />
      <Select label="Основной контакт" name="is_primary" defaultValue={String(link?.is_primary ?? false)} disabled={busy} error={fields.is_primary}><option value="false">Нет</option><option value="true">Да</option></Select>
      <div className="modal-footer"><Button variant="secondary" onClick={close} disabled={busy}>Отмена</Button><Button type="submit" loading={busy} disabled={!link && (!selected || loading || !!lookupError)}>Сохранить связь</Button></div>
    </form>
  </Modal>;
}

export function Families({ kind, user }: { kind: Kind; user: User }) {
  const admin = user.roles.includes('admin'); const [data, setData] = useState<PersonPage | null>(null); const [page, setPage] = useState(1); const [filters, setFilters] = useState(''); const [version, setVersion] = useState(0);
  const [loading, setLoading] = useState(true); const [error, setError] = useState(''); const [success, setSuccess] = useState(''); const [editor, setEditor] = useState<Person | 'new' | null>(null);
  const [archive, setArchive] = useState<Person | null>(null); const [busy, setBusy] = useState(false); const [archiveError, setArchiveError] = useState('');
  useEffect(() => { const controller = new AbortController(); setLoading(true); setError(''); request<PersonPage>(`/${kind}?page=${page}&page_size=20${filters}`, 'GET', undefined, controller.signal).then(setData).catch(err => { if (!controller.signal.aborted) setError(message(err)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); }); return () => controller.abort(); }, [kind, page, filters, version]);
  function apply(event: FormEvent<HTMLFormElement>) { event.preventDefault(); const data = new FormData(event.currentTarget); const params = new URLSearchParams(); for (const key of ['search', 'status']) { const value = String(data.get(key) ?? '').trim(); if (value) params.set(key, value); } setFilters(params.size ? `&${params}` : ''); setPage(1); }
  async function archiveCard() { if (!archive) return; setBusy(true); setArchiveError(''); try { await request(`/${kind}/${archive.id}`, 'PATCH', { status: 'archived' }); setArchive(null); setSuccess('Карточка перенесена в архив. Связи сохранены.'); setVersion(value => value + 1); } catch (err) { setArchiveError(message(err)); } finally { setBusy(false); } }
  const columns: Column<Person>[] = [
    { key: 'name', title: kind === 'students' ? 'Ученик' : 'Родитель', render: value => <><a href={`#/${kind}/${value.id}`}><strong>{value.name}</strong></a><span className="muted">Карточка №{value.id}</span></> },
    { key: 'details', title: kind === 'students' ? 'Дата рождения и куратор' : 'Контакты', render: value => kind === 'students' ? <><span>{date(value.birth_date)}</span><span>{value.curator_name || 'Куратор не назначен'}</span></> : <><span>{value.phone || 'Телефон не указан'}</span><span>{value.email || 'Email не указан'}</span></> },
    { key: 'status', title: 'Статус', render: value => <Status status={value.status} /> },
    ...(admin ? [{ key: 'actions', title: 'Действия', render: (value: Person) => <div className="row-actions"><Button variant="secondary" onClick={() => setEditor(value)} aria-label={`Изменить ${labels[kind].singular} ${value.name}`}>Изменить</Button>{value.status === 'active' && <Button variant="ghost" onClick={() => { setArchive(value); setArchiveError(''); }} aria-label={`Архивировать ${labels[kind].singular} ${value.name}`}>В архив</Button>}</div> }] : []),
  ];
  return <><PageHeading title={labels[kind].title} description={user.roles.includes('curator') && !user.roles.some(role => role === 'admin' || role === 'leader') ? 'Карточки закреплённых за вами учеников и их родителей.' : 'Карточки, контакты и семейные связи. Записи с одинаковым ФИО сохраняются отдельно.'} action={admin && <Button onClick={() => setEditor('new')}>{labels[kind].add}</Button>} />
    <form className="filters" onSubmit={apply}><Input label={kind === 'students' ? 'Поиск по ФИО' : 'Поиск по ФИО или контакту'} name="search" maxLength={200} /><Select label="Статус" name="status"><option value="">Все статусы</option><option value="active">Активен</option><option value="archived">В архиве</option></Select><Button type="submit" variant="secondary">Применить</Button></form>
    <div className="stack"><Notice tone="success">{success}</Notice><Notice tone="error">{error}</Notice><Card className="table-card" aria-busy={loading}><div className="table-heading"><h2>{labels[kind].title}: список</h2><span className="muted">Всего: {data?.total ?? '—'}</span></div>
      {loading ? <p className="empty-state" role="status">Загружаем данные…</p> : error ? <EmptyState title="Не удалось загрузить карточки" action={<Button variant="secondary" onClick={() => setVersion(value => value + 1)}>Повторить загрузку</Button>} /> : data?.items.length ? <Table caption={`${labels[kind].title} AIQ`} columns={columns} items={data.items} rowKey={value => value.id} /> : <EmptyState title="Карточки не найдены" description={kind === 'students' && user.roles.includes('teacher') && !user.roles.some(role => ['admin', 'leader', 'curator'].includes(role)) ? 'Состав групп станет доступен после назначения ваших занятий.' : 'Попробуйте изменить фильтры или добавить новую карточку.'} />}
      {data && <Pagination page={page} total={data.total} pageSize={20} onChange={setPage} disabled={loading} />}
    </Card></div>
    {editor && <Editor kind={kind} person={editor === 'new' ? undefined : editor} close={() => setEditor(null)} saved={() => { setEditor(null); setSuccess('Карточка сохранена.'); setVersion(value => value + 1); }} />}
    {archive && <Modal title="Перенести карточку в архив?" description={`${archive.name}. Карточка и семейные связи сохранятся. Новые связи с архивной карточкой недоступны.`} onClose={() => setArchive(null)} busy={busy}><Notice tone="error">{archiveError}</Notice><div className="modal-footer"><Button variant="secondary" onClick={() => setArchive(null)} disabled={busy}>Отмена</Button><Button onClick={() => void archiveCard()} loading={busy}>Перенести в архив</Button></div></Modal>}
  </>;
}

export function FamilyDetails({ kind, id, user }: { kind: Kind; id: string; user: User }) {
  const admin = user.roles.includes('admin'); const student = kind === 'students'; const [card, setCard] = useState<FamilyCard | null>(null); const [error, setError] = useState(''); const [success, setSuccess] = useState(''); const [version, setVersion] = useState(0); const [loading, setLoading] = useState(true);
  const [edit, setEdit] = useState(false); const [linkEditor, setLinkEditor] = useState<FamilyLink | 'new' | null>(null);
  useEffect(() => { const controller = new AbortController(); setLoading(true); setError(''); request<FamilyCard>(`/${kind}/${id}`, 'GET', undefined, controller.signal).then(setCard).catch(err => { if (!controller.signal.aborted) setError(message(err)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); }); return () => controller.abort(); }, [kind, id, version]);
  const related = student ? card?.parents : card?.children;
  const columns: Column<FamilyLink>[] = [
    { key: 'name', title: student ? 'Родитель' : 'Ученик', render: value => <><a href={`#/${student ? 'parents' : 'students'}/${value.id}`}>{value.name}</a><span className="muted">Карточка №{value.id}</span></> },
    { key: 'representation', title: 'Представительство', render: value => <><span>{value.relationship}</span><Badge tone={value.is_primary ? 'success' : 'neutral'}>{value.is_primary ? 'Основной контакт' : 'Дополнительный контакт'}</Badge></> },
    { key: 'details', title: student ? 'Контакты' : 'Дата рождения', render: value => student ? <><span>{value.phone || 'Телефон не указан'}</span><span>{value.email || 'Email не указан'}</span></> : date(value.birth_date) },
    { key: 'status', title: 'Статус', render: value => <Status status={value.status} /> },
    ...(admin && student ? [{ key: 'actions', title: 'Действия', render: (value: FamilyLink) => <Button variant="secondary" onClick={() => setLinkEditor(value)} aria-label={`Изменить связь ${value.name}`}>Изменить связь</Button> }] : []),
  ];
  return <><a className="back-link" href={`#/${kind}`}>← {labels[kind].title}</a><PageHeading title={card?.name ?? `Карточка ${labels[kind].singular}`} description={card ? `Карточка №${card.id}` : undefined} action={admin && card && <Button variant="secondary" onClick={() => setEdit(true)}>Изменить карточку</Button>} />
    <div className="stack"><Notice tone="error">{error}</Notice><Notice tone="success">{success}</Notice>{loading ? <p role="status">Загружаем данные…</p> : error ? <Card><Button variant="secondary" onClick={() => setVersion(value => value + 1)}>Повторить загрузку</Button></Card> : card && <>
      <Card><h2>Основные сведения</h2><dl className="detail-list"><div><dt>Статус</dt><dd><Status status={card.status} /></dd></div>{student ? <><div><dt>Дата рождения</dt><dd>{date(card.birth_date)}</dd></div><div><dt>Куратор</dt><dd>{card.curator_name || 'Не назначен'}</dd></div></> : <><div><dt>Телефон</dt><dd>{card.phone || 'Не указан'}</dd></div><div><dt>Email</dt><dd>{card.email || 'Не указан'}</dd></div></>}</dl></Card>
      <Card className="table-card"><div className="table-heading"><h2>{student ? 'Родители и представители' : 'Связанные ученики'}</h2>{student && admin && card.status === 'active' && <Button onClick={() => setLinkEditor('new')}>Связать с родителем</Button>}</div>
        {related?.length ? <Table caption={student ? 'Родители ученика' : 'Дети родителя'} columns={columns} items={related} rowKey={value => value.id} /> : <EmptyState title="Связи пока не добавлены" />}
      </Card>
    </>}</div>
    {edit && card && <Editor kind={kind} person={card} close={() => setEdit(false)} saved={() => { setEdit(false); setSuccess('Карточка сохранена.'); setVersion(value => value + 1); }} />}
    {linkEditor && <LinkEditor studentID={Number(id)} link={linkEditor === 'new' ? undefined : linkEditor} close={() => setLinkEditor(null)} saved={() => { setLinkEditor(null); setSuccess('Семейная связь сохранена.'); setVersion(value => value + 1); }} />}
  </>;
}
