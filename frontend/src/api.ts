export type Role = 'admin' | 'leader' | 'teacher' | 'curator' | 'methodologist' | 'education_leader';
export type User = {
  id: number;
  name: string;
  email: string;
  roles: Role[];
  blocked: boolean;
  must_change_password: boolean;
  created_at: string;
  updated_at: string;
};
export type Page = { items: User[]; total: number; page: number; page_size: number };
export const roles: Record<Role, string> = {
  admin: 'Администратор', leader: 'Руководитель', teacher: 'Педагог', curator: 'Куратор',
  methodologist: 'Методист', education_leader: 'Руководитель образования',
};
export class APIError extends Error {
  constructor(public status: number, public code: string, message: string, public fields: Record<string, string> = {}) {
    super(message);
  }
}
let csrf = '';
export async function getCSRF() {
  const result = await request<{ csrf_token: string }>('/auth/csrf');
  csrf = result.csrf_token;
}
export async function request<T>(path: string, method = 'GET', body?: unknown, signal?: AbortSignal): Promise<T> {
  if (method !== 'GET' && !csrf) await getCSRF();
  const response = await fetch(`/api/v1${path}`, {
    method, credentials: 'same-origin', signal,
    headers: {
      ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
      ...(method === 'GET' ? {} : { 'X-CSRF-Token': csrf }),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!response.ok) {
    const error = await response.json().catch(() => ({ code: 'connection_error', message: 'Сервер недоступен.' }));
    signal?.throwIfAborted();
    if (response.status === 401 && path !== '/auth/login') window.dispatchEvent(new Event('aiq-session-ended'));
    if (error.code === 'csrf_failed') csrf = '';
    throw new APIError(response.status, error.code, error.message, error.field_errors);
  }
  if (response.status === 204) { if (path.startsWith('/auth/')) csrf = ''; return undefined as T; }
  const result = await response.json();
  if (result.csrf_token) csrf = result.csrf_token;
  return result as T;
}
export function message(error: unknown): string {
  if (error instanceof APIError) return [error.message, ...Object.values(error.fields || {})].join(' ');
  return 'Не удалось связаться с сервером. Проверьте соединение и повторите.';
}
