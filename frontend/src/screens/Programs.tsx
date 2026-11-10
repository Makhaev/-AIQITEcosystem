import { useEffect, useId, useState, type FormEvent } from 'react';
import { EntityCard } from '../components/EntityCard';
import { ModuleFiles } from '../components/ModuleFiles';
import { APIError, message, request, type User } from '../api';
import { Badge, Button, Card, EmptyState, Input, Modal, Notice, PageHeading, Pagination, Select, Table, type Column } from '../components/ui';

type Program = { module_count?: number; id: number; name: string; description: string; status: 'draft' | 'active' | 'archived'; created_at: string; updated_at: string };
type Module = { id: number; program_id: number; name: string; description: string; position: number };
type ProgramCard = Program & { modules: Module[] };
type ProgramPage = { items: Program[]; total: number; page: number; page_size: number };
function Status({ status }: { status: Program['status'] }) { return <Badge tone={status === 'active' ? 'success' : status === 'draft' ? 'warning' : 'neutral'}>{status === 'active' ? 'Активна' : status === 'draft' ? 'Черновик' : 'В архиве'}</Badge>; }
function Description({ value, disabled, error }: { value?: string; disabled: boolean; error?: string }) {
  const id = useId();
  return <div className="field"><label htmlFor={id}>Описание</label><textarea id={id} name="description" defaultValue={value} maxLength={5000} disabled={disabled} aria-invalid={!!error || undefined} aria-describedby={[`${id}-hint`, error ? `${id}-error` : ''].filter(Boolean).join(' ')} /><p className="field-hint" id={`${id}-hint`}>Необязательно. До 5000 символов.</p>{error && <p className="field-error" id={`${id}-error`}>{error}</p>}</div>;
}
function ProgramEditor({ program, close, saved }: { program?: Program; close: () => void; saved: () => void }) {
  const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [fields, setFields] = useState<Record<string, string>>({});
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const data = new FormData(event.currentTarget); setBusy(true); setError(''); setFields({});
    try { await request(program ? `/programs/${program.id}` : '/programs', program ? 'PATCH' : 'POST', { name: data.get('name'), description: data.get('description'), status: data.get('status') }); saved(); }
    catch (err) { setError(message(err)); if (err instanceof APIError) setFields(err.fields ?? {}); } finally { setBusy(false); }
  }
  return <Modal title={program ? 'Изменить программу' : 'Новая программа'} description="Новые группы можно связать с программой после перевода в статус «Активна»." onClose={close} busy={busy}><Notice tone="error">{error}</Notice><form onSubmit={submit}>
    <Input label="Название" name="name" required maxLength={200} defaultValue={program?.name} disabled={busy} error={fields.name} />
    <Description value={program?.description} disabled={busy} error={fields.description} />
    <Select label="Статус" name="status" defaultValue={program?.status ?? 'draft'} disabled={busy} error={fields.status}><option value="draft">Черновик</option><option value="active">Активна</option><option value="archived">В архиве</option></Select>
    <div className="modal-footer"><Button variant="secondary" onClick={close} disabled={busy}>Отмена</Button><Button type="submit" loading={busy}>Сохранить программу</Button></div>
  </form></Modal>;
}
function ModuleEditor({ programID, module, position, close, saved }: { programID: number; module?: Module; position: number; close: () => void; saved: () => void }) {
  const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [fields, setFields] = useState<Record<string, string>>({});
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); const data = new FormData(event.currentTarget); setBusy(true); setError(''); setFields({});
    try { await request(`/programs/${programID}/modules${module ? `/${module.id}` : ''}`, module ? 'PATCH' : 'POST', { name: data.get('name'), description: data.get('description'), position: Number(data.get('position')) }); saved(); }
    catch (err) { setError(message(err)); if (err instanceof APIError) setFields(err.fields ?? {}); } finally { setBusy(false); }
  }
  return <Modal title={module ? 'Изменить модуль' : 'Новый модуль'} description="Порядковый номер должен быть свободен в этой программе. Пробелы в нумерации допустимы." onClose={close} busy={busy}><Notice tone="error">{error}</Notice><form onSubmit={submit}>
    <Input label="Название" name="name" defaultValue={module?.name} required maxLength={200} disabled={busy} error={fields.name} />
    <Description value={module?.description} disabled={busy} error={fields.description} />
    <Input label="Порядковый номер" name="position" type="number" step={1} min={1} max={2147483647} required defaultValue={module?.position ?? position} disabled={busy} error={fields.position} />
    <div className="modal-footer"><Button variant="secondary" onClick={close} disabled={busy}>Отмена</Button><Button type="submit" loading={busy}>Сохранить модуль</Button></div>
  </form></Modal>;
}

