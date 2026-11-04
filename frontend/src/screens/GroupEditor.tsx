import { useEffect, useState, type FormEvent } from 'react';
import { APIError, message, request } from '../api';
import { Button, Input, Modal, Notice, Select } from '../components/ui';
import { referenceItems } from './Education';
import { VersionPicker } from './ProgramVersions';

type GroupRecord = { id: number; [key: string]: unknown };
export function GroupEditor({ record, close, saved }: { record?: GroupRecord; close: () => void; saved: () => void }) {
  const [branches,setBranches]=useState<GroupRecord[]>([]); const[curators,setCurators]=useState<GroupRecord[]>([]);
  const [branch, setBranch] = useState(String(record?.branch_id ?? ''));
  const [curator, setCurator] = useState(String(record?.curator_id ?? ''));
  const [programs, setPrograms] = useState<GroupRecord[]>([]); const [teachers, setTeachers] = useState<GroupRecord[]>([]);
  const [program, setProgram] = useState(String(record?.program_id ?? '')); const [version, setVersion] = useState('');
  const [teacher, setTeacher] = useState(String(record?.teacher_id ?? ''));
  const [loading, setLoading] = useState(true); const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [fields, setFields] = useState<Record<string, string>>({}); const [reload, setReload] = useState(0);
  useEffect(() => {
    const controller = new AbortController(); setLoading(true); setError('');
    Promise.all([
      referenceItems('programs?status=active', controller.signal),
      referenceItems('teachers?status=active', controller.signal),
      referenceItems('branches', controller.signal),
      referenceItems('users?role=curator', controller.signal),
    ]).then(([p, t, b, c]) => {
      if (controller.signal.aborted) return;
      if (record && !p.some(i => i.id === record.program_id)) p.push({ id: Number(record.program_id), name: String(record.program_name ?? '') });
      if (record?.teacher_id && !t.some(i => i.id === record.teacher_id)) t.push({ id: Number(record.teacher_id), name: String(record.teacher_name ?? '') });
      setPrograms(p); setTeachers(t); setBranches(b); setCurators(c);
    }).catch(error => { if (!controller.signal.aborted) setError(message(error)); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [record, reload]);
  async function save(e: FormEvent<HTMLFormElement>) {
    e.preventDefault(); const data = new FormData(e.currentTarget); setBusy(true); setError(''); setFields({});
    const body = { branch_id:data.get('branch_id')?Number(data.get('branch_id')):null,curator_id:data.get('curator_id')?Number(data.get('curator_id')):null,name: String(data.get('name') ?? '').trim(), teacher_id: teacher ? Number(teacher) : null, capacity: Number(data.get('capacity')), status: String(data.get('status')), ...(!record ? { program_id: Number(program), program_version_id: Number(version) } : {}) };
    try { await request(record ? `/groups/${record.id}` : '/groups', record ? 'PATCH' : 'POST', body); saved(); } catch (err) { setError(message(err)); if (err instanceof APIError) setFields(err.fields); } finally { setBusy(false); }
  }
  return <Modal title={record ? 'Изменить: группа' : 'Создать: группа'} onClose={close} busy={busy}><Notice tone="error">{error}</Notice>{error && !programs.length && <Button variant="secondary" onClick={() => setReload(v => v + 1)}>Повторить загрузку</Button>}<form onSubmit={save}>
    <Input label="Название" name="name" required maxLength={200} defaultValue={String(record?.name ?? '')} disabled={busy} error={fields.name} />
    <Select label="Программа" required value={program} disabled={busy || loading || !!record} onChange={e => { setProgram(e.target.value); setVersion(''); }} hint={record ? 'Для смены программы откройте перевод в карточке группы.' : undefined}><option value="">Выберите программу</option>{programs.map(p => <option key={p.id} value={p.id}>{String(p.name)}</option>)}</Select>
    {!record && <VersionPicker programID={Number(program)} selected={version} onChange={setVersion} disabled={busy} />}
    <Select label="Основной педагог" name="teacher_id" value={teacher} onChange={e => setTeacher(e.target.value)} disabled={busy || loading} error={fields.teacher_id}><option value="">Не выбран</option>{teachers.map(t => <option key={t.id} value={t.id}>{String(t.name)}</option>)}</Select>
    <Select label="Филиал" name="branch_id" value={branch} onChange={event => setBranch(event.target.value)} disabled={busy||loading} error={fields.branch_id}><option value="">Не выбран</option>{branches.filter(b=>b.status==='active'||b.id===record?.branch_id).map(b=><option key={b.id} value={b.id}>{String(b.name)}</option>)}</Select>
    <Select label="Куратор группы" name="curator_id" value={curator} onChange={event => setCurator(event.target.value)} disabled={busy||loading} error={fields.curator_id} hint="Кураторы действующих групп имеют приоритет над индивидуальным назначением ученику."><option value="">Не назначен</option>{curators.filter(c=>!c.blocked).map(c=><option key={c.id} value={c.id}>{String(c.name)}</option>)}{!!record?.curator_id && !curators.some(c => c.id === record.curator_id && !c.blocked) && <option value={String(record.curator_id)}>{String(record.curator_name || 'Текущий куратор')} (прежнее назначение)</option>}</Select>
    <Input label="Вместимость" name="capacity" type="number" min={1} max={2147483647} required defaultValue={Number(record?.capacity ?? 10)} disabled={busy} error={fields.capacity} />
    <Select label="Статус" name="status" defaultValue={String(record?.status ?? 'draft')} disabled={busy}><option value="draft">Черновик</option><option value="active">Активна</option><option value="archived">В архиве</option></Select>
    <div className="modal-footer"><Button variant="secondary" disabled={busy} onClick={close}>Отмена</Button><Button type="submit" loading={busy} disabled={loading || !programs.length || !record && !version}>Сохранить</Button></div>
  </form></Modal>;
}
