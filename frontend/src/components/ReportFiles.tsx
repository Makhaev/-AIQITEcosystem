import { useId, useState } from 'react';
import { Button, Notice } from './ui';

export type ReportFile = { id: number; name: string; size: number; created_at: string };
export const MAX_REPORT_FILE = 10 * 1024 * 1024;
export function fileSize(bytes: number) { return bytes < 1024 ? `${bytes} Б` : bytes < 1024 * 1024 ? `${(bytes / 1024).toLocaleString('ru-RU',{maximumFractionDigits:1})} КБ` : `${(bytes / 1024 / 1024).toLocaleString('ru-RU',{maximumFractionDigits:1})} МБ`; }
function FileIcon({name}: {name:string}) { return <span className="report-file-icon" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="M14 3H5v18h14V8zM14 3v5h5M8 12h8M8 16h6" /></svg><span>{name.includes('.') ? name.split('.').at(-1)!.slice(0,4).toUpperCase() : 'ФАЙЛ'}</span></span>; }

export function ReportFileList({reportID,files,removeFile,disabled=false}: {reportID:number;files:ReportFile[];removeFile?:(file:ReportFile)=>void;disabled?:boolean}) {
  if (!files.length) return null;
  return <section className="report-attachments" aria-label="Файлы отчёта"><div className="report-attachments__heading"><h3>Материалы к отчёту</h3><span className="badge badge--success">{files.length}</span></div><ul className="report-file-list">{files.map(file=><li className="report-file-row" key={file.id}><FileIcon name={file.name}/><div className="report-file-info"><a href={`/api/v1/curator-reports/${reportID}/files/${file.id}`} download={file.name}>{file.name}</a><span>{fileSize(file.size)} · {new Date(file.created_at).toLocaleDateString('ru-RU',{timeZone:'Europe/Moscow'})}</span></div>{removeFile ? <Button variant="ghost" disabled={disabled} onClick={()=>removeFile(file)} aria-label={`Удалить вложение ${file.name}`}>×</Button> : <a className="report-file-download" href={`/api/v1/curator-reports/${reportID}/files/${file.id}`} download={file.name} aria-label={`Скачать ${file.name}`}><svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5"><path d="M12 3v12m-5-5 5 5 5-5M4 16v5h16v-5"/></svg></a>}</li>)}</ul></section>;
}

export function ReportFilePicker({files,onChange,existingCount,busy}: {files:File[];onChange:(files:File[])=>void;existingCount:number;busy:boolean}) {
  const inputID=useId(); const [dragging,setDragging]=useState(false); const [error,setError]=useState('');
  function add(incoming:File[]) {
    if (busy) return; setError('');
    const unique=incoming.filter(file=>!files.some(old=>old.name===file.name && old.size===file.size && old.lastModified===file.lastModified));
    if (files.length+existingCount+unique.length>20) { setError('В одном отчёте может быть до 20 файлов.'); return; }
    const invalid=unique.find(file=>!file.size || file.size>MAX_REPORT_FILE || [...file.name].length>200);
    if (invalid) { setError(`«${invalid.name}»: выберите непустой файл до 10 МБ с названием до 200 символов.`); return; }
    onChange([...files,...unique]);
  }
  return <section className="report-attachments stack" aria-label="Добавление вложений"><div className="report-attachments__heading"><h3>Вложения к отчёту</h3><span className="muted">{existingCount+files.length} / 20</span></div>
    <div className={`report-dropzone${dragging?' report-dropzone--active':''}`} onDragOver={event=>{event.preventDefault();if(!busy)setDragging(true);}} onDragLeave={event=>{if(!event.currentTarget.contains(event.relatedTarget as Node|null))setDragging(false);}} onDrop={event=>{event.preventDefault();setDragging(false);add(Array.from(event.dataTransfer.files));}}>
      <span className="report-dropzone__icon" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"><path d="m8 13 7-7a3 3 0 0 1 4 4L9 20a5 5 0 0 1-7-7L13 2a2 2 0 0 1 3 3L6 15a1 1 0 0 0 2 2l9-9"/></svg></span>
      <div><p className="report-dropzone__title">Дополните отчёт материалами</p><p className="muted">Перетащите файлы сюда или выберите на устройстве</p></div>
      <input id={inputID} className="sr-only" type="file" multiple disabled={busy} onChange={event=>{add(Array.from(event.currentTarget.files||[]));event.currentTarget.value='';}} />
      <label className="button button--secondary" htmlFor={inputID}>Выбрать файлы для отчёта</label>
      <p className="report-dropzone__hint">Документы, фотографии и работы ученика · до 10 МБ каждый</p>
    </div>
    <Notice tone="error">{error}</Notice>
    {!!files.length && <ul className="report-file-list" aria-label="Выбранные вложения">{files.map((file,index)=><li className="report-file-row" key={`${file.name}/${file.size}/${index}`}><FileIcon name={file.name}/><div className="report-file-info"><strong>{file.name}</strong><span>{fileSize(file.size)} · Готов к загрузке</span></div><Button variant="ghost" disabled={busy} onClick={()=>onChange(files.filter((_,i)=>i!==index))} aria-label={`Убрать из выбора ${file.name}`}>×</Button></li>)}</ul>}
    <p className="field-hint">Файлы сохранятся вместе с черновиком. Родители получат доступ после публикации отчёта.</p>
  </section>;
}