export function Programs({ user }: { user: User }) {
  const admin = user.roles.some(role => role === 'admin' || role === 'methodist'); const [data, setData] = useState<ProgramPage | null>(null); const [page, setPage] = useState(1); const [filters, setFilters] = useState(''); const [version, setVersion] = useState(0);
  const [loading, setLoading] = useState(true); const [error, setError] = useState(''); const [success, setSuccess] = useState(''); const [editor, setEditor] = useState<Program | 'new' | null>(null);
  const [archive, setArchive] = useState<Program | null>(null); const [archiveError, setArchiveError] = useState(''); const [busy, setBusy] = useState(false);
  useEffect(() => { const controller = new AbortController(); setLoading(true); setError(''); request<ProgramPage>(`/programs?page=${page}&page_size=20${filters}`, 'GET', undefined, controller.signal).then(setData).catch(err => { if (!controller.signal.aborted) setError(message(err)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); }); return () => controller.abort(); }, [page, filters, version]);
  function apply(event: FormEvent<HTMLFormElement>) { event.preventDefault(); const data = new FormData(event.currentTarget); const query = new URLSearchParams(); for (const key of ['search', 'status']) { const value = String(data.get(key) ?? '').trim(); if (value) query.set(key, value); } setPage(1); setFilters(query.size ? `&${query}` : ''); }
  async function archiveProgram() { if (!archive) return; setBusy(true); setArchiveError(''); try { await request(`/programs/${archive.id}`, 'PATCH', { status: 'archived' }); setArchive(null); setSuccess('Программа перенесена в архив. Модули и существующие связи сохранены.'); setVersion(value => value + 1); } catch (err) { setArchiveError(message(err)); } finally { setBusy(false); } }
  return <><PageHeading title="Программы" description="Образовательные программы и упорядоченные модули." action={admin && <Button onClick={() => setEditor('new')}>Добавить программу</Button>} />
    <form className="filters" onSubmit={apply}><Input label="Поиск по названию" name="search" maxLength={200} /><Select label="Статус" name="status"><option value="">Все статусы</option><option value="draft">Черновик</option><option value="active">Активна</option><option value="archived">В архиве</option></Select><Button variant="secondary" type="submit">Применить</Button></form>
    <div className="stack"><Notice tone="success">{success}</Notice><Notice tone="error">{error}</Notice><Card className="table-card" aria-busy={loading}><div className="table-heading"><h2>Список программ</h2><span className="muted">Всего: {data?.total ?? '—'}</span></div>
      {loading ? <p className="empty-state" role="status">Загружаем данные…</p> : error ? <EmptyState title="Не удалось загрузить программы" action={<Button variant="secondary" onClick={() => setVersion(value => value + 1)}>Повторить загрузку</Button>} /> : data?.items.length ? <div className="entity-grid program-cards" aria-label="Программы AIQ">{data.items.map(value => <EntityCard key={value.id} title={value.name} titleHref={`#/programs/${value.id}`} status={value.status === 'active' ? 'Активна' : value.status === 'draft' ? 'Черновик' : 'В архиве'} tone={value.status === 'active' ? 'success' : value.status === 'draft' ? 'warning' : 'neutral'} description={value.description || 'Описание пока не добавлено.'} facts={[{ label: 'Модулей', value: value.module_count ?? 'Нет данных' }]} icon={<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="M3 4h6a4 4 0 0 1 3 1.5A4 4 0 0 1 15 4h6v16h-6a4 4 0 0 0-3 1.5A4 4 0 0 0 9 20H3zM12 5.5v16" /></svg>} action={<a className="button button--primary" href={`#/programs/${value.id}`}>Открыть программу</a>}>
      {admin && <details className="entity-details"><summary>Управление программой</summary><div className="row-actions"><Button variant="secondary" onClick={() => setEditor(value)} aria-label={`Изменить программу ${value.name}`}>Изменить</Button>{value.status !== 'archived' && <Button variant="ghost" onClick={() => { setArchive(value); setArchiveError(''); }} aria-label={`Архивировать программу ${value.name}`}>В архив</Button>}</div></details>}
    </EntityCard>)}</div> : <EmptyState title="Программы не найдены" description={user.roles.some(role => role === 'admin' || role === 'leader' || role === 'methodist') ? 'Добавьте программу или измените фильтры.' : 'Здесь появятся программы связанных с вами групп.'} />}
      {data && <Pagination page={page} total={data.total} pageSize={20} onChange={setPage} disabled={loading} />}
    </Card></div>
    {editor && <ProgramEditor program={editor === 'new' ? undefined : editor} close={() => setEditor(null)} saved={() => { setEditor(null); setSuccess('Программа сохранена.'); setVersion(value => value + 1); }} />}
    {archive && <Modal title="Перенести программу в архив?" description={`${archive.name}. Программа и её модули останутся в истории. Назначать её новым группам будет нельзя.`} onClose={() => setArchive(null)} busy={busy}><Notice tone="error">{archiveError}</Notice><div className="modal-footer"><Button variant="secondary" onClick={() => setArchive(null)} disabled={busy}>Отмена</Button><Button onClick={() => void archiveProgram()} loading={busy}>Перенести в архив</Button></div></Modal>}
  </>;
}

