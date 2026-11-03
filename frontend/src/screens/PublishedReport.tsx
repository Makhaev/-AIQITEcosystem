import { useEffect, useState } from 'react';
import { message, request } from '../api';
import { Button, Modal, Notice } from '../components/ui';
import { FamilyAttachments, type FamilyReport } from './FamilyPortal';
import { FamilyReportContent } from './FamilyReportContent';

export function PublishedReport({ id, onClose }: { id: number; onClose: () => void }) {
  const [report, setReport] = useState<FamilyReport | null>(null);
  const [error, setError] = useState('');
  const [reload, setReload] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setReport(null); setError('');
    request<FamilyReport>(`/family/reports/${id}`, 'GET', undefined, controller.signal)
      .then(value => { if (!controller.signal.aborted) setReport(value); })
      .catch(err => { if (!controller.signal.aborted) setError(message(err)); });
    return () => controller.abort();
  }, [id, reload]);
  return <Modal title={report?.title || 'Опубликованный отчёт'} onClose={onClose}>
    <div className="stack">
      <Notice tone="error">{error}</Notice>
      {!report && !error && <p role="status">Загрузка отчёта…</p>}
      {report && <><FamilyReportContent report={report} /><FamilyAttachments files={report.files} />{!report.body && !report.snapshot && <p>Текст отчёта пока не заполнен.</p>}</>}
      <Button variant="secondary" onClick={() => setReload(v => v + 1)}>{error ? 'Повторить загрузку отчёта' : 'Обновить отчёт'}</Button>
    </div>
  </Modal>;
}
