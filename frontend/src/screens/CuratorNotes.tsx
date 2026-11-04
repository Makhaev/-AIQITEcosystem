import { useEffect, useId, useState, type FormEvent } from 'react';
import { message, request } from '../api';
import { Button, Card, Notice, Pagination } from '../components/ui';
type Revision={version:number;body:string;editor_name:string;created_at:string};
type Note={id:number;author_name:string;body:string;version:number;created_at:string;can_edit:boolean;history:Revision[]};
const at=(v:string)=>new Date(v).toLocaleString('ru-RU',{timeZone:'Europe/Moscow'});
export function CuratorNotes({studentID}:{studentID:string}){
 return <StudentNotes key={studentID} studentID={studentID}/>;
}
function StudentNotes({studentID}:{studentID:string}){
 const [data,setData]=useState<{items:Note[];total:number}|null>(null);const [page,setPage]=useState(1);const [reload,setReload]=useState(0);const [editing,setEditing]=useState<Note|null>(null);const [body,setBody]=useState('');const [error,setError]=useState('');const [busy,setBusy]=useState(false);const id=useId();
 useEffect(()=>{const c=new AbortController();setData(null);setError('');void request<{items:Note[];total:number}>(`/students/${studentID}/notes?page=${page}`,'GET',undefined,c.signal).then(setData).catch(e=>{if(!c.signal.aborted)setError(message(e))});return()=>c.abort()},[studentID,page,reload]);
 async function save(e:FormEvent){e.preventDefault();setBusy(true);setError('');try{await request(`/students/${studentID}/notes${editing?`/${editing.id}`:''}`,editing?'PATCH':'POST',{body,...(editing?{version:editing.version}:{})});setBody('');setEditing(null);setPage(1);setReload(v=>v+1)}catch(e){setError(message(e))}finally{setBusy(false)}}
 return <Card><h2>Заметки куратора</h2><p className="muted">Внутренняя лента. Заметки и их история не публикуются семье. После смены куратора лента доступна новому куратору.</p><Notice tone="error">{error}</Notice>{error&&<Button variant="secondary" onClick={()=>setReload(v=>v+1)}>Обновить заметки</Button>}
 <form onSubmit={save}><label htmlFor={id}>{editing?'Исправление заметки':'Новая заметка'}</label><textarea className="input" id={id} value={body} onChange={e=>setBody(e.target.value)} maxLength={2000} required disabled={busy}/><div className="row-actions"><Button type="submit" loading={busy}>{editing?'Сохранить исправление':'Добавить заметку'}</Button>{editing&&<Button variant="secondary" onClick={()=>{setEditing(null);setBody('')}}>Отмена</Button>}</div></form>
 {!data&&!error&&<p role="status">Загружаем заметки…</p>}{data?.items.map(note=><article className="pilot-list-row" key={note.id}><h3>{note.author_name}</h3><p className="muted">{at(note.created_at)} МСК · версия {note.version}</p><p className="pilot-prose">{note.body}</p>{note.can_edit&&<Button variant="secondary" onClick={()=>{setEditing(note);setBody(note.body)}}>Исправить заметку</Button>}<details><summary>История изменения</summary>{note.history.map(v=><div key={v.version}><p>{v.editor_name} · {at(v.created_at)} МСК · версия {v.version}</p><p className="pilot-prose">{v.body}</p></div>)}</details></article>)}{data&&!data.items.length&&<p>Заметок пока нет.</p>}{data&&<Pagination page={page} pageSize={20} total={data.total} onChange={setPage}/>}</Card>
}