function ProgramContent({ card, editable, editProgram, editModule }: { card: ProgramCard; editable: boolean; editProgram: () => void; editModule: (module: Module | 'new') => void }) {
  const [selectedID, setSelectedID] = useState<number | null>(null);
  const [treeOpen, setTreeOpen] = useState(false);
  const selected = card.modules.find(module => module.id === selectedID);
  const choose = (id: number | null) => { setSelectedID(id); setTreeOpen(false); };
  const outline = <nav aria-label="Содержание программы"><Button variant="ghost" aria-current={!selected ? 'page' : undefined} onClick={() => choose(null)}>{card.name}</Button>
    <ol className="program-module-list">{card.modules.map(module => <li key={module.id}><Button variant="ghost" aria-current={selected?.id === module.id ? 'page' : undefined} onClick={() => choose(module.id)}>{module.position}. {module.name}</Button></li>)}</ol>
    {!card.modules.length && <p className="muted">Модулей пока нет.</p>}
    {editable && <Button variant="secondary" onClick={() => { setTreeOpen(false); editModule('new'); }}>Добавить модуль</Button>}
  </nav>;
  return <><Button className="program-tree-toggle" variant="secondary" onClick={() => setTreeOpen(true)} aria-haspopup="dialog">Содержание программы</Button>
    <div className="program-editor"><aside className="program-outline"><Card><h2>Содержание</h2>{outline}</Card></aside>
      <section aria-label="Выбранный элемент программы" className="stack"><Card><div className="table-heading"><h2>{selected ? selected.name : 'Основные данные программы'}</h2>
        {(selected ? editable : true) && <Button variant="secondary" onClick={() => selected ? editModule(selected) : editProgram()}>{selected ? 'Изменить модуль' : 'Изменить программу'}</Button>}</div>
        <Status status={card.status} />{selected && <p>Модуль {selected.position}</p>}
        <p className="program-description">{(selected ? selected.description : card.description) || 'Описание не заполнено.'}</p>
        {!selected && <p>Модулей: {card.modules.length}. Выберите модуль в содержании, чтобы посмотреть и изменить его описание.</p>}
        {card.status === 'archived' && <p className="muted">Для изменения модулей восстановите программу через её статус.</p>}
      </Card>{selected && <Card><ModuleFiles key={`${card.id}/${selected.id}`} programID={card.id} moduleID={selected.id} editable={editable} /></Card>}</section>
    </div>{treeOpen && <Modal className="navigation-drawer" title="Содержание программы" onClose={() => setTreeOpen(false)}>{outline}</Modal>}</>;
}

export function ProgramDetails({ id, user }: { id: string; user: User }) {
  const admin = user.roles.some(role => role === 'admin' || role === 'methodist'); const [card, setCard] = useState<ProgramCard | null>(null); const [loading, setLoading] = useState(true); const [error, setError] = useState(''); const [success, setSuccess] = useState(''); const [version, setVersion] = useState(0);
  const [editor, setEditor] = useState(false); const [moduleEditor, setModuleEditor] = useState<Module | 'new' | null>(null);
  const [filesModule, setFilesModule] = useState<Module | null>(null);
  useEffect(() => { const controller = new AbortController(); setLoading(true); setError(''); request<ProgramCard>(`/programs/${id}`, 'GET', undefined, controller.signal).then(setCard).catch(err => { if (!controller.signal.aborted) setError(message(err)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); }); return () => controller.abort(); }, [id, version]);
  const editable = admin && card?.status !== 'archived'; const positions = new Set(card?.modules.map(value => value.position)); let next = 1; while (positions.has(next)) next++;
  const columns: Column<Module>[] = [
    { key: 'position', title: 'Порядковый номер', render: value => value.position },
    { key: 'name', title: 'Модуль', render: value => <strong>{value.name}</strong> },
    { key: 'description', title: 'Описание', render: value => <span className="program-description">{value.description || 'Не указано'}</span> },
    { key: 'files', title: 'Учебные материалы', render: value => <Button variant="secondary" onClick={() => setFilesModule(value)} aria-label={`Файлы модуля ${value.name}`}>Файлы</Button> },
    ...(editable ? [{ key: 'actions', title: 'Действия', render: (value: Module) => <Button variant="secondary" onClick={() => setModuleEditor(value)} aria-label={`Изменить модуль ${value.name}`}>Изменить</Button> }] : []),
  ];
  return <><a className="back-link" href="#/programs">← Программы</a><PageHeading title={card?.name ?? 'Программа'} description="Содержание программы и последовательность модулей." action={card && admin && <Button variant="secondary" onClick={() => setEditor(true)}>Изменить программу</Button>} />
    <div className="stack"><Notice tone="success">{success}</Notice><Notice tone="error">{error}</Notice>{loading ? <p role="status">Загружаем данные…</p> : error ? <Card><Button variant="secondary" onClick={() => setVersion(value => value + 1)}>Повторить загрузку</Button></Card> : card && <>
      {user.roles.includes('methodist') ? <ProgramContent card={card} editable={editable} editProgram={() => setEditor(true)} editModule={setModuleEditor} /> : <><Card><h2>Основные данные</h2><dl className="detail-list"><div><dt>Статус</dt><dd><Status status={card.status} /></dd></div><div><dt>Описание</dt><dd className="program-description">{card.description || 'Не указано'}</dd></div></dl>{card.status === 'archived' && <p className="muted">Для изменения модулей восстановите программу через её статус.</p>}</Card>
      <Card className="table-card"><div className="table-heading"><h2>Модули программы</h2>{editable && <Button onClick={() => setModuleEditor('new')}>Добавить модуль</Button>}</div>{card.modules.length ? <Table caption="Модули программы" items={card.modules} columns={columns} rowKey={value => value.id} /> : <EmptyState title="Модулей пока нет" />}</Card></>}
    </>}</div>
    {editor && card && <ProgramEditor program={card} close={() => setEditor(false)} saved={() => { setEditor(false); setSuccess('Программа сохранена.'); setVersion(value => value + 1); }} />}
    {filesModule && card && <Modal title={`Файлы: ${filesModule.name}`} onClose={() => setFilesModule(null)}><ModuleFiles key={`${card.id}/${filesModule.id}`} programID={card.id} moduleID={filesModule.id} editable={editable} /></Modal>}
    {moduleEditor && card && <ModuleEditor programID={card.id} module={moduleEditor === 'new' ? undefined : moduleEditor} position={next} close={() => setModuleEditor(null)} saved={() => { setModuleEditor(null); setSuccess('Модуль сохранён.'); setVersion(value => value + 1); }} />}
  </>;
}
